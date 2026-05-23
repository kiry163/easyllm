# OpenAI Image Generation Design

## Goal

Add a first-phase image generation capability to `easyllm` that is separate from the existing text-generation and engine orchestration APIs.

The first implementation target is OpenAI image generation with `b64_json` output. The design must preserve the current text client and engine behavior, keep the public API coherent, and leave a clean path for later provider expansion.

## Scope

This design covers:

- A new top-level image client API
- A dedicated image request and response model
- OpenAI image generation via `/images/generations`
- `b64_json` response handling
- Error handling, retry, and timeout behavior
- First-phase tests and documentation

This design does not cover:

- Image editing, masking, or variation APIs
- Engine integration
- Streaming image generation
- Session persistence for image outputs
- Multi-provider image support in phase one
- URL-based image return handling

## Requirements

### Functional

1. Existing `Client.Generate(...)` must remain unchanged.
2. Existing `Client.GenerateStream(...)` must remain unchanged.
3. Existing `Engine.Run(...)` and `Engine.RunStream(...)` must remain unchanged.
4. The library must expose an explicit image-generation entry point instead of overloading `ModelRequest`.
5. The first phase must support OpenAI image generation requests and parse `b64_json` image outputs.
6. Callers must be able to pass a small set of stable image options directly and provider-specific options through an escape hatch.
7. The image-generation API must return provider-neutral response types even though phase one only supports OpenAI.

### Non-Functional

1. The design must not pollute the existing text and tool-calling abstractions with media-specific fields.
2. Provider-specific request semantics must remain isolated in provider code.
3. The first phase must be narrow enough to implement safely in the current codebase.
4. The public API should be easy to understand from a small example.

## Public API

### Top-Level Constructor

Add a new top-level constructor:

```go
func NewImageClient(config Config) (ImageClient, error)
```

`NewClient(config)` remains the text-generation constructor. `NewImageClient(config)` is its image-generation counterpart.

Phase-one provider support:

- `ProviderOpenAI`: supported
- all other providers: return a clear unsupported-provider error

### Image Client Interface

Add a dedicated interface:

```go
type ImageClient interface {
    GenerateImage(context.Context, ImageRequest) (*ImageResponse, error)
}
```

This interface is intentionally separate from the existing `Client` interface. Image generation is treated as a parallel capability, not as an extension of the text model client.

### Request and Response Types

Public request and response types stay narrow:

```go
type ImageRequest struct {
    Prompt       string
    Model        string
    Size         string
    Quality      string
    Background   string
    OutputFormat string
    Count        int
    Options      map[string]any
}

type GeneratedImage struct {
    B64JSON       string
    RevisedPrompt string
}

type ImageResponse struct {
    Created  int64
    Images   []GeneratedImage
    Usage    Usage
    Provider string
    Raw      any
}
```

Phase-one rules:

- `Prompt` is required.
- `Model` should be resolved the same way the current text clients resolve model defaults: request value first, configured default second.
- `Count` defaults to `1` when unset or zero.
- `Options` is the provider-specific escape hatch for less stable request fields.
- `Images` may contain multiple outputs, even though most first-phase calls will likely request one image.
- `Config.ExtraBody` applies to the image client as provider-default request fields, and `ImageRequest.Options` overrides those defaults per request.

## Intended Usage

The first-phase user experience is a direct, single-call image API:

```go
client, err := easyllm.NewImageClient(easyllm.Config{
    Provider: easyllm.ProviderOpenAI,
    APIKey:   os.Getenv("OPENAI_API_KEY"),
    Model:    "gpt-image-1",
})

resp, err := client.GenerateImage(ctx, easyllm.ImageRequest{
    Prompt:       "A product-style photo of a ceramic mug on a wood desk",
    Size:         "1024x1024",
    Quality:      "high",
    Background:   "opaque",
    OutputFormat: "png",
    Count:        1,
})
```

The library returns `b64_json` data and does not write files, upload assets, or manage session state. The caller is responsible for decoding and storing the generated image bytes.

## Architecture

### 1. Public API Layer

The public `easyllm` package defines:

- `ImageClient`
- `ImageRequest`
- `GeneratedImage`
- `ImageResponse`
- `NewImageClient(config)`

This layer contains no OpenAI-specific field names beyond the top-level provider selection.

### 2. Internal Model Layer

Add image-specific internal types in `internal/model`:

- `ImageClient`
- `ImageRequest`
- `GeneratedImage`
- `ImageResponse`

This mirrors the existing text-side split between public aliases and internal model types. The image types remain separate from `ModelRequest` and `ModelResponse`.

### 3. Provider Layer

Phase one adds an OpenAI-specific image client implementation under `internal/provider/openai`.

Responsibilities:

- construct the `/images/generations` request body
- resolve the effective model
- merge explicit image fields with provider-specific options
- send the HTTP request with existing retry and timeout behavior
- parse `created`, `data[].b64_json`, `data[].revised_prompt`, and usage fields

The provider layer owns all OpenAI-specific request semantics. The public layer only carries stable image-generation concepts.

## Request Mapping

Phase one targets OpenAI image generation through `POST /images/generations`.

The effective request body is built from:

1. resolved model
2. client-level `Config.ExtraBody`
3. explicit `ImageRequest` fields
4. provider-specific `ImageRequest.Options`, merged last so they can override explicit fields and client defaults

Expected direct field mapping:

- `Prompt` -> `prompt`
- resolved `Model` -> `model`
- `Size` -> `size`
- `Quality` -> `quality`
- `Background` -> `background`
- `OutputFormat` -> `output_format`
- `Count` -> `n`

Design choices:

- The library does not expose enums for `size`, `quality`, `background`, or `output_format` in phase one.
- The library does not try to normalize provider-specific value vocabularies.
- `Config.ExtraBody` provides image-client-level provider defaults, while `ImageRequest.Options` is the per-request escape hatch for less stable or provider-private parameters.

## Response Mapping

The provider implementation maps the OpenAI response into `ImageResponse`.

Expected fields:

- top-level `created` -> `ImageResponse.Created`
- each `data[]` item:
  - `b64_json` -> `GeneratedImage.B64JSON`
  - `revised_prompt` -> `GeneratedImage.RevisedPrompt`
- provider usage fields -> `ImageResponse.Usage`
- full decoded provider response -> `ImageResponse.Raw`

Phase-one response constraints:

- Only `b64_json` is modeled as a first-class image payload.
- URL outputs are out of scope.
- No image bytes are decoded automatically by the library.

## Validation and Defaults

Validation should stay minimal and predictable.

Configuration-level validation:

- `Provider` is required.
- `APIKey` is required for `ProviderOpenAI`.
- `Model` should follow the same configured-default expectation as the current text client constructor.

Request-level validation:

- `Prompt` is required.
- `Count <= 0` is normalized to `1`.

The library should avoid aggressive validation of provider value strings in phase one. If a caller passes an unsupported `size` or `quality`, the provider should be allowed to return its normal API error.

## Error Handling

The image client should follow the current provider behavior style.

Examples of returned errors:

- invalid config passed to `NewImageClient(...)`
- invalid request such as missing prompt
- network or timeout failure
- provider non-2xx response
- malformed provider response

Phase-one rules:

- provider non-2xx responses should include the HTTP status and a bounded preview of the response body
- timeout and retry handling should reuse the existing OpenAI provider infrastructure
- the library does not attempt any semantic validation of image content

## Testing

### Top-Level Tests

Add tests in the top-level package for:

- successful `NewImageClient(...)` construction for `ProviderOpenAI`
- unsupported-provider errors for other built-in providers
- config validation behavior

### OpenAI Provider Tests

Add tests for:

- request path `/images/generations`
- mapping of explicit request fields
- model resolution behavior
- `Count` defaulting to `1`
- `Options` overriding explicit request fields
- parsing `created`
- parsing `b64_json`
- parsing `revised_prompt`
- parsing usage
- retry behavior
- timeout behavior
- non-2xx provider errors

## Documentation

Update `README.md` with a focused image-generation section that includes:

- what `NewImageClient(config)` is for
- that phase one supports OpenAI image generation only
- that the response currently returns `b64_json`
- a minimal usage example
- a short note that image generation is intentionally separate from `Engine`

## Future Extension Path

This design intentionally keeps a clean path for later expansion without forcing it into phase one.

Likely future additions:

- OpenAI image edit support
- URL output handling when there is a clear need
- provider-specific implementations beyond OpenAI

Those can be added on top of the image-specific client abstraction without changing the text client or engine APIs.
