# Engine Result And Usage Design

**Goal**

Tighten the public `engine` result contract so backend callers can reliably distinguish execution failure from business outcome, and expose run-level and session-level token usage including cached input tokens.

**Context**

`easyllm` now uses `engine` as the single public execution entrypoint. The next step is to stabilize the semantics of `Run(...)`, `RunResult`, `StopReason`, and usage accounting so application backends can depend on them without reverse-engineering internal behavior.

## Design Summary

The `engine` API keeps a strict split between execution errors and business results:

- `Run(ctx, req)` returns `error` only when the engine itself cannot complete execution.
- Any completed execution returns `RunResult`, even when tools fail, only partially succeed, or the engine stops because it hit the configured model call limit.

Usage is promoted to a first-class public type. It is aggregated across all model calls in a run and retained on both `RunResult` and session state.

## Public API Decisions

### `Run(...) error`

`error` represents execution failure only.

Examples:

- provider/model call failure
- hook failure
- invalid internal engine state

Non-examples:

- tool invocation failure
- partial business success
- reaching the configured max model call limit
- stopping immediately after a successful tool call

If `Run(...)` returns a non-nil `RunResult`, the engine completed successfully from the runtime's perspective.

### `StopReason`

The public stop reason contract is intentionally small and stable:

- `stop`
- `stop_on_tool_result`
- `model_call_limit_exceeded`

No separate stop reason is added for tool failure. Tool failures are business-level outcomes captured in `ToolResults`.

### `RunResult`

The public result shape should be:

```go
type RunResult struct {
    OutputText     string
    ToolResults    []ToolCallResult
    StopReason     string
    ModelCallCount int
    ToolCallCount  int
    Usage          Usage
    Session        SessionSnapshot
}
```

Field intent:

- `OutputText`: final assistant text captured during the run, if any
- `ToolResults`: ordered record of tool invocations and their outcomes
- `StopReason`: one of the stable stop reason values above
- `ModelCallCount`: total model invocations made during this run
- `ToolCallCount`: total tool calls issued by the model during this run
- `Usage`: aggregated token usage for this run
- `Session`: final in-memory session snapshot after the run completes

## Usage Contract

The public usage type should be:

```go
type Usage struct {
    InputTokens       int
    OutputTokens      int
    TotalTokens       int
    CachedInputTokens int
    Details           map[string]any
}
```

Field intent:

- `InputTokens`: sum of all input tokens consumed across model calls in the run or session
- `OutputTokens`: sum of all output tokens consumed across model calls in the run or session
- `TotalTokens`: sum of all total tokens consumed across model calls in the run or session
- `CachedInputTokens`: sum of all provider-reported cached input tokens across model calls
- `Details`: optional provider-specific usage details that do not belong in the cross-provider core contract

### Aggregation Rules

- `RunResult.Usage` contains the usage accumulated during the current run.
- `Session.Usage` contains the cumulative usage stored on the live session.
- `SessionSnapshot.Usage` contains the cumulative usage at snapshot time.
- Providers that do not report cached input token usage leave `CachedInputTokens` at `0`.
- Providers that do not report extra usage details leave `Details` empty or nil.

## Provider Boundary

The provider-neutral usage contract stays small. Providers may expose richer raw usage payloads internally, but the `engine` layer only depends on:

- input tokens
- output tokens
- total tokens
- cached input tokens
- optional provider-specific details

If a provider cannot produce one of these values, the engine falls back to zero values instead of failing execution.

## Runtime Behavior

The engine continues to append tool results to session state exactly as it does today. The semantic changes are:

- tool failures remain in `ToolResults` and do not become execution errors
- hitting the model call limit becomes a normal completed result with `StopReason = "model_call_limit_exceeded"`
- successful early exit after tool completion remains a normal completed result with `StopReason = "stop_on_tool_result"`
- token usage is aggregated per run and retained on session state

## Testing Requirements

Implementation should add or update tests that verify:

- `Run(...)` returns `error` only for execution failures
- tool failure still produces a `RunResult`
- `StopReason` values remain the three stable strings above
- `RunResult.Usage` aggregates usage across multiple model calls
- `Session.Usage` and `SessionSnapshot.Usage` preserve cumulative usage
- cached input tokens aggregate correctly when provider responses include them
- providers without cached token data still succeed with zero values

## Scope Boundaries

This design does not add:

- new request-level execution controls
- new stop reason variants
- workflow semantics beyond the existing tool loop
- provider-specific typed usage substructures beyond `Details`

The goal is to stabilize the current engine contract, not widen it.
