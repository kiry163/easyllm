# easyllm

`easyllm` is a lightweight Go library for building LLM-backed backend workflows with provider-neutral model calls, tool calling, hooks, and in-memory sessions.

The public API is intentionally centered on one import:

- `easyllm.NewClient(...)` creates a provider-backed model client.
- `easyllm.NewEngine(...)` runs instructions, tools, sessions, hooks, and the tool-calling loop.
- `easyllm.NewTool(...)` turns typed Go structs and runner methods into model-callable tools.
- `easyllm.NewEmbeddingClient(...)` creates embeddings through the OpenAI protocol.
- `easyllm.NewRerankClient(...)` reranks text documents with Qwen3-Rerank.

## Install

```bash
go get github.com/kiry163/easyllm
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kiry163/easyllm"
)

func main() {
	client, err := easyllm.NewClient(easyllm.Config{
		Provider: easyllm.ProviderOpenAI,
		APIKey:   os.Getenv("OPENAI_API_KEY"),
		Model:    "gpt-4.1-mini",
	})
	if err != nil {
		panic(err)
	}

	engine := easyllm.NewEngine(
		client,
		easyllm.WithInstructions("Answer clearly and concisely."),
	)

	result, err := engine.Run(context.Background(), easyllm.RunRequest{
		Input: "Explain recursion in one sentence.",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
}
```

## Providers

Built-in providers are selected through `easyllm.Config.Provider`:

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: easyllm.ProviderQwen,
	APIKey:   os.Getenv("DASHSCOPE_API_KEY"),
	Model:    "qwen-plus",
})
```

Supported provider constants:

- `easyllm.ProviderOpenAI`
- `easyllm.ProviderQwen`
- `easyllm.ProviderDeepSeek`
- `easyllm.ProviderOpenAICompatible`

OpenAI, Qwen, and DeepSeek use built-in default base URLs. Set `BaseURL` when you need a custom endpoint.

Use `ListModels(...)` to fetch the provider's remote `/models` list. This call does not require `Config.Model`.

```go
models, err := easyllm.ListModels(ctx, easyllm.Config{
	Provider: easyllm.ProviderOpenAI,
	APIKey:   os.Getenv("OPENAI_API_KEY"),
})
if err != nil {
	panic(err)
}

for _, model := range models.Models {
	fmt.Println(model.ID)
}
```

Common model request fields live directly on `Config`:

```go
temperature := 0.6
topP := 0.7
maxTokens := 512
enableThinking := false

client, err := easyllm.NewClient(easyllm.Config{
	Provider:                easyllm.ProviderQwen,
	APIKey:                  os.Getenv("DASHSCOPE_API_KEY"),
	Model:                   "qwen-plus",
	Temperature:             &temperature,
	TopP:                    &topP,
	MaxTokens:               &maxTokens,
	EnableThinking:          &enableThinking,
	Timeout:                 2 * time.Minute,
	StreamFirstEventTimeout: 20 * time.Second,
	MaxRetries:              3,
	InitialBackoff:          200 * time.Millisecond,
	MaxBackoff:              time.Second,
})
```

Retry behavior is configured from the same top-level `Config`. `MaxRetries` controls the number of extra retries after the first attempt, and backoff grows exponentially from `InitialBackoff` up to `MaxBackoff`.

`Timeout` is the total timeout for one HTTP request. For streaming requests, `StreamFirstEventTimeout` controls how long the client may wait for the first SSE event before it aborts with `easyllm.ErrStreamFirstEventTimeout`. If that happens before any stream event is emitted, the request can be retried according to `MaxRetries`.

`Engine.Run(...)` and `Engine.RunStream(...)` wrap execution failures in `EngineError` so callers can branch on `Kind` while preserving the original error through `errors.Is` and `errors.As`:

```go
result, err := engine.Run(ctx, req)
if err != nil {
	var engineErr *easyllm.EngineError
	if errors.As(err, &engineErr) {
		switch engineErr.Kind {
		case easyllm.EngineErrorTimeout:
			// request timed out
		case easyllm.EngineErrorModelRequest:
			// provider request failed
		}
	}
}
```

`EnableThinking` is currently a provider-level compatibility flag. Built-in providers map it only when they support a provider-specific thinking switch. Today that mainly applies to Qwen and DeepSeek.

Thinking mode and forced tool selection are incompatible on several OpenAI-compatible providers: DashScope/Qwen and DeepSeek reject `tool_choice: "required"` (and the object form that forces a specific tool) with an HTTP 400 while thinking is enabled. When an assembled request explicitly enables thinking and also forces a tool, `easyllm` automatically drops the forced choice so the provider default (automatic selection) applies; thinking stays enabled and the model is normally still able to call the tool. The adaptation keys off explicit request flags only: `enable_thinking: true` (Qwen / DashScope-style endpoints), `thinking: {"type":"enabled"}` (DeepSeek chat) and `reasoning: {"effort":"<not none>"}` (DeepSeek responses). It applies on the chat, responses, and streaming transports alike. Models that enable thinking by default without one of these flags on the wire are not detected; force a specific tool there by disabling thinking explicitly.

Provider-specific request body fields can be passed through `ExtraBody`. Values in `ExtraBody` are merged after normalized config fields, so they can override the final request body.

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: easyllm.ProviderQwen,
	APIKey:   os.Getenv("DASHSCOPE_API_KEY"),
	Model:    "qwen-plus",
	ExtraBody: map[string]any{
		"enable_thinking": false,
		"custom_flag":     true,
	},
})
```

DeepSeek keeps its reasoning controls provider-local instead of adding more top-level `Config` fields. The chat transport configures thinking through `thinking` / `reasoning_effort`:

- Default request body includes `thinking: {"type":"enabled"}`.
- Default request body includes `reasoning_effort: "high"` while thinking is enabled.
- Setting `EnableThinking` to `false` switches the default thinking payload to `{"type":"disabled"}`.
- `ExtraBody` can override either field when you need a provider-specific value.

Example:

```go
enableThinking := false

client, err := easyllm.NewClient(easyllm.Config{
	Provider:        easyllm.ProviderDeepSeek,
	APIKey:          os.Getenv("DEEPSEEK_API_KEY"),
	Model:           "deepseek-v4-flash",
	EnableThinking:  &enableThinking,
	ExtraBody: map[string]any{
		"thinking":         map[string]any{"type": "disabled"},
		"reasoning_effort": "low",
	},
})
```

DeepSeek also serves the responses transport. It is stateless (`store: false`, no `previous_response_id`), takes the full conversation in `input`, and configures thinking through `reasoning.effort` instead of the chat fields:

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider:  easyllm.ProviderDeepSeek,
	APIKey:    os.Getenv("DEEPSEEK_API_KEY"),
	Model:     "deepseek-flash",
	Transport: easyllm.TransportResponses,
	ExtraBody: map[string]any{
		"reasoning": map[string]any{"effort": "low"},
	},
})
```

`EnableThinking` maps onto that field as follows:

| `EnableThinking` | chat transport | responses transport |
| --- | --- | --- |
| `nil` | `thinking:{"type":"enabled"}` + `reasoning_effort:"high"` | `reasoning:{"effort":"high"}` |
| `true` | same as `nil` | same as `nil` |
| `false` | `thinking:{"type":"disabled"}` | `reasoning:{"effort":"none"}` |

Forced tool selection is rejected while thinking is on, on both transports: the chat transport is detected through `thinking` / `enable_thinking`, the responses transport through a `reasoning.effort` other than `none`. The responses rule is scoped to that transport, so a chat request that carries its own `reasoning` object (OpenRouter, for example) keeps its forced `tool_choice`.

When DeepSeek returns tool calls in thinking mode, `easyllm` preserves the provider-required continuation state across the tool loop automatically: the follow-up request replays the chain on the assistant message — `reasoning_content` on the chat transport, a `reasoning` item on the responses transport — without any caller-side work.

Reasoning text is a typed field rather than opaque provider state. `AssistantOutput.ReasoningContent` holds the chain a response produced, and an `AssistantMessageItem` passed back through `ModelRequest.Input` writes its `ReasoningContent` back to the provider:

```go
resp, err := client.Generate(ctx, req)
assistant := resp.Output[0].(easyllm.AssistantOutput)
fmt.Println(assistant.ReasoningContent)
```

Streaming reports the chain while it is produced through `StreamEventReasoningDelta` events; concatenating their `Text` yields the same string as the final `AssistantOutput.ReasoningContent`. Providers that emit no reasoning — including DeepSeek with `reasoning.effort` set to `none` — leave both the field and the event stream empty.

Responses calls also surface `FinishReason`: `stop`, `tool_calls`, `length` for a response truncated by `max_output_tokens`, or the provider's `content_filter` reason. A response that ends in `failed` is returned as an error, and `usage.output_tokens_details.reasoning_tokens` is reported as `Usage.Details["reasoning_tokens"]`.

## HTTP Middleware

Use `HTTPMiddleware` to wrap every built-in provider HTTP attempt. Middleware applies to ordinary and streaming model calls, image generation, model listing, and retries.

```go
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

middleware := func(next easyllm.HTTPDoer) easyllm.HTTPDoer {
	return doerFunc(func(req *http.Request) (*http.Response, error) {
		// Wait, trace, or audit before the real HTTP request starts.
		return next.Do(req)
	})
}

client, err := easyllm.NewClient(easyllm.Config{
	Provider:       easyllm.ProviderOpenAI,
	APIKey:         os.Getenv("OPENAI_API_KEY"),
	Model:          "gpt-4.1-mini",
	Timeout:        30 * time.Second,
	HTTPMiddleware: middleware,
})
```

Work performed before `next.Do` is outside `Config.Timeout`, but remains subject to the request context. `Config.Timeout` starts when the wrapped easyllm HTTP client begins the real request and continues through response-body reading.

For streaming or concurrency tracking, wrap `resp.Body` and finish bookkeeping when the body reaches EOF or is closed. Middleware must be safe for concurrent use and must call the supplied `next.Do` to preserve easyllm's timeout and connection reuse.

## Engine

An `Engine` is the runtime layer around a model client. It owns instructions, tools, hooks, model-call limits, and the tool-calling loop.

```go
engine := easyllm.NewEngine(
	client,
	easyllm.WithInstructions("Answer as a concise backend assistant."),
	easyllm.WithMaxModelCalls(3),
)

result, err := engine.Run(ctx, easyllm.RunRequest{
	Input: "Summarize this incident.",
})
```

### Current Time

Enable current-time injection when the model needs an explicit clock. This
feature applies only to `Engine.Run(...)` and `Engine.RunStream(...)`; direct
`Client.Generate(...)` calls are unchanged.

```go
location, err := time.LoadLocation("Asia/Shanghai")
if err != nil {
	panic(err)
}

engine := easyllm.NewEngine(
	client,
	easyllm.WithCurrentTime(location),
)
```

The engine reads the clock once at the start of each run and injects a temporary
system message such as
`Current date and time: 2026-07-20T15:30:45+08:00 (Asia/Shanghai).` Every model
call in the same tool loop sees that same time. A later run reads the clock
again, including when it reuses the same session.

The injected message is included in `OnModelRequest`, but it is not stored in
`Session.Items` or session snapshots. Current-time injection is disabled unless
`WithCurrentTime(...)` is supplied. A nil location means the host's local time
zone (`time.Local`, which follows `TZ`); the zone name in the injected message is
then `Local`.

For applications serving users in different time zones, override the engine
location for an individual run:

```go
result, err := engine.Run(ctx, easyllm.RunRequest{
	Input:               "How long until the end of today?",
	CurrentTimeLocation: userLocation,
})
```

`Run(...)` may perform multiple model calls when tools are involved:

```text
easyllm.Engine.Run
  -> easyllm.Client.Generate
  -> easyllm.Tool.Invoke
  -> easyllm.Client.Generate
```

### Cost Tracking

`WithPricing(...)` prices every model call and reports the result on
`RunResult.Usage`. Costs are plain `float64` amounts in a currency you choose;
the library performs no conversion.

```go
engine := easyllm.NewEngine(
	client,
	easyllm.WithPricing(easyllm.SimplePricing(easyllm.Rates{
		Input:       2, // per million tokens
		Output:      8,
		CachedInput: 0.5,
	})),
)

result, err := engine.Run(ctx, easyllm.RunRequest{Input: "hello"})
fmt.Println(result.Usage.TotalCost)
```

Cached input tokens are a subset of input tokens, so `InputCost` covers the
uncached remainder only:

```text
InputCost       = (InputTokens - CachedInputTokens) * Input       / 1e6
CachedInputCost = CachedInputTokens                 * CachedInput / 1e6
OutputCost      = OutputTokens                      * Output      / 1e6
TotalCost       = InputCost + OutputCost + CachedInputCost
```

`TotalCost` is derived and always equals the sum of the three components.

#### Peak and Off-Peak Rates

`NewPricing(...)` adds recurring time windows that override the base rates. The
first matching window wins. Windows are evaluated in the host's local time zone
unless `Location` says otherwise.

```go
pricing, err := easyllm.NewPricing(easyllm.PricingConfig{
	Rates: easyllm.Rates{Input: 2, Output: 8, CachedInput: 0.5}, // peak
	Windows: []easyllm.RateWindow{{
		Start: "00:30",
		End:   "08:30",
		Rates: easyllm.Rates{Input: 1, Output: 4, CachedInput: 0.25}, // off-peak
	}},
})
if err != nil {
	panic(err)
}

engine := easyllm.NewEngine(client, easyllm.WithPricing(pricing))
```

`Start` is inclusive and `End` is exclusive. An `End` at or before `Start` wraps
past midnight, so `"22:00"`–`"06:00"` covers one night. `"24:00"` is accepted as
`End` for a whole-day window. `Weekdays` limits a window to the days it opens on:
a window opening Monday 22:00 also covers Tuesday 02:00, while Monday 02:00
belongs to Sunday and does not match.

```go
easyllm.RateWindow{
	Start:    "22:00",
	End:      "06:00",
	Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
	Rates:    easyllm.Rates{Input: 1, Output: 4, CachedInput: 0.25},
}
```

`NewPricing(...)` rejects malformed windows, negative rates, and out-of-range
weekdays, so a bad price list fails at startup rather than mid-run.

The location is read once, while the price list is compiled. Leaving the windows
on the local zone ties cost to the deployment host: a container with `TZ` unset,
or set to an empty value, runs in UTC and its off-peak window moves with it. Pin
`Location` (or `TZ`) when the same price list must produce the same cost
everywhere.

#### Loading Pricing From JSON

```json
{
  "location": "Asia/Shanghai",
  "rates": {"input": 2, "output": 8, "cached_input": 0.5},
  "windows": [
    {
      "start": "00:30",
      "end": "08:30",
      "weekdays": [1, 2, 3, 4, 5],
      "rates": {"input": 1, "output": 4, "cached_input": 0.25}
    }
  ]
}
```

```go
pricing, err := easyllm.NewPricingJSON(text)
```

`weekdays` uses `time.Weekday` numbers, `0` for Sunday through `6` for Saturday.
An omitted `location` uses the host's local time zone. Unknown fields are
rejected, so a misspelled key cannot silently fall back to base rates.

#### Custom Pricing

Implement `Pricer`, or wrap a function with `PricerFunc`, when prices come from
another source or depend on more than time. `Model` and `Provider` are set only
when the client reports them.

```go
engine := easyllm.NewEngine(client, easyllm.WithPricing(easyllm.PricerFunc(
	func(in easyllm.CostInput) easyllm.Cost {
		return myPriceList.Lookup(in.Model, in.Time)
	},
)))
```

A `Pricer` returns a zero `Cost` for anything it cannot price: a missing price
must not fail a run that has already spent tokens.

Costs aggregate exactly like tokens. Every model call is priced at the instant
it completes, so a tool loop that crosses a rate boundary is charged per call.
`RunResult.Usage` covers the current run; `Session.Usage` and
`RunResult.Session.Usage` accumulate across the session.

### Multimodal Input

Use `InputParts` when a user message needs text plus image resources. Image URLs may be remote URLs or provider-supported data URLs.
`Input` and `InputParts` are mutually exclusive; when using images, include the text prompt as a text part.

```go
result, err := engine.Run(ctx, easyllm.RunRequest{
	InputParts: []easyllm.ContentPart{
		easyllm.NewTextPart("Describe this image."),
		easyllm.NewImagePart("https://example.com/cat.png", easyllm.ImageDetailAuto),
	},
})
if err != nil {
	panic(err)
}

fmt.Println(result.OutputText)
```

For direct model calls, pass the same parts through `UserMessageItem.Content`:

```go
resp, err := client.Generate(ctx, easyllm.ModelRequest{
	Input: []easyllm.InputItem{
		easyllm.UserMessageItem{
			Content: []easyllm.ContentPart{
				easyllm.NewTextPart("What is in this image?"),
				easyllm.NewImagePart("data:image/png;base64,...", easyllm.ImageDetailLow),
			},
		},
	},
})
```

## Streaming

Use `GenerateStream(...)` for a single streaming model call and `RunStream(...)` when you want the engine to keep handling sessions and tool calls.

```go
err = client.GenerateStream(ctx, easyllm.ModelRequest{
	Input: []easyllm.InputItem{
		easyllm.UserMessageItem{
			Content: []easyllm.TextPart{{Text: "Say hello."}},
		},
	},
}, func(event easyllm.StreamEvent) error {
	switch event.Type {
	case easyllm.StreamEventMessageDelta:
		fmt.Print(event.Text)
	case easyllm.StreamEventDone:
		fmt.Println()
	}
	return nil
})
if err != nil {
	panic(err)
}
```

`RunStream(...)` adds tool lifecycle events on top of text deltas:

```go
result, err := engine.RunStream(ctx, easyllm.RunRequest{
	Input: "Check the weather in Hangzhou.",
}, func(event easyllm.StreamEvent) error {
	switch event.Type {
	case easyllm.StreamEventReasoningDelta:
		fmt.Print(event.Text) // chain of thought, when the provider emits one
	case easyllm.StreamEventMessageDelta:
		fmt.Print(event.Text)
	case easyllm.StreamEventToolStart:
		fmt.Printf("\n[tool start] %s\n", event.ToolName)
	case easyllm.StreamEventToolFinish:
		fmt.Printf("[tool finish] %s\n", event.ToolName)
	case easyllm.StreamEventDone:
		fmt.Println("\n[done]")
	}
	return nil
})
if err != nil {
	panic(err)
}

fmt.Println(result.StopReason)
```

Streaming is available on the built-in OpenAI, OpenAI-compatible, Qwen, and DeepSeek clients. `TransportResponses` streaming is available on every provider that exposes the responses transport, DeepSeek included.

## Embeddings

Use `NewEmbeddingClient(...)` with OpenAI or an OpenAI-compatible endpoint. Inputs are batched and returned as `[]float32` vectors.

```go
client, err := easyllm.NewEmbeddingClient(easyllm.Config{
	Provider: easyllm.ProviderOpenAI,
	APIKey:   os.Getenv("OPENAI_API_KEY"),
	Model:    "text-embedding-3-small",
})
if err != nil {
	panic(err)
}

resp, err := client.Embed(context.Background(), easyllm.EmbeddingRequest{
	Input:      []string{"first document", "second document"},
	Dimensions: 1024,
})
if err != nil {
	panic(err)
}

fmt.Println(resp.Embeddings[0].Vector)
```

`ProviderOpenAICompatible` uses the same `/embeddings` wire protocol and requires `Config.BaseURL`. Phase one supports numeric embedding arrays; base64 and token-ID inputs are not supported.

## Reranking

Use `NewRerankClient(...)` with `ProviderQwen` and `qwen3-rerank`. The client calls the flat `POST /reranks` endpoint and returns each original document with its score.

```go
client, err := easyllm.NewRerankClient(easyllm.Config{
	Provider: easyllm.ProviderQwen,
	APIKey:   os.Getenv("DASHSCOPE_API_KEY"),
	Model:    "qwen3-rerank",
})
if err != nil {
	panic(err)
}

resp, err := client.Rerank(context.Background(), easyllm.RerankRequest{
	Query:        "What is a vector database?",
	Documents:    []string{"A vector database stores embeddings.", "A queue schedules jobs."},
	TopN:         1,
	Instruction:  "Rank passages by how directly they answer the query.",
})
if err != nil {
	panic(err)
}

fmt.Println(resp.Results[0].Document, resp.Results[0].Score)
```

Phase one does not support `gte-rerank-v2`, the nested DashScope rerank protocol, or multimodal reranking.

## Image Generation

Use `NewImageClient(...)` for direct image generation requests. This API is separate from `NewClient(...)` and does not use `Engine`.

```go
client, err := easyllm.NewImageClient(easyllm.Config{
	Provider: easyllm.ProviderOpenAI,
	APIKey:   os.Getenv("OPENAI_API_KEY"),
	Model:    "gpt-image-1",
})
if err != nil {
	panic(err)
}

resp, err := client.GenerateImage(context.Background(), easyllm.ImageRequest{
	Prompt:       "A clean isometric diagram of a distributed job queue",
	Size:         "1024x1024",
	Quality:      "high",
	OutputFormat: "png",
})
if err != nil {
	panic(err)
}

imageBytes, err := base64.StdEncoding.DecodeString(resp.Images[0].B64JSON)
if err != nil {
	panic(err)
}

fmt.Println(len(imageBytes))
```

Phase one supports OpenAI image generation only and returns `b64_json` image payloads.

## Tool Calling

Tools are strongly typed with Go structs. JSON field names follow `encoding/json`: use a `json` tag for a stable model-facing name, otherwise the Go field name is used. Use the `tool` tag only for descriptions, required fields, enums, validation constraints, and recursive depth limits.

```go
type WeatherArgs struct {
	City string `json:"city" tool:"required,desc=City name"`
	Unit string `json:"unit" tool:"desc=Temperature unit,enum=celsius|fahrenheit"`
}

type WeatherTool struct {
	service WeatherService
}

func (WeatherTool) Name() string {
	return "get_weather"
}

func (WeatherTool) Description() string {
	return "Get current weather for a city"
}

func (w WeatherTool) Run(ctx context.Context, call easyllm.ToolCallContext, args WeatherArgs) (easyllm.ToolResult, error) {
	weather, err := w.service.Current(ctx, args.City, args.Unit)
	if err != nil {
		return easyllm.ToolResult{}, err
	}
	return easyllm.ToolResult{
		Message: "weather fetched",
		Data: map[string]any{
			"city":    args.City,
			"weather": weather,
		},
	}, nil
}

weatherTool, err := easyllm.NewTool[WeatherArgs](WeatherTool{
	service: weatherService,
}, easyllm.WithUnknownArguments(easyllm.UnknownArgumentsWarn))
if err != nil {
	panic(err)
}

engine := easyllm.NewEngine(
	client,
	easyllm.WithTools(weatherTool),
)
```

Recursive structs are supported when the recursive field declares `maxDepth`:

```go
type Node struct {
	Name     string `json:"name"`
	Children []Node `json:"children" tool:"maxDepth=2"`
}
```

`maxDepth` counts recursive edges, not node layers. `maxDepth=2` permits `root -> child -> grandchild`; the grandchild's `children` property is omitted from the generated schema. Therefore, a caller that wants 8 node layers (for example, an existing `maxProofreadStructureDepth=8` setting) should use `maxDepth=7`. The value must be at least `1`. A recursive field without `maxDepth`, an invalid value, or `maxDepth` on a non-recursive field returns an error. Runtime binding applies the same limit and reports an error for input that exceeds it.

Model-side and local tool parallelism are configured separately. `ParallelToolCalls`
controls whether an OpenAI-compatible model may return multiple calls in one
response; `nil` preserves the provider default. The engine executes a returned
batch concurrently by default, with `0` meaning no additional per-Run limit.

```go
allowParallelCalls := true
client, err := easyllm.NewClient(easyllm.Config{
	Provider:          easyllm.ProviderOpenAI,
	APIKey:            apiKey,
	Model:             "gpt-5",
	ParallelToolCalls: &allowParallelCalls,
})

engine := easyllm.NewEngine(
	client,
	easyllm.WithTools(weatherTool),
	easyllm.WithParallelToolExecution(true),
	easyllm.WithMaxToolConcurrency(4),
	easyllm.WithStopOnToolSuccess(weatherTool),
)
```

`WithParallelToolExecution(false)` forces serial execution without changing the
model request. A stop-on-success condition is evaluated after every call in the
current model-response batch has completed, so already accepted calls are never
discarded. Tool implementations may therefore be invoked concurrently, including
multiple invocations of the same tool instance, and must protect mutable state.

When a specific stop tool is configured, the engine starts injecting a temporary
system reminder once half of the model-call budget remains, rounded up. The
reminder includes the registered tool name and the number of calls left,
including the current request, and is not persisted in the session. Override
the threshold with `WithStopToolReminder(n)`, or disable reminders explicitly with
`WithStopToolReminder(0)`.

Tool arguments are collected as raw provider strings and repaired once the registered tool's Go type is known. Unknown arguments are rejected by default; tools can choose `UnknownArgumentsWarn` or `UnknownArgumentsIgnore`. Warning details, the original arguments, and the selected repair strategy are available from `ToolCallContext`. Tool errors are returned to the model as structured tool results and are also recorded in `RunResult.ToolResults`.

When upgrading, move any `tool:"name=..."` field name to the field's `json` tag. Custom `Tool` implementations now receive raw arguments through `ToolCallContext.RawArguments`, so `Tool.Invoke` no longer accepts a separate argument map. `ToolCallOutput` and `ToolCallItem` likewise expose `RawArguments` instead of parsed `Arguments` or `Repaired` fields.

## Sessions

Sessions keep in-memory conversation state across runs. Pass the same session into multiple calls to continue a conversation.

```go
session := easyllm.NewSession()
engine := easyllm.NewEngine(client)

first, err := engine.Run(ctx, easyllm.RunRequest{
	Session: session,
	Input:   "My name is Lin.",
})
if err != nil {
	panic(err)
}

second, err := engine.Run(ctx, easyllm.RunRequest{
	Session: session,
	Input:   "What is my name?",
})
if err != nil {
	panic(err)
}

fmt.Println(first.OutputText)
fmt.Println(second.OutputText)
```

`easyllm.Session` is in-memory only. Persisting or trimming conversation history is left to the application.

## Results

`Run(...)` returns an error only for execution failures, such as provider call failures or hook failures. Business outcomes stay in `RunResult`, including tool errors, partial success, and model-call-limit stops.

Stable stop reason constants:

- `easyllm.StopReasonStop`
- `easyllm.StopReasonToolSucceeded`
- `easyllm.StopReasonModelCallLimitExceeded`

`RunResult` includes the final output text, model-call count, tool-call count, network retry count, tool results, a session snapshot, and usage for the current run. `RetryCount` counts provider-level network retries and does not include the first request attempt.

```go
type Usage struct {
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	InputCost         float64
	OutputCost        float64
	CachedInputCost   float64
	TotalCost         float64
	Details           map[string]any
}
```

`RunResult.Usage` is scoped to the current run. `Session` and `SessionSnapshot` keep cumulative usage across the life of the session.

The cost fields are always present. They stay zero unless the engine is given a `WithPricing(...)` price list.

## Custom Providers

For OpenAI-compatible providers, use `ProviderOpenAICompatible` with a custom `BaseURL`.

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: easyllm.ProviderOpenAICompatible,
	BaseURL:  "https://your-provider.example/v1",
	APIKey:   os.Getenv("CUSTOM_PROVIDER_API_KEY"),
	Model:    "custom-model",
	ExtraBody: map[string]any{
		"custom_flag": true,
	},
})
```

For fully custom protocols, implement `easyllm.Client` and pass it to `easyllm.NewEngine(...)`.

```go
type CustomClient struct{}

func (CustomClient) Generate(ctx context.Context, req easyllm.ModelRequest) (*easyllm.ModelResponse, error) {
	// Call your provider and return provider-neutral output items.
	return &easyllm.ModelResponse{
		// Report the served model so a model-aware Pricer can see it.
		Model: "custom-model",
		Output: []easyllm.OutputItem{
			easyllm.AssistantOutput{
				Role:    "assistant",
				Content: []easyllm.TextPart{{Text: "done"}},
			},
		},
		FinishReason: "stop",
	}, nil
}

func (c CustomClient) GenerateStream(ctx context.Context, req easyllm.ModelRequest, handler easyllm.StreamHandler) error {
	resp, err := c.Generate(ctx, req)
	if err != nil {
		return err
	}
	if handler != nil {
		if err := handler(easyllm.StreamEvent{Type: easyllm.StreamEventMessageDelta, Text: "done"}); err != nil {
			return err
		}
		if err := handler(easyllm.StreamEvent{Type: easyllm.StreamEventDone, Raw: resp}); err != nil {
			return err
		}
	}
	return nil
}

engine := easyllm.NewEngine(CustomClient{})
```
