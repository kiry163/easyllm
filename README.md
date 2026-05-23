# easyllm

`easyllm` is a lightweight Go library for building LLM-backed backend workflows with provider-neutral model calls, tool calling, and in-memory sessions.

The normal API is intentionally centered on one import:

- `easyllm.NewClient(...)`: selects provider, credentials, model, transport, and common model parameters.
- `easyllm.NewEngine(...)`: runs instructions, tools, sessions, hooks, and the tool-calling loop.
- `easyllm.NewTool(...)`: builds typed tools from Go structs and runner methods.

Advanced users can import `provider/openai` directly when they need lower-level OpenAI-compatible client construction.

## Status

This project is early-stage. The public API is designed around a single top-level package for application code.

Implemented packages:

- `easyllm`: client construction, provider dispatch, runtime execution, sessions, hooks, tools, schemas, and provider-neutral model types.
- `provider/openai`: advanced OpenAI provider constructors.
- `cmd/easyllm-call`: small Qwen-focused CLI for real provider smoke tests.

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
	temperature := 0.6
	topP := 0.7
	maxTokens := 512

	client, err := easyllm.NewClient(easyllm.Config{
		Provider:    easyllm.ProviderQwen,
		APIKey:      os.Getenv("DASHSCOPE_API_KEY"),
		BaseURL:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:       "qwen-plus",
		Transport:   easyllm.TransportChat,
		Temperature: &temperature,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
		Options: map[string]any{
			"thinking": false,
		},
	})
	if err != nil {
		panic(err)
	}

	runtime := easyllm.NewEngine(
		client,
		easyllm.WithInstructions("Answer clearly and concisely."),
	)

	result, err := runtime.Run(context.Background(), easyllm.RunRequest{
		Input: "用一句话解释递归。",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
}
```

### OpenAI

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: easyllm.ProviderOpenAI,
	APIKey:   os.Getenv("OPENAI_API_KEY"),
	Model:    "gpt-4.1-mini",
})
if err != nil {
	panic(err)
}

runtime := easyllm.NewEngine(client)

result, err := runtime.Run(context.Background(), easyllm.RunRequest{
	Input: "Say hello in one sentence.",
})
```

### DeepSeek

```go
client, err := easyllm.NewClient(easyllm.Config{
	Provider: easyllm.ProviderDeepSeek,
	APIKey:   os.Getenv("DEEPSEEK_API_KEY"),
	Model:    "deepseek-v4-flash",
})
if err != nil {
	panic(err)
}

runtime := easyllm.NewEngine(client)

result, err := runtime.Run(context.Background(), easyllm.RunRequest{
	Input: "Say hello in one sentence.",
})
```

## Tool Calling

Tools are strongly typed with struct tags. The runtime executes model-requested tool calls, appends tool results to the in-memory session, and continues the model loop until it reaches a final answer or call limit.

```go
type WeatherArgs struct {
	City string `tool:"name=city,required,desc=City name"`
}

type WeatherTool struct {
	db *sql.DB
}

func (WeatherTool) Name() string {
	return "get_weather"
}

func (WeatherTool) Description() string {
	return "Get current weather"
}

func (w WeatherTool) Run(ctx context.Context, call easyllm.ToolCallContext, args WeatherArgs) (easyllm.ToolResult, error) {
	return easyllm.ToolResult{
		Message: "sunny",
		Data: map[string]any{
			"city": args.City,
			"temp": "25C",
		},
	}, nil
}

weatherTool, err := easyllm.NewTool[WeatherArgs](WeatherTool{db: db})
if err != nil {
	panic(err)
}

runtime := easyllm.NewEngine(
	client,
	easyllm.WithTools(weatherTool),
)
```

Tool arguments include JSON repair for common malformed payloads, including string-wrapped JSON and fenced JSON blocks.

## CLI Smoke Test

The repository includes a small CLI for testing real Qwen model calls. It reads most values from `.env` or environment variables and currently supports `-env` and `-transport`.

```bash
export DASHSCOPE_API_KEY=your_key_here
export QWEN_MODEL=qwen-plus
export EASYLLM_PROMPT="用一句话解释递归。"

go run ./cmd/easyllm-call \
  -transport chat
```

Use `-transport responses` to test the Responses-style adapter.

## Architecture

```text
easyllm.NewEngine(...).Run
  -> easyllm.Client.Generate
      -> OpenAI-compatible HTTP call
  -> easyllm.Tool.Invoke
  -> easyllm.Client.Generate
```

Important boundaries:

- `easyllm.Engine.Run` is one runtime execution. It may contain multiple model API calls if tools are used.
- `easyllm.Client.Generate` is one model call from the runtime perspective. It may retry HTTP requests internally.
- Provider implementations create configured model clients. They do not store session state.
- `easyllm.Session` is in-memory only and is not persisted by the library.

## Result Semantics

`Run(...)` returns `error` only for execution failures, such as a provider call failure or hook failure. Business outcomes stay in `RunResult`, including tool errors, partial success, and model-call-limit stops.

Stable stop reason constants:

- `easyllm.StopReasonStop`
- `easyllm.StopReasonStopOnToolResult`
- `easyllm.StopReasonModelCallLimitExceeded`

`RunResult` also exposes aggregated usage for the current run:

```go
type Usage struct {
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	Details           map[string]any
}
```

`RunResult.Usage` is the usage total for the current run. `easyllm.Session` and `easyllm.SessionSnapshot` keep cumulative usage across the life of the session.

Defaults are intentionally conservative:

- `easyllm.NewEngine(...)` defaults to `3` model calls unless `easyllm.WithMaxModelCalls(...)` is set.
- OpenAI-compatible providers use a `30s` HTTP timeout by default.
- Retry defaults to one attempt unless retry is configured.

## Advanced OpenAI Provider Usage

Most applications should use `easyllm.NewClient(...)`. Built-in OpenAI-compatible vendors such as Qwen and DeepSeek are selected through `easyllm.Config.Provider`, not through public vendor packages.

The lower-level `provider/openai` package is available when you need direct OpenAI client construction:

```go
package main

import (
	"os"
	"time"

	"github.com/kiry163/easyllm/provider/openai"
)

func main() {
	p := openai.NewProvider(
		openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
		openai.WithTimeout(30*time.Second),
		openai.WithRetry(openai.RetryConfig{MaxAttempts: 2}),
	)

	client := p.ChatClient(openai.ChatClientConfig{
		Model: "gpt-4.1-mini",
	})

	_ = client
}
```

## Development

Run the full test suite:

```bash
go test ./...
```

Format code:

```bash
gofmt -w $(find . -name '*.go')
```

## Roadmap

Near-term work:

- Validate model configuration values such as `temperature`, `top_p`, and `max_tokens`.
- Add more examples for tool calling and session continuation.
- Expand OpenAI-compatible request/response coverage.
- Add streaming support.
- Decide how to expose vision, image, and speech model clients without overloading text generation APIs.
