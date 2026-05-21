# easyllm

`easyllm` is a lightweight Go runtime for building LLM agent workflows with provider-neutral model calls, tool calling, and in-memory sessions.

The current design separates provider connection setup from model configuration:

- **Provider**: vendor connection settings such as API key, base URL, HTTP client, and retry behavior.
- **Model client**: model name and model-level parameters such as `temperature`, `top_p`, `max_tokens`, and Qwen thinking mode.
- **Agent/runtime**: instructions, tools, session state, and tool-calling loop execution.

## Status

This project is early-stage. The public API is being shaped around real usage with OpenAI-compatible providers and Qwen.

Implemented packages:

- `provider`: provider-neutral request, response, and `ModelClient` interface.
- `provider/openai`: OpenAI provider constructors.
- `provider/qwen`: Qwen provider constructors backed by DashScope OpenAI-compatible APIs.
- `provider/openai/compat`: reusable OpenAI-compatible protocol base.
- `agent`: thin agent facade over the runtime.
- `runtime`: in-memory session and tool-calling execution loop.
- `tool`: tool definitions, schema generation, typed argument binding, and JSON repair.
- `cmd/easyllm-call`: small CLI for real provider smoke tests.

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

	"github.com/kiry163/easyllm/agent"
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

	a := agent.Agent{
		Instructions: "Answer clearly and concisely.",
		Client:       model,
	}

	result, err := a.Run(context.Background(), agent.RunRequest{
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

	"github.com/kiry163/easyllm/agent"
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

	a := agent.Agent{
		Client: model,
	}

	result, err := a.Run(context.Background(), agent.RunRequest{
		Input: "Say hello in one sentence.",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
}
```

## Tool Calling

Tools are strongly typed with struct tags. The runtime executes model-requested tool calls, appends tool results to the in-memory session, and continues the model loop until it reaches a final answer or call limit.

```go
type WeatherArgs struct {
	City string `tool:"name=city,required,desc=City name"`
}

weatherTool, err := tool.NewStruct("get_weather", "Get current weather", func(ctx context.Context, call tool.CallContext, args WeatherArgs) (tool.Result, error) {
	return tool.Result{
		Message: "sunny",
		Data: map[string]any{
			"city": args.City,
			"temp": "25C",
		},
	}, nil
})
if err != nil {
	panic(err)
}

a := agent.Agent{
	Client: model,
	Tools:  []tool.Tool{weatherTool},
}
```

`tool` includes JSON repair for common malformed tool argument payloads, including string-wrapped JSON and fenced JSON blocks.

## CLI Smoke Test

The repository includes a small CLI for testing real model calls.

### Qwen

```bash
export DASHSCOPE_API_KEY=your_key_here

go run ./cmd/easyllm-call \
  -provider qwen \
  -transport chat \
  -model qwen-plus \
  -enable-thinking=false \
  -temperature 0.6 \
  -top-p 0.7 \
  -max-tokens 512 \
  -prompt "用一句话解释递归。"
```

### OpenAI

```bash
export OPENAI_API_KEY=your_key_here

go run ./cmd/easyllm-call \
  -provider openai \
  -transport chat \
  -model gpt-4.1-mini \
  -prompt "Say hello in one sentence."
```

Use `-transport responses` to test the Responses-style adapter.

## Architecture

```text
Agent.Run
  -> runtime.Runner
      -> provider.ModelClient.Generate
          -> provider/openai/compat HTTP call
      -> tool.Tool.Invoke
      -> provider.ModelClient.Generate
```

Important boundaries:

- `Agent.Run` is one agent execution. It may contain multiple model API calls if tools are used.
- `provider.ModelClient.Generate` is one model call from the runtime perspective. It may retry HTTP requests internally.
- Provider packages create configured model clients. They do not store session state.
- Sessions are in-memory only and are not persisted by the library.

## Provider Model

Providers are created with vendor connection settings:

```go
p := qwen.NewProvider(
	qwen.WithAPIKey(os.Getenv("DASHSCOPE_API_KEY")),
	qwen.WithBaseURL(qwen.DefaultBaseURL),
)
```

Model clients are created from providers with model-level configuration:

```go
model := p.ChatModel(qwen.ChatModelConfig{
	Model: "qwen-plus",
})
```

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
