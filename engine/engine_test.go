package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/kiry163/easyllm/provider"
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

func TestNewRunAppendsInputAndReturnsOutput(t *testing.T) {
	engine := New(
		fakeClient{},
		WithInstructions("be helpful"),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "hello"})
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

func TestRunCanStopAfterSuccessfulToolCallFromEngineOption(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client := &toolCallingClient{}
	engine := New(
		client,
		WithTools(runTool),
		WithStopAfterToolCall(true),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected one model call, got %d", client.calls)
	}
	if result.StopReason != StopReasonStopOnToolResult {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if result.OutputText != "" {
		t.Fatalf("expected no model summary after tool call, got %q", result.OutputText)
	}
}

func TestRunUsesDefaultMaxModelCallsFromOptions(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client := &toolCallingClient{}
	engine := New(
		client,
		WithTools(runTool),
		WithMaxModelCalls(1),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.StopReason != StopReasonModelCallLimitExceeded {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if result.ModelCallCount != 1 {
		t.Fatalf("expected one model call, got %d", result.ModelCallCount)
	}
	if client.calls != 1 {
		t.Fatalf("expected one client call, got %d", client.calls)
	}
}

func TestRunReturnsToolResults(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	engine := New(
		&toolCallingClient{},
		WithTools(runTool),
		WithStopAfterToolCall(true),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected one tool result, got %d", len(result.ToolResults))
	}
	if result.ToolResults[0].Call.Name != "submit" {
		t.Fatalf("unexpected tool name: %q", result.ToolResults[0].Call.Name)
	}
	if result.ToolResults[0].Result.Message != "submitted" {
		t.Fatalf("unexpected tool result: %q", result.ToolResults[0].Result.Message)
	}
}

func TestRunCreatesSessionWhenNil(t *testing.T) {
	engine := New(fakeClient{})

	result, err := engine.Run(context.Background(), RunRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Session.Items) == 0 {
		t.Fatalf("expected session items to be captured")
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

type usageClient struct {
	calls int
}

func (c *usageClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
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
			Usage: provider.Usage{
				InputTokens:       10,
				OutputTokens:      3,
				TotalTokens:       13,
				CachedInputTokens: 4,
			},
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
		Usage: provider.Usage{
			InputTokens:       8,
			OutputTokens:      2,
			TotalTokens:       10,
			CachedInputTokens: 1,
		},
	}, nil
}

type failingTool struct{}

func (failingTool) Name() string {
	return "submit"
}

func (failingTool) Description() string {
	return "submit payload"
}

func (failingTool) Run(ctx context.Context, call tool.CallContext, args submitArgs) (tool.Result, error) {
	return tool.Result{}, errors.New("submit failed")
}

func TestRunReturnsResultWhenToolFails(t *testing.T) {
	runTool, err := tool.New[submitArgs](failingTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	engine := New(
		&toolCallingClient{},
		WithTools(runTool),
		WithMaxModelCalls(1),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("expected tool failure to stay in result, got error: %v", err)
	}
	if result.StopReason != StopReasonModelCallLimitExceeded {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected one tool result, got %d", len(result.ToolResults))
	}
	if result.ToolResults[0].Err == nil {
		t.Fatalf("expected tool error to be recorded")
	}
}

func TestRunAggregatesUsageAcrossModelCalls(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	engine := New(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Usage.TotalTokens != 23 {
		t.Fatalf("unexpected total tokens: %+v", result.Usage)
	}
	if result.Usage.InputTokens != 18 || result.Usage.OutputTokens != 5 {
		t.Fatalf("unexpected usage split: %+v", result.Usage)
	}
	if result.Usage.CachedInputTokens != 5 {
		t.Fatalf("unexpected cached tokens: %+v", result.Usage)
	}
}

func TestRunCopiesUsageOntoSessionSnapshot(t *testing.T) {
	runTool, err := tool.New[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	engine := New(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Session.Usage.InputTokens != result.Usage.InputTokens ||
		result.Session.Usage.OutputTokens != result.Usage.OutputTokens ||
		result.Session.Usage.TotalTokens != result.Usage.TotalTokens ||
		result.Session.Usage.CachedInputTokens != result.Usage.CachedInputTokens {
		t.Fatalf("expected session usage to match run usage: session=%+v run=%+v", result.Session.Usage, result.Usage)
	}
}

type errorClient struct{}

func (errorClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	return nil, errors.New("model unavailable")
}

func TestRunReturnsExecutionErrorWhenModelCallFails(t *testing.T) {
	engine := New(errorClient{})

	result, err := engine.Run(context.Background(), RunRequest{Input: "hello"})
	if err == nil {
		t.Fatalf("expected execution error")
	}
	if result != nil {
		t.Fatalf("expected nil result on execution error, got %+v", result)
	}
}
