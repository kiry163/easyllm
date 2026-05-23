package examples

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kiry163/easyllm"
)

type echoClient struct{}

func (echoClient) Generate(ctx context.Context, req easyllm.ModelRequest) (*easyllm.ModelResponse, error) {
	var lastUserText string
	for _, item := range req.Input {
		if user, ok := item.(easyllm.UserMessageItem); ok && len(user.Content) > 0 {
			lastUserText = user.Content[len(user.Content)-1].Text
		}
	}
	return messageResponse("echo: " + lastUserText), nil
}

func Example_basicRun() {
	rt := easyllm.NewEngine(
		echoClient{},
		easyllm.WithInstructions("Answer briefly."),
	)

	result, err := rt.Run(context.Background(), easyllm.RunRequest{
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
	session := easyllm.NewSession()
	rt := easyllm.NewEngine(echoClient{})

	first, err := rt.Run(context.Background(), easyllm.RunRequest{
		Session: session,
		Input:   "first",
	})
	if err != nil {
		panic(err)
	}
	second, err := rt.Run(context.Background(), easyllm.RunRequest{
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

func (toolCallingClient) Generate(ctx context.Context, req easyllm.ModelRequest) (*easyllm.ModelResponse, error) {
	for _, item := range req.Input {
		if _, ok := item.(easyllm.ToolResultItem); ok {
			return messageResponse("tool result received"), nil
		}
	}
	return &easyllm.ModelResponse{
		Output: []easyllm.OutputItem{
			easyllm.ToolCallOutput{
				CallID:    "call_1",
				Name:      "submit",
				Arguments: map[string]any{"value": "ok"},
			},
		},
		FinishReason: "tool_calls",
	}, nil
}

func Example_toolCalling() {
	submitTool, err := easyllm.NewTool[submitArgs](submitTool{})
	if err != nil {
		panic(err)
	}

	rt := easyllm.NewEngine(
		toolCallingClient{},
		easyllm.WithTools(submitTool),
	)
	result, err := rt.Run(context.Background(), easyllm.RunRequest{
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

type profileExtractionClient struct{}

func (profileExtractionClient) Generate(ctx context.Context, req easyllm.ModelRequest) (*easyllm.ModelResponse, error) {
	for _, item := range req.Input {
		if _, ok := item.(easyllm.ToolResultItem); ok {
			return messageResponse("profile submitted"), nil
		}
	}
	return &easyllm.ModelResponse{
		Output: []easyllm.OutputItem{
			easyllm.ToolCallOutput{
				CallID: "call_profile_1",
				Name:   "submit_student_profile",
				Arguments: map[string]any{
					"nickname": "小林",
					"grade":    "五年级",
					"gender":   "男",
					"hobbies":  []any{"足球", "机器人", "画画"},
				},
			},
		},
		FinishReason: "tool_calls",
	}, nil
}

type studentProfile struct {
	Nickname string
	Grade    string
	Gender   string
	Hobbies  []string
}

type profileStore struct {
	Profile studentProfile
}

type submitProfileArgs struct {
	Nickname string   `tool:"name=nickname,required,desc=Student nickname"`
	Grade    string   `tool:"name=grade,required,desc=Student grade"`
	Gender   string   `tool:"name=gender,required,desc=Student gender,enum=男|女|未说明"`
	Hobbies  []string `tool:"name=hobbies,required,desc=Student hobbies"`
}

type submitProfileTool struct {
	store *profileStore
}

func (submitProfileTool) Name() string {
	return "submit_student_profile"
}

func (submitProfileTool) Description() string {
	return "Submit a structured student profile extracted from the user's self introduction"
}

func (t submitProfileTool) Run(ctx context.Context, call easyllm.ToolCallContext, args submitProfileArgs) (easyllm.ToolResult, error) {
	t.store.Profile = studentProfile{
		Nickname: args.Nickname,
		Grade:    args.Grade,
		Gender:   args.Gender,
		Hobbies:  args.Hobbies,
	}
	return easyllm.ToolResult{
		Message: "student profile submitted",
		Data: map[string]any{
			"profile": t.store.Profile,
		},
	}, nil
}

func Example_submitStructuredProfileToMemory() {
	store := &profileStore{}
	submitProfile, err := easyllm.NewTool[submitProfileArgs](submitProfileTool{store: store})
	if err != nil {
		panic(err)
	}

	rt := easyllm.NewEngine(
		profileExtractionClient{},
		easyllm.WithInstructions("Extract the student profile from the user's self introduction and submit it with the tool."),
		easyllm.WithTools(submitProfile),
		easyllm.WithStopAfterToolCall(true),
	)
	result, err := rt.Run(context.Background(), easyllm.RunRequest{
		Input: "大家好，我叫小林，是五年级男生。我喜欢足球、机器人和画画。",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.StopReason)
	fmt.Println(result.ToolResults[0].Call.Name)
	fmt.Printf("%s %s %s\n", store.Profile.Nickname, store.Profile.Grade, store.Profile.Gender)
	fmt.Println(strings.Join(store.Profile.Hobbies, ","))

	// Output:
	// stop_on_tool_result
	// submit_student_profile
	// 小林 五年级 男
	// 足球,机器人,画画
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

func (t submitTool) Run(ctx context.Context, call easyllm.ToolCallContext, args submitArgs) (easyllm.ToolResult, error) {
	return easyllm.ToolResult{
		Message: "accepted by " + t.service,
		Data:    map[string]any{"value": args.Value},
	}, nil
}

func Example_schemaTags() {
	type forecastArgs struct {
		Unit string `tool:"name=unit,required,desc=Temperature unit,enum=celsius|fahrenheit"`
		Days int    `tool:"name=days,desc=Forecast days,minimum=1,maximum=10"`
	}

	schema, err := easyllm.SchemaFor[forecastArgs]()
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

func Example_newClient() {
	client, err := easyllm.NewClient(easyllm.Config{
		Provider: easyllm.ProviderQwen,
		APIKey:   "example-key",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:    "qwen-plus",
		Options: map[string]any{
			"thinking": false,
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(client != nil)

	// Output:
	// true
}

func messageResponse(text string) *easyllm.ModelResponse {
	return &easyllm.ModelResponse{
		Output: []easyllm.OutputItem{
			easyllm.MessageOutput{
				Role:    "assistant",
				Content: []easyllm.TextPart{{Text: text}},
			},
		},
		FinishReason: "stop",
	}
}
