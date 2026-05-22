package examples

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kiry163/easyllm/agent"
	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/provider/qwen"
	"github.com/kiry163/easyllm/runtime"
	"github.com/kiry163/easyllm/tool"
)

type echoClient struct{}

func (echoClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	var lastUserText string
	for _, item := range req.Input {
		if user, ok := item.(provider.UserMessageItem); ok && len(user.Content) > 0 {
			lastUserText = user.Content[len(user.Content)-1].Text
		}
	}
	return messageResponse("echo: " + lastUserText), nil
}

func Example_basicRun() {
	a := agent.Agent{
		Instructions: "Answer briefly.",
		Client:       echoClient{},
	}

	result, err := a.Run(context.Background(), agent.RunRequest{
		Input: "hello",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)

	// Output:
	// echo: hello
}

func Example_sessionContinuation() {
	session := runtime.NewSession()
	a := agent.Agent{Client: echoClient{}}

	first, err := a.Run(context.Background(), agent.RunRequest{
		Session: session,
		Input:   "first",
	})
	if err != nil {
		panic(err)
	}
	second, err := a.Run(context.Background(), agent.RunRequest{
		Session: session,
		Input:   "second",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(first.OutputText)
	fmt.Println(second.OutputText)
	fmt.Println(len(second.Session.Items))

	// Output:
	// echo: first
	// echo: second
	// 4
}

type toolCallingClient struct{}

func (toolCallingClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	for _, item := range req.Input {
		if _, ok := item.(provider.ToolResultItem); ok {
			return messageResponse("tool result received"), nil
		}
	}
	return &provider.ModelResponse{
		Output: []provider.OutputItem{
			provider.ToolCallOutput{
				CallID:    "call_1",
				Name:      "submit",
				Arguments: map[string]any{"value": "ok"},
			},
		},
		FinishReason: "tool_calls",
	}, nil
}

func Example_toolCalling() {
	submitTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		panic(err)
	}

	a := agent.Agent{
		Client: toolCallingClient{},
		Tools:  []tool.Tool{submitTool},
	}
	result, err := a.Run(context.Background(), agent.RunRequest{
		Input: "submit ok",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.OutputText)
	fmt.Println(result.StopReason)

	// Output:
	// tool result received
	// stop
}

type submitTool struct {
	service string
}

type submitArgs struct {
	Value string `tool:"name=value,required,desc=Value to submit,enum=ok|retry"`
}

func (submitTool) Name() string {
	return "submit"
}

func (submitTool) Description() string {
	return "Submit a value"
}

func (t submitTool) Run(ctx context.Context, call tool.CallContext, args submitArgs) (tool.Result, error) {
	return tool.Result{
		Message: "accepted by " + t.service,
		Data:    map[string]any{"value": args.Value},
	}, nil
}

func Example_schemaTags() {
	type forecastArgs struct {
		Unit string `tool:"name=unit,required,desc=Temperature unit,enum=celsius|fahrenheit"`
		Days int    `tool:"name=days,desc=Forecast days,minimum=1,maximum=10"`
	}

	schema, err := tool.SchemaFor[forecastArgs]()
	if err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(data))

	// Output:
	// {
	//   "additionalProperties": false,
	//   "properties": {
	//     "days": {
	//       "description": "Forecast days",
	//       "maximum": 10,
	//       "minimum": 1,
	//       "type": "integer"
	//     },
	//     "unit": {
	//       "description": "Temperature unit",
	//       "enum": [
	//         "celsius",
	//         "fahrenheit"
	//       ],
	//       "type": "string"
	//     }
	//   },
	//   "required": [
	//     "unit"
	//   ],
	//   "type": "object"
	// }
}

func Example_qwenProviderConfig() {
	p := qwen.NewProvider(
		qwen.WithAPIKey("example-key"),
		qwen.WithBaseURL(qwen.DefaultBaseURL),
	)

	thinking := false
	model := p.ChatModel(qwen.ChatModelConfig{
		Model:    "qwen-plus",
		Thinking: &thinking,
	})

	fmt.Println(model != nil)

	// Output:
	// true
}

func messageResponse(text string) *provider.ModelResponse {
	return &provider.ModelResponse{
		Output: []provider.OutputItem{
			provider.MessageOutput{
				Role:    "assistant",
				Content: []provider.TextPart{{Text: text}},
			},
		},
		FinishReason: "stop",
	}
}
