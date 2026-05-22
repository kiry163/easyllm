package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

type fakeClient struct{}

func (fakeClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	if req.Model != "" {
		return nil, fmt.Errorf("expected empty model in runtime request, got %q", req.Model)
	}
	hasToolResult := false
	for _, item := range req.Input {
		if _, ok := item.(provider.ToolResultItem); ok {
			hasToolResult = true
			break
		}
	}
	if !hasToolResult {
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
	return &provider.ModelResponse{
		Output: []provider.OutputItem{
			provider.MessageOutput{
				Role: "assistant",
				Content: []provider.TextPart{
					{Text: "done"},
				},
			},
		},
		FinishReason: "stop",
	}, nil
}

func TestRunnerExecutesToolCallsWithinSession(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	session := NewSession()
	session.AppendUserText("hello")

	runner := Runner{Client: fakeClient{}}
	result, err := runner.Run(context.Background(), RunRequest{
		Session:       session,
		Tools:         []tool.Tool{runTool},
		MaxModelCalls: 3,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
	if result.ToolCallCount != 1 {
		t.Fatalf("unexpected tool call count: %d", result.ToolCallCount)
	}
}

func TestRunnerCanStopAfterSuccessfulToolCall(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	session := NewSession()
	session.AppendUserText("hello")

	runner := Runner{Client: fakeClient{}}
	result, err := runner.Run(context.Background(), RunRequest{
		Session:           session,
		Tools:             []tool.Tool{runTool},
		MaxModelCalls:     3,
		StopAfterToolCall: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ModelCallCount != 1 {
		t.Fatalf("expected one model call, got %d", result.ModelCallCount)
	}
	if result.StopReason != "stop_on_tool_result" {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if result.OutputText != "" {
		t.Fatalf("expected no model summary after tool call, got %q", result.OutputText)
	}
}

type submitArgs struct {
	Value string `tool:"name=value,required"`
}

type submitTool struct{}

func (submitTool) Name() string {
	return "submit"
}

func (submitTool) Description() string {
	return "submit payload"
}

func (submitTool) Run(ctx context.Context, call tool.CallContext, args submitArgs) (tool.Result, error) {
	return tool.Result{Message: "ok"}, nil
}

func TestSessionMessagesViewIncludesToolResults(t *testing.T) {
	session := NewSession()
	session.AppendUserText("hello")
	session.AppendItems(provider.ToolResultItem{
		CallID:  "call_1",
		Name:    "submit",
		Content: "{\"status\":\"ok\"}",
	})

	messages := session.MessagesView()
	if len(messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(messages))
	}
	if messages[1].Role != "tool" {
		t.Fatalf("unexpected role: %q", messages[1].Role)
	}
}
