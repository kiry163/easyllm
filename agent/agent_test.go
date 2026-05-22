package agent

import (
	"context"
	"testing"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/runtime"
	"github.com/kiry163/easyllm/tool"
)

type fakeClient struct{}

func (fakeClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
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

func TestAgentRunAppendsInputAndReturnsOutput(t *testing.T) {
	a := Agent{
		Instructions: "be helpful",
		Client:       fakeClient{},
		Runner:       &runtime.Runner{Client: fakeClient{}},
	}

	result, err := a.Run(context.Background(), RunRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
}

func TestAgentRunDoesNotRequireAgentModelWhenProviderOwnsIt(t *testing.T) {
	a := Agent{
		Client: fakeClient{},
		Runner: &runtime.Runner{Client: fakeClient{}},
	}

	result, err := a.Run(context.Background(), RunRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
}

type toolCallingClient struct {
	calls int
}

func (c *toolCallingClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	c.calls++
	if c.calls == 1 {
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
				Role:    "assistant",
				Content: []provider.TextPart{{Text: "done"}},
			},
		},
		FinishReason: "stop",
	}, nil
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
	return tool.Result{Message: "submitted"}, nil
}

func TestAgentRunCanStopAfterSuccessfulToolCall(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client := &toolCallingClient{}
	a := Agent{
		Client: client,
		Tools:  []tool.Tool{runTool},
	}

	result, err := a.Run(context.Background(), RunRequest{
		Input:             "submit",
		StopAfterToolCall: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected one model call, got %d", client.calls)
	}
	if result.StopReason != "stop_on_tool_result" {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if result.OutputText != "" {
		t.Fatalf("expected no model summary after tool call, got %q", result.OutputText)
	}
}
