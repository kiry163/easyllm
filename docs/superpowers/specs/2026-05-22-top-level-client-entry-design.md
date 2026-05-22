# Top-Level Client Entry Design

**Goal**

Introduce a single top-level client construction path so users can get started with `easyllm` without choosing a provider subpackage up front.

## Context

The current API exposes multiple provider entry points such as:

- `provider/openai`
- `provider/qwen`
- `provider/deepseek`

That flexibility is useful internally, but it increases user choice cost too early. New users must decide which provider package to import and which factory to call before they can make their first request.

This conflicts with the product goal implied by the library name: the default path should be easy, and provider-specific choices should be deferred until the user actually needs them.

At the same time, the existing architecture already has a useful separation:

- `engine` consumes a client abstraction
- provider packages implement that abstraction

That means the user-facing entry can be simplified without redesigning the runtime.

## Design Summary

Add a top-level constructor:

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: "openai",
	APIKey:   os.Getenv("OPENAI_API_KEY"),
	BaseURL:  "https://api.openai.com/v1",
	Model:    "gpt-4.1-mini",
})
```

This constructor becomes the default documented entry point.

Provider subpackages remain available, but they are demoted to advanced usage rather than first-class getting-started APIs.

## Public API

### Top-Level Constructor

Add a new top-level constructor in the root package:

```go
func NewClient(config Config) (Client, error)
```

The constructor validates the config, selects the correct provider implementation, and returns the configured client.

### Top-Level Config

Add a new root-level config type:

```go
type Config struct {
	Provider    string
	APIKey      string
	BaseURL     string
	Model       string
	Transport   string
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
	Timeout     time.Duration
	MaxAttempts int
	Options     map[string]any
}
```

This config is intentionally split into:

- explicit high-frequency cross-provider fields
- `Options` for provider-specific extensions

`Transport` is optional. The zero value maps to `chat`.

### Supported Provider Values

In the initial version, `Config.Provider` accepts:

- `openai`
- `qwen`
- `deepseek`

These are explicit product-level identifiers, not protocol-family labels.

## Provider-Specific Options

Provider-specific capabilities are passed through `Options`.

Example:

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: "qwen",
	APIKey:   os.Getenv("DASHSCOPE_API_KEY"),
	Model:    "qwen-plus",
	Options: map[string]any{
		"thinking": false,
	},
})
```

The top-level constructor is responsible for mapping supported option keys into the corresponding provider client config.

In the initial version, only clearly supported keys should be accepted. Unknown provider-specific options should return a validation error instead of being silently ignored.

## Validation Rules

The top-level constructor validates:

- `Provider` must be one of the supported values
- `Transport` must be supported by the selected provider
- `APIKey` is required
- `Model` is required
- `BaseURL` is optional when the selected provider has a default base URL
- provider-specific `Options` keys must be recognized
- provider-specific `Options` values must have the expected types

`Timeout` and `MaxAttempts` are optional. Zero values mean the provider package defaults remain in effect.

In the initial version:

- `openai` supports `chat` and `responses`
- `qwen` supports `chat` and `responses`
- `deepseek` supports `chat` only

## Engine Boundary

This change does not alter the engine contract.

`engine.New(...)` continues to accept the same client abstraction as before. The top-level constructor only changes how the client is built, not how the engine consumes it.

This preserves the current layering:

- root package chooses a provider implementation
- provider package constructs the concrete client
- engine uses the returned client through the existing abstraction

## Package Roles

### Root Package

The root package becomes the primary user entry point for:

- choosing a provider
- configuring common settings
- constructing a client

### Provider Packages

Provider packages remain in the repository, but their role shifts:

- implementation detail for top-level dispatch
- advanced usage path for users who need direct provider control

They should no longer be the primary path shown in the README quick start.

### Engine Package

The engine package remains unchanged in responsibility and public shape for this design phase.

## Documentation Positioning

README should be reorganized around the new default path:

1. Quick start uses `easyllm.NewClient`
2. Tool calling and engine examples also use the top-level constructor
3. Provider subpackages move to an advanced section

`qwen` and `deepseek` should not be presented as first-class getting-started entry points anymore.

## Error Handling

`NewClient` returns an error for:

- unsupported provider names
- missing required fields
- invalid provider-specific option names
- invalid provider-specific option types

This keeps misconfiguration failures early and explicit.

## Implementation Boundaries

This design includes:

- root-level `Config`
- root-level `NewClient`
- provider dispatch and config translation
- tests for validation and provider mapping
- README/examples/CLI updates toward the top-level path

This design does not include:

- redesigning engine behavior
- removing provider subpackages
- adding every possible provider-specific option

## Result

After this change, the default user flow becomes:

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: "openai",
	APIKey:   os.Getenv("OPENAI_API_KEY"),
	Model:    "gpt-4.1-mini",
})
if err != nil {
	panic(err)
}

rt := engine.New(client)
```

Users get one obvious starting point, while the existing provider architecture remains intact underneath.
