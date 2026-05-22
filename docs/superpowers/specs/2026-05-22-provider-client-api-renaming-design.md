# Provider Client API Renaming Design

**Goal**

Rename provider factory methods and config types so the public API reflects that these constructors return executable model clients, not model descriptors.

**Context**

The current provider API uses names such as `ChatModel(...)` and `ChatModelConfig`, but these factories actually return `provider.ModelClient`. That creates a semantic mismatch:

- `ChatModel(...)` reads like a pure model-description accessor
- the return value is actually a configured client with behavior
- downstream code ends up naming executable clients as `model`, which weakens API clarity

`easyllm` does not currently need compatibility shims for external callers, so this is a good time to make the public API precise.

## Design Summary

Rename provider factory methods from `*Model` to `*Client`, and rename matching config types from `*ModelConfig` to `*ClientConfig`.

This is a naming-only API cleanup. No behavior changes are introduced.

## Public API Changes

### Method Renames

Rename the public provider methods as follows:

- `ChatModel(...)` -> `ChatClient(...)`
- `ResponsesModel(...)` -> `ResponsesClient(...)`

This applies to all providers that currently expose these methods:

- `provider/openai`
- `provider/qwen`
- `provider/deepseek`

### Config Type Renames

Rename the public config types as follows:

- `ChatModelConfig` -> `ChatClientConfig`
- `ResponsesModelConfig` -> `ResponsesClientConfig`

The fields inside these config structs stay unchanged. This is a semantic rename only.

## Scope Boundaries

This change includes:

- provider package public API renames
- internal provider implementation updates needed to compile
- tests updated to use the new names
- examples, README, and CLI updated to use the new names

This change does not include:

- compatibility aliases
- deprecated wrappers
- behavior changes in transport selection, retry, timeout, or request shaping
- renaming unrelated interfaces such as `provider.ModelClient`

## Naming Rationale

The new names align with the actual abstraction boundary:

- `Provider` owns vendor-level connection configuration
- `ChatClient(...)` and `ResponsesClient(...)` construct executable clients
- `provider.ModelClient` remains the provider-neutral runtime interface consumed by `engine`

This makes code read more honestly:

```go
p := openai.NewProvider(...)
client := p.ChatClient(openai.ChatClientConfig{
	Model: "gpt-4.1-mini",
})
rt := engine.New(client)
```

That is clearer than assigning the return value of `ChatModel(...)` to a variable named `model` when the value is actually a client.

## Affected Packages

Expected direct code changes:

- `provider/openai/client.go`
- `provider/qwen/client.go`
- `provider/deepseek/client.go`
- provider tests under those packages
- `cmd/easyllm-call/main.go`
- `examples/examples_test.go`
- `README.md`

## Testing Requirements

Implementation must verify:

- all provider package tests compile and pass with the new API names
- CLI and examples compile with the renamed methods and config types
- no runtime behavior changes occur in the existing provider tests
- repository-wide tests still pass

## Migration Decision

No compatibility layer will be kept.

Because there are no known external callers depending on the old names, the library should switch directly to the new API instead of carrying aliases or deprecated wrappers that would delay cleanup.

## Result

After this change, the provider-facing API will consistently express that callers are constructing executable clients:

- `ChatClient(ChatClientConfig)`
- `ResponsesClient(ResponsesClientConfig)`

The engine-facing runtime contract stays unchanged.
