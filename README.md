# easyllm

`easyllm` is a lightweight Go engine for building LLM-backed backend workflows with provider-neutral model calls, tool calling, and in-memory sessions.

The current design separates provider connection setup from model configuration:

- **Provider**: vendor connection settings such as API key, base URL, HTTP client, and retry behavior.
- **Model client**: model name and model-level parameters such as `temperature`, `top_p`, `max_tokens`, and Qwen thinking mode.
- **Engine**: instructions, tools, session state, hooks, and tool-calling loop execution.

## Status

This project is early-stage. The public API is centered on a single `engine` entrypoint, with provider packages configuring model clients and the engine handling the execution loop.

Implemented packages:

- `provider`: provider-neutral request, response, and `ModelClient` interface.
- `provider/openai`: OpenAI provider constructors.
- `provider/qwen`: Qwen provider constructors backed by DashScope OpenAI-compatible APIs.
- `provider/deepseek`: DeepSeek provider constructors backed by DeepSeek OpenAI-compatible APIs.
- `provider/openai/compat`: reusable OpenAI-compatible protocol base.
- `engine`: public execution entrypoint, session types, hooks, and tool-calling loop.
- `tool`: tool definitions, schema generation, typed argument binding, and JSON repair.
- `cmd/easyllm-call`: small Qwen-focused CLI for real provider smoke tests.

## Install

```bash
go get github.com/kiry163/easyllm
```

## Quick Start: Qwen

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kiry163/easyllm/engine"
	"github.com/kiry163/easyllm/provider/qwen"
)

func main() {
	p := qwen.NewProvider(
		qwen.WithAPIKey(os.Getenv("DASHSCOPE_API_KEY")),
	)

	thinking := false
	temperature := 0.6
	topP := 0.7
	maxTokens := 512

	model := p.ChatModel(qwen.ChatModelConfig{
		Model:       "qwen-plus",
		Thinking:    &thinking,
		Temperature: &temperature,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
	})

	rt := engine.New(
		model,
		engine.WithInstructions("Answer clearly and concisely."),
	)

	result, err := rt.Run(context.Background(), engine.RunRequest{
		Input: "用一句话解释递归。",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
}
```

## Quick Start: OpenAI

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kiry163/easyllm/engine"
	"github.com/kiry163/easyllm/provider/openai"
)

func main() {
	p := openai.NewProvider(
		openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
		openai.WithBaseURL("https://api.openai.com/v1"),
	)

	model := p.ChatModel(openai.ChatModelConfig{
		Model: "gpt-4.1-mini",
	})

	rt := engine.New(model)

	result, err := rt.Run(context.Background(), engine.RunRequest{
		Input: "Say hello in one sentence.",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
}
```

## Quick Start: DeepSeek

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kiry163/easyllm/engine"
	"github.com/kiry163/easyllm/provider/deepseek"
)

func main() {
	p := deepseek.NewProvider(
		deepseek.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
	)

	model := p.ChatModel(deepseek.ChatModelConfig{
		Model: "deepseek-v4-flash",
	})

	rt := engine.New(model)

	result, err := rt.Run(context.Background(), engine.RunRequest{
		Input: "Say hello in one sentence.",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
}
```

## Tool Calling

Tools are strongly typed with struct tags. The engine executes model-requested tool calls, appends tool results to the in-memory session, and continues the model loop until it reaches a final answer or call limit.

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

func (w WeatherTool) Run(ctx context.Context, call tool.CallContext, args WeatherArgs) (tool.Result, error) {
	return tool.Result{
		Message: "sunny",
		Data: map[string]any{
			"city": args.City,
			"temp": "25C",
		},
	}, nil
}

weatherTool, err := tool.New[WeatherArgs](WeatherTool{db: db})
if err != nil {
	panic(err)
}

rt := engine.New(
	model,
	engine.WithTools(weatherTool),
)
```

`tool` includes JSON repair for common malformed tool argument payloads, including string-wrapped JSON and fenced JSON blocks.

## CLI Smoke Test

The repository includes a small CLI for testing real Qwen model calls. It reads most values from `.env` or environment variables and currently supports `-env` and `-transport`.

### Qwen

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
engine.New(...).Run
  -> provider.ModelClient.Generate
      -> provider/openai/compat HTTP call
  -> tool.Tool.Invoke
  -> provider.ModelClient.Generate
```

Important boundaries:

- `engine.Engine.Run` is one engine execution. It may contain multiple model API calls if tools are used.
- `provider.ModelClient.Generate` is one model call from the runtime perspective. It may retry HTTP requests internally.
- Provider packages create configured model clients. They do not store session state.
- `engine.Session` is in-memory only and is not persisted by the library.

## Result Semantics

`Run(...)` returns `error` only for execution failures, such as a provider call failure or hook failure. Business outcomes stay in `RunResult`, including tool errors, partial success, and model-call-limit stops.

Stable `engine.StopReason` constants:

- `engine.StopReasonStop`
- `engine.StopReasonStopOnToolResult`
- `engine.StopReasonModelCallLimitExceeded`

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

`RunResult.Usage` is the usage total for the current run. `engine.Session` and `engine.SessionSnapshot` keep cumulative usage across the life of the session.

## Provider Model

Providers are created with vendor connection settings:

```go
p := qwen.NewProvider(
	qwen.WithAPIKey(os.Getenv("DASHSCOPE_API_KEY")),
	qwen.WithBaseURL(qwen.DefaultBaseURL),
	qwen.WithTimeout(30 * time.Second),
	qwen.WithRetry(qwen.RetryConfig{MaxAttempts: 2}),
)
```

Model clients are created from providers with model-level configuration:

```go
model := p.ChatModel(qwen.ChatModelConfig{
	Model: "qwen-plus",
})
```

Engine and provider defaults are intentionally conservative:

- `engine.New(...)` defaults to `3` model calls unless `engine.WithMaxModelCalls(...)` is set.
- OpenAI-compatible providers use a `30s` HTTP timeout by default.
- Retry defaults to one attempt unless `WithRetry` is configured.

This avoids guessing behavior from model names. If a provider has different model families or transports, expose a different constructor such as `ChatModel`, `ResponsesModel`, `VisionModel`, or `ImageModel`.

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
- Add more built-in providers.
- Decide how to expose vision, image, and speech model clients without overloading text generation APIs.
