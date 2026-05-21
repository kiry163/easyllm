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
	runTool, err := tool.NewStruct("submit", "submit payload", func(ctx context.Context, call tool.CallContext, args struct {
		Value string `tool:"name=value,required"`
	}) (tool.Result, error) {
		return tool.Result{Message: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("NewStruct returned error: %v", err)
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
