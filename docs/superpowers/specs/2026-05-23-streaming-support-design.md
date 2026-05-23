# Streaming Support Design

## Goal

Add streaming request support to `easyllm` without changing the behavior of the existing synchronous APIs.

The first implementation target is `ProviderOpenAICompatible`. The design must preserve the current top-level library shape, keep non-streaming behavior stable, and leave a clean path for later provider reuse.

## Scope

This design covers:

- A new streaming API for `Client`
- A new streaming API for `Engine`
- A shared stream event model
- Streaming integration with the existing engine tool loop
- Error and cancellation semantics
- First-phase tests

This design does not cover:

- Reworking current non-streaming timeout or retry behavior
- Streaming support for all built-in providers in the first phase
- Public exposure of tool arguments or tool result payloads in stream events
- Real-time token accounting or progressive usage events
- A streaming-specific hook system redesign

## Requirements

### Functional

1. Existing `Client.Generate(...)` must remain unchanged.
2. Existing `Engine.Run(...)` must remain unchanged.
3. The library must provide explicit streaming entry points rather than overloading current methods with flags.
4. Streaming text output must be delivered incrementally as deltas.
5. `Engine` streaming must support the existing tool loop.
6. `Engine.RunStream(...)` must still return a final `RunResult`.
7. Tool execution must be visible to callers through stream events, but only at the start/finish level in phase one.

### Non-Functional

1. The first phase must stay narrow enough to implement without destabilizing the current codebase.
2. The public API should remain coherent and not require type assertions to access streaming.
3. Internal streaming transport details should stay isolated from engine orchestration logic.
4. Later provider support should be able to reuse the transport-layer streaming design.

## Public API

### Client

Extend the public `Client` interface with a streaming method:

```go
type Client interface {
    Generate(context.Context, ModelRequest) (*ModelResponse, error)
    GenerateStream(context.Context, ModelRequest, StreamHandler) error
}
```

`Generate(...)` remains the synchronous single-call API. `GenerateStream(...)` is the new explicit streaming API for a single model request.

### Engine

Add a parallel streaming entry point:

```go
func (e *Engine) RunStream(ctx context.Context, req RunRequest, handler StreamHandler) (*RunResult, error)
```

`Run(...)` remains unchanged. `RunStream(...)` reuses the existing runtime behavior while emitting stream events during model execution and tool transitions.

### Stream Handler

The external consumption model is callback-based:

```go
type StreamHandler func(StreamEvent) error
```

The callback returns an error to signal early termination by the caller.

## Stream Event Model

Phase one uses a single lightweight event type shared by both `Client` and `Engine`.

```go
type StreamEventType string

const (
    StreamEventMessageDelta StreamEventType = "message_delta"
    StreamEventToolStart    StreamEventType = "tool_start"
    StreamEventToolFinish   StreamEventType = "tool_finish"
    StreamEventDone         StreamEventType = "done"
)

type StreamEvent struct {
    Type     StreamEventType
    Text     string
    ToolName string
    Raw      any
}
```

Phase-one rules:

- `message_delta` carries only newly received text.
- `tool_start` and `tool_finish` carry only the tool name.
- `done` indicates normal stream completion.
- `Raw` is reserved for future transport or provider diagnostics, but is not a primary part of phase-one behavior.

Out of scope for phase one:

- Full accumulated text snapshots
- Tool argument exposure
- Tool result payload exposure
- Dedicated error events

## Behavioral Model

### Client.GenerateStream

`GenerateStream(...)` is the streaming form of one model call. It is responsible for:

- starting the provider stream request
- decoding the stream protocol
- emitting text deltas through the handler
- aggregating the streamed response internally into a complete `ModelResponse`

It is not responsible for:

- sessions
- tool loop orchestration
- stop reason policy across multiple model calls
- final `RunResult` construction

The caller sees a stream of events and a terminal `error` result. Internally, the client implementation must still build a complete response so higher layers can continue using the same normalized result model.

### Engine.RunStream

`RunStream(...)` is the streaming form of the current engine runtime. It reuses:

- instruction handling
- session management
- request construction
- model call counting
- tool loop logic
- usage aggregation
- stop reason handling
- final `RunResult` assembly

The only major behavioral change is that each model call is executed through `GenerateStream(...)` instead of `Generate(...)`, with streamed text deltas forwarded to the caller during execution.

## Architecture

### 1. Public API Layer

The public package defines:

- `StreamEventType`
- `StreamEvent`
- `StreamHandler`
- `Client.GenerateStream(...)`
- `Engine.RunStream(...)`

This layer contains no provider-specific streaming protocol knowledge.

### 2. Transport Streaming Layer

Phase one adds the concrete streaming transport implementation to `internal/openai/compat`.

Responsibilities:

- send the HTTP request in provider-compatible streaming mode
- parse the upstream stream format
- extract assistant text deltas
- collect tool call fragments when present
- detect stream termination
- aggregate a full normalized `model.ModelResponse`

This layer translates provider streaming output into the library's internal normalized representation.

### 3. Single-Call Aggregation Layer

Streaming transport cannot stop at event emission alone. It must also build the final single-call response object that the engine already knows how to consume.

Responsibilities:

- accumulate text deltas into full assistant content
- accumulate tool call fragments into full `ToolCallOutput` values
- preserve finish reason and usage when they arrive
- return one complete `ModelResponse` when the stream ends normally

This keeps the engine insulated from low-level stream parsing details.

### 4. Engine Orchestration Layer

`Engine.RunStream(...)` uses the aggregated result from each streamed model call to preserve the current agent loop behavior.

Responsibilities:

- write normalized model outputs into the session
- inspect outputs for tool calls
- execute tools
- append tool results back into the session
- continue the next model iteration when needed
- emit tool lifecycle events to the caller
- return the final `RunResult`

## RunStream Data Flow

The expected runtime flow for one `RunStream(...)` call is:

1. Prepare the session and append the new user input.
2. Build the first `ModelRequest`.
3. Call `Client.GenerateStream(...)`.
4. For each text delta received during the stream, emit `message_delta`.
5. When the stream ends, receive the internally aggregated `ModelResponse`.
6. Append that response to the session and update usage, iteration, and last response state.
7. Inspect the normalized outputs:
   - if there are no tool calls, emit `done` and return the final `RunResult`
   - if there are tool calls, continue with tool execution
8. For each tool call:
   - emit `tool_start`
   - execute the tool
   - append the tool result item to the session
   - emit `tool_finish`
9. Start the next model iteration if the engine should continue.
10. If the run stops normally after the final model iteration, emit `done` and return the final `RunResult`.

Important semantics:

- `message_delta` events come only from model text output.
- Tool results are not emitted as text events.
- `tool_finish` is not a terminal event; more model output may follow.

## Error Handling

### Model Request Failure

Examples:

- request creation failure
- HTTP transport failure
- upstream provider error
- stream decoding failure
- malformed streaming payload

Behavior:

- `GenerateStream(...)` returns `error`
- `RunStream(...)` returns `error`
- no `done` event is emitted

This matches the existing distinction between execution failures and successful runs with business-level tool outcomes.

### Tool Failure

Tool execution failures remain non-fatal runtime results, consistent with current `Run(...)` behavior.

Behavior:

- emit `tool_start`
- execute the tool
- emit `tool_finish` even if the tool returns an error
- record the tool error in `RunResult.ToolResults`
- append a structured tool result back into the session for later model consumption
- do not fail `RunStream(...)` solely because the tool failed

### Handler Failure

If the caller's stream handler returns an error:

- stop reading or processing further events
- abort any remaining engine work
- return that error from `GenerateStream(...)` or `RunStream(...)`
- do not emit `done`

This makes caller intent explicit and avoids continuing expensive model or tool work after the consumer has already failed.

### Context Cancellation

If the provided context is canceled or reaches its deadline:

- stop stream processing immediately
- return `ctx.Err()`
- do not emit `done`

## Provider Scope

Phase one implements streaming only for `ProviderOpenAICompatible`.

Rationale:

- this matches the current internal reuse boundary
- it keeps the first implementation focused
- later provider support can reuse the `internal/openai/compat` streaming transport design

OpenAI, Qwen, and DeepSeek streaming support are deferred follow-up work.

## Compatibility Strategy

The design intentionally avoids a mode flag on existing methods. Streaming and non-streaming remain separate entry points:

- `Generate(...)` stays synchronous
- `GenerateStream(...)` is always streaming
- `Run(...)` stays synchronous
- `RunStream(...)` is always streaming

This keeps method contracts clear and avoids mixing return-value styles or lifecycle semantics.

## Testing Strategy

Phase-one tests should cover the following areas.

### Client Streaming

1. Emits multiple `message_delta` events in order.
2. Properly finishes a normal stream.
3. Aggregates a complete normalized `ModelResponse` from the stream.
4. Preserves finish reason and usage when present.
5. Reconstructs tool calls when they appear in stream fragments.

### Engine Streaming

1. Streams assistant text through `RunStream(...)`.
2. Executes the existing tool loop after a streamed tool call.
3. Emits `tool_start` and `tool_finish` in the expected order.
4. Continues streaming text in later iterations after tool execution.
5. Returns a complete `RunResult` at the end of the run.

### Error Paths

1. Provider stream request fails.
2. Stream payload is malformed.
3. Handler returns an error.
4. Context is canceled mid-stream.
5. Tool execution fails but the run still completes with the tool error recorded in `RunResult`.

### Regression

1. Existing synchronous `Generate(...)` tests remain green.
2. Existing synchronous `Run(...)` tests remain green.
3. Existing provider behavior outside streaming remains unchanged.

## Implementation Boundaries

The first implementation should avoid unrelated structural cleanup. The goal is to add streaming without broad refactoring.

Targeted refactoring is acceptable only where it directly supports:

- sharing result aggregation logic between sync and stream paths
- keeping provider transport logic separate from engine logic
- keeping public event types coherent

## Follow-Up Work

Expected follow-up items after phase one:

- add streaming support to other built-in providers through the compat layer or provider-specific adapters
- decide whether hooks need stream-aware lifecycle additions
- consider richer stream events if consumers need more tool visibility
- consider stream-specific timeout and liveness policies once the transport path exists

## Summary

The design adds explicit streaming APIs to both `Client` and `Engine`, keeps existing synchronous behavior intact, implements the first phase only for `ProviderOpenAICompatible`, and preserves the current engine tool loop by separating low-level stream parsing from higher-level runtime orchestration.
