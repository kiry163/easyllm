# easyllm

`easyllm` is a lightweight Go library for building LLM-backed backend workflows with provider-neutral model calls, tool calling, hooks, and in-memory sessions.

The public API is intentionally centered on one import:

- `easyllm.NewClient(...)` creates a provider-backed model client.
- `easyllm.NewEngine(...)` runs instructions, tools, sessions, hooks, and the tool-calling loop.
- `easyllm.NewTool(...)` turns typed Go structs and runner methods into model-callable tools.

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

DeepSeek keeps its reasoning controls provider-local instead of adding more top-level `Config` fields:

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

When DeepSeek returns tool calls in thinking mode, `easyllm` preserves the provider-required continuation state across the tool loop automatically. That replay stays internal to the provider adapter rather than becoming part of the public API.

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

`Run(...)` may perform multiple model calls when tools are involved:

```text
easyllm.Engine.Run
  -> easyllm.Client.Generate
  -> easyllm.Tool.Invoke
  -> easyllm.Client.Generate
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

Streaming is available on the built-in OpenAI, OpenAI-compatible, Qwen, and DeepSeek clients. `TransportResponses` streaming is available on providers that expose the responses transport; DeepSeek remains chat-only.

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

Tools are strongly typed with Go structs. Field names default to `json` tags when present, then snake_case Go field names. Use the `tool` tag for descriptions, required fields, enums, and validation constraints.

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
})
if err != nil {
	panic(err)
}

engine := easyllm.NewEngine(
	client,
	easyllm.WithTools(weatherTool),
)
```

Tool arguments include JSON repair for common malformed payloads, including string-wrapped JSON and fenced JSON blocks. Tool errors are returned to the model as structured tool results and are also recorded in `RunResult.ToolResults`.

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
- `easyllm.StopReasonStopOnToolResult`
- `easyllm.StopReasonModelCallLimitExceeded`

`RunResult` includes the final output text, model-call count, tool-call count, tool results, a session snapshot, and usage for the current run.

```go
type Usage struct {
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	Details           map[string]any
}
```

`RunResult.Usage` is scoped to the current run. `Session` and `SessionSnapshot` keep cumulative usage across the life of the session.

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
		Output: []easyllm.OutputItem{
			easyllm.MessageOutput{
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
