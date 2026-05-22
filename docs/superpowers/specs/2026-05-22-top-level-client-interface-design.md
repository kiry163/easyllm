# Top-Level Client Interface Design

**Goal**

Move the core runtime client abstraction from the `provider` package to the root package so the public API reflects that `easyllm` itself owns the main client contract.

## Context

The library now exposes a top-level entry point:

```go
client, err := easyllm.NewClient(easyllm.Config{...})
```

That makes the current return type name `provider.ModelClient` feel misplaced. The root package is now the main public API for constructing clients, while `engine` consumes the same abstraction at runtime.

The interface itself is still correct:

```go
Generate(context.Context, ModelRequest) (*ModelResponse, error)
```

The problem is ownership and naming:

- `provider.ModelClient` suggests the abstraction belongs to provider implementations
- in practice it is the core runtime contract of the whole library
- the default public path is now rooted in `easyllm`, not in `provider`

## Design Summary

Introduce a root-level interface:

```go
type Client interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
}
```

Then migrate the public API so:

- `easyllm.NewClient(...)` returns `easyllm.Client`
- `engine.New(...)` accepts `easyllm.Client`
- provider packages continue to return their local `provider.Client` shape to avoid an import cycle, while root-level APIs expose `easyllm.Client`

The old `provider.ModelClient` name is removed from production code. The lower-level `provider.Client` interface remains as the provider package's internal-facing contract because provider subpackages cannot import the root package without creating an import cycle.

For this repository, no external compatibility guarantee is currently required, so no deprecated `ModelClient` compatibility alias is kept.

## Public API Changes

### Root-Level Types

Add to the root package:

```go
type Client interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
}
```

The root package should also expose the request and response model types users need alongside the interface. The goal is that common application code can stay in the root package for the main contract instead of importing `provider` just to name core runtime types.

### Root-Level Constructor

Update:

```go
func NewClient(config Config) (Client, error)
```

### Engine Constructor

Update `engine.New(...)` so its first parameter is `easyllm.Client`.

This is a contract ownership change, not a behavior change.

### Provider Packages

Provider package constructors return `provider.Client`. This interface has the same method shape as `easyllm.Client`, so returned values satisfy the root interface when passed through `easyllm.NewClient` or `engine.New`.

This applies to:

- `provider/openai`
- `provider/qwen`
- `provider/deepseek`

## Type Ownership

The central design decision is:

- the public client abstraction belongs to the product root package
- provider packages expose a lower-level same-shaped implementation contract
- engine depends on that abstraction

This is a better public ownership model than letting the provider layer define the main runtime contract, while still preserving acyclic package dependencies.

## Scope Boundaries

This design includes:

- introducing `easyllm.Client`
- updating `easyllm.NewClient` to return `easyllm.Client`
- updating `engine` to depend on `easyllm.Client`
- removing `provider.ModelClient`
- updating tests, examples, CLI, and docs to use the new interface name

This design does not include:

- changing the behavior of `Generate`
- redesigning request or response semantics
- changing tool-calling behavior
- changing provider transport behavior

## Request And Response Types

There are two reasonable paths:

1. Move request and response types fully into the root package
2. Keep the structs in `provider` for now and use root-level aliases

For this phase, the preferred path is root-level aliases to keep the change narrowly scoped:

```go
type ModelRequest = provider.ModelRequest
type ModelResponse = provider.ModelResponse
type InputItem = provider.InputItem
type OutputItem = provider.OutputItem
```

Likewise for commonly used concrete request and response item types that appear in public examples.

This lets the interface live in the root package immediately without forcing a large structural move of all request and response types in the same change.

## Migration Strategy

Perform the migration in two layers:

### Phase 1

Create root-level aliases for the request/response types and define `easyllm.Client` in terms of those aliases.

### Phase 2

Switch constructors and function signatures to return or accept `easyllm.Client`.

This keeps the rename focused and minimizes behavioral risk.

## Engine Boundary

The engine package still should not know about specific providers.

Its dependency becomes:

- `easyllm.Client`

instead of:

- `provider.ModelClient`

That preserves the same architectural layering while putting the contract in the correct place.

## Provider Package Positioning

After this change, provider packages become even more clearly implementation packages:

- they expose advanced configuration and vendor-specific constructors
- they return `provider.Client`, which is compatible with the root `easyllm.Client` contract
- they no longer define the library's central public runtime interface

## Documentation Positioning

After this change, public examples should read consistently:

```go
client, err := easyllm.NewClient(...)
rt := engine.New(client)
```

Users should not need to reason about `provider.ModelClient` in standard documentation anymore.

## Result

After this change:

- the main client abstraction is `easyllm.Client`
- the main constructor returns `easyllm.Client`
- the engine consumes `easyllm.Client`
- provider packages return `provider.Client` as their lower-level contract

The runtime contract becomes aligned with the actual public product surface.
