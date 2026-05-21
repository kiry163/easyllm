# easyllm v1 Design

## Overview

`easyllm` v1 is a Go library for building tool-capable agent workflows on top of multiple model providers.

The library should:

- standardize model invocation behind a small provider interface
- support both chat-style and responses-style transports
- execute tool-calling loops within a session
- keep session state in memory only
- allow users to bring their own provider implementation

The library should not:

- persist memory across processes
- provide long-term memory or knowledge storage
- implement planning graphs or multi-agent orchestration
- implement image, audio, or speech generation in v1

The architectural center of v1 is `provider + runtime`, with `tool` as a reusable foundation and `agent` as a thin policy layer.

## Goals

- Provide a stable `provider.Client` abstraction that runtime depends on.
- Normalize OpenAI Chat Completions and Responses transports into one internal model response shape.
- Support robust tool calling with strong argument binding and JSON repair.
- Support session-scoped continuous execution without persistence.
- Provide a thin `agent.Agent` wrapper for common usage.
- Keep the API small enough that internal and external users can implement custom providers without friction.

## Non-Goals

- Persistent memory stores
- Cross-process session restoration
- Planner implementations
- Multi-agent orchestration
- Workflow DAGs
- Parallel tool execution
- Automatic tool retries
- Full multimodal implementation
- Complex provider capability negotiation

## Package Layout

v1 should be implemented in a single repository with multiple packages:

- `provider`
- `provider/openai`
- `tool`
- `runtime`
- `agent`

Existing code in `agentruntime` should be migrated incrementally into these packages rather than rewritten all at once.

## Core Design Principles

1. Internal normalization should align with Responses-style semantics.
2. External compatibility must include both Chat and Responses transports.
3. Provider integration must remain open via a user-implemented interface.
4. Session state is in-memory runtime context only.
5. Runtime owns the execution loop; agent owns convenience and default policy.

## Provider Layer

### Responsibilities

The `provider` package is responsible for:

- defining the standard model request and response types
- exposing the provider interface used by runtime and agent
- normalizing vendor-specific responses into standard output items
- performing API retry for transient model call failures
- performing first-pass tool argument decoding and JSON repair

### Interface

The runtime-facing provider interface is:

```go
type Client interface {
	Generate(ctx context.Context, req ModelRequest) (*ModelResponse, error)
}
```

This is the only interface runtime requires. Users may provide their own implementation instead of using built-in adapters.

### Standard Request

`ModelRequest` should contain:

- `Model string`
- `Input []InputItem`
- `Tools []tool.Definition`
- `Options map[string]any`
- `Metadata map[string]any`

The request is transport-neutral. It must not expose Chat-only or Responses-only fields as required top-level concepts.

### Standard Response

`ModelResponse` should contain:

- `Output []OutputItem`
- `FinishReason string`
- `Usage Usage`
- `ResponseID string`
- `Provider string`
- `Raw any`

`Output` is the only model output surface runtime should depend on.

### Built-in Providers

v1 should ship with OpenAI adapters that implement `provider.Client`:

- `provider/openai` chat transport
- `provider/openai` responses transport

Both adapters normalize into the same `ModelResponse`.

### Retry Policy

Provider implementations should support configurable retry for transient API failures.

Default retry policy:

- max attempts: 3
- exponential backoff
- jitter enabled
- respect context cancellation and deadlines
- respect `Retry-After` when present

Retryable failures:

- network errors
- timeouts
- HTTP `429`
- HTTP `500`
- HTTP `502`
- HTTP `503`
- HTTP `504`

Non-retryable failures by default:

- HTTP `400`
- HTTP `401`
- HTTP `403`
- HTTP `404`
- deterministic local decoding failures

Retry belongs to the provider layer only. Runtime must not add its own model-call retry loop.

## Input and Output Items

### Input Items

Session state should be stored as normalized input items rather than raw chat messages.

v1 should support these item categories:

- system message item
- user message item
- assistant message item
- tool call item
- tool result item

Each item should include common fields:

- `Type string`
- `ID string`
- `Metadata map[string]any`

Message-like items should carry:

- `Content []ContentPart`

v1 should only implement text content parts, but the structure must allow future image and audio parts without renaming the core abstractions.

### Output Items

`ModelResponse.Output` should represent standardized model output. v1 should support:

- assistant message output
- tool call output

Tool call output should include:

- `CallID string`
- `Name string`
- `Arguments map[string]any`
- `RawArguments string`
- `Repaired bool`

`RawArguments` and `Repaired` are required for debugging, observability, and fallback handling.

## Tool Layer

### Responsibilities

The `tool` package is responsible for:

- tool definitions
- JSON-schema-like parameter definitions
- strong typed argument binding
- standard tool result and tool error encoding
- defensive validation before invoking a tool

### Interface

```go
type Definition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type CallContext struct {
	CallID    string
	Name      string
	Iteration int
	Metadata  map[string]any
}

type Result struct {
	Message string
	Data    map[string]any
}

type Tool interface {
	Definition() Definition
	Invoke(context.Context, CallContext, map[string]any) (Result, error)
}
```

Typed helpers should be supported:

```go
func NewStruct[T any](name, description string, handler StructHandler[T]) (Tool, error)
```

### Tool Errors

The tool layer should support a structured payload error type so runtime can encode standardized tool failures without guessing from plain strings.

Tool invocation failures are not runtime-fatal by default.

## JSON Repair

JSON repair is a required v1 feature because tool arguments from providers may arrive with:

- quoted JSON objects
- fenced code blocks
- malformed commas or brackets
- string-wrapped JSON

Repair should happen in two places:

1. provider layer normalization
2. tool layer defensive validation before binding

Provider-side repair should preserve:

- raw argument text
- repaired flag

If repair still fails, the failure should be treated as a bad tool call, not as a fatal provider response failure.

Suggested tool error codes:

- `invalid_tool_arguments`
- `tool_arguments_decode_failed`

## Runtime Layer

### Responsibilities

The `runtime` package is responsible for:

- session ownership
- execution loop
- tool call extraction and execution
- appending model and tool outputs back into session state
- stop and continue decisions
- usage aggregation
- lifecycle hooks

Runtime must not understand vendor-native protocol details after provider normalization.

### Session

`Session` holds in-memory conversational state only.

Suggested fields:

- `ID string`
- `Model string`
- `Items []provider.InputItem`
- `Usage Usage`
- `LastResponse *provider.ModelResponse`
- `Iteration int`
- `Metadata map[string]any`

Suggested methods:

- `NewSession(model string, opts ...SessionOption) *Session`
- `AppendUserText(text string)`
- `AppendItems(items ...provider.InputItem)`
- `ItemsView() []provider.InputItem`
- `MessagesView() []Message`
- `Snapshot() SessionSnapshot`

Session state must be externally visible but modified through controlled methods rather than arbitrary slice mutation.

### Runner

Suggested runtime interface:

```go
type Runner struct {
	Client provider.Client
	Hooks  Hooks
}

type RunRequest struct {
	Session          *Session
	Tools            []tool.Tool
	Options          map[string]any
	MaxModelCalls    int
	StopOnToolResult func(tool.Result) bool
	Metadata         map[string]any
}

type RunResult struct {
	Session        SessionSnapshot
	OutputText     string
	ModelCallCount int
	ToolCallCount  int
	StopReason     string
}
```

### Execution Flow

The runner flow should be:

1. read current session items
2. build `provider.ModelRequest`
3. call `provider.Client.Generate`
4. append model output into session
5. if no tool calls are present, stop
6. if tool calls are present, execute them in order
7. append tool results into session
8. continue or stop according to runtime policy

### Stop Policy

v1 stop reasons should include:

- `stop`
- `stop_on_tool_result`
- `model_call_limit_exceeded`
- `context_cancelled`
- `provider_error`

Tool errors should not stop the run by default. They should be encoded as tool result items and sent back into the loop.

### Tool Execution Policy

v1 should execute tool calls serially and in model-returned order.

Rationale:

- deterministic behavior
- simpler debugging
- avoids hidden dependency races

Parallel tool execution is out of scope for v1.

## Hooks and Observability

v1 should expose lifecycle hooks for:

- run start
- model request
- model response
- tool call start
- tool call finish
- run finish

Hook payloads should carry snapshots rather than mutable internal references.

Hook errors should abort the run by default. This is safer for audit, policy, and instrumentation use cases.

## Agent Layer

The `agent` package is a thin convenience layer on top of runtime.

It should own:

- model defaults
- instructions
- default tools
- default runtime options

It should not own:

- persistent memory
- planner logic
- orchestration logic

Suggested shape:

```go
type Agent struct {
	Model        string
	Instructions string
	Tools        []tool.Tool
	Client       provider.Client
	Runner       *runtime.Runner
	Options      map[string]any
}
```

`Agent.Run` should:

1. ensure a session exists
2. inject instructions on first use
3. append user input
4. call the runtime runner
5. return a result plus session snapshot

The agent layer exists to improve ergonomics, not to replace runtime as the core execution engine.

## Compatibility Strategy

v1 should support both:

- built-in OpenAI provider implementations
- user-provided custom provider implementations

Custom providers are expected to implement only `provider.Client`.

Optional capability-reporting interfaces may be added later, but they must not be required in v1.

## Migration Strategy

Migration from the current `agentruntime` package should proceed in phases:

### Phase 1

- replace `ChatRequest` / `ChatResponse` concepts with `ModelRequest` / `ModelResponse`
- introduce normalized input and output items
- keep existing runner behavior mostly intact

### Phase 2

- extract tool definitions, schema, binding, and payload errors into `tool`

### Phase 3

- extract provider abstractions
- implement OpenAI chat adapter
- implement OpenAI responses adapter
- add provider retry

### Phase 4

- refactor runtime to use `Session`
- expose snapshot and item views

### Phase 5

- add the thin `agent` layer

### Phase 6

- expand tests for transport differences, retries, JSON repair, and tool error continuation

## Testing Scope

v1 should include tests for:

- chat transport normalization
- responses transport normalization
- malformed tool arguments and repair
- retry behavior
- tool error continuation
- stop policies
- strong typed tool binding

## Positioning

Recommended one-line project description:

> A lightweight Go LLM agent runtime that standardizes model providers, supports tool calling, and enables continuous in-session execution with pluggable providers.

This description is intentionally narrower than a full agent platform claim and matches the actual v1 scope.
