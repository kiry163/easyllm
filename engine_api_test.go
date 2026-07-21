package easyllm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

type fakeClient struct{}

func (fakeClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	return &model.ModelResponse{
		Output: []model.OutputItem{
			model.AssistantOutput{
				Role: "assistant",
				Content: []model.TextPart{
					{Text: "done"},
				},
			},
		},
		FinishReason: "stop",
	}, nil
}

func (fakeClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	resp, err := fakeClient{}.Generate(ctx, req)
	if err != nil {
		return err
	}
	if handler != nil {
		if len(resp.Output) > 0 {
			if msg, ok := resp.Output[0].(model.AssistantOutput); ok && len(msg.Content) > 0 {
				if err := handler(model.StreamEvent{Type: model.StreamEventMessageDelta, Text: msg.Content[0].Text}); err != nil {
					return err
				}
			}
		}
		if err := handler(model.StreamEvent{Type: model.StreamEventDone, Raw: resp}); err != nil {
			return err
		}
	}
	return nil
}

func TestNewRunAppendsInputAndReturnsOutput(t *testing.T) {
	runtime := NewEngine(
		fakeClient{},
		WithInstructions("be helpful"),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
}

type captureClient struct {
	requests []model.ModelRequest
}

func (c *captureClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	c.requests = append(c.requests, req)
	return fakeClient{}.Generate(ctx, req)
}

func (c *captureClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	c.requests = append(c.requests, req)
	return fakeClient{}.GenerateStream(ctx, req, handler)
}

func TestRunAppendsMultimodalInputParts(t *testing.T) {
	client := &captureClient{}
	runtime := NewEngine(client)

	_, err := runtime.Run(context.Background(), RunRequest{
		InputParts: []ContentPart{
			NewTextPart("describe this image"),
			NewImagePart("https://example.com/cat.png", ImageDetailLow),
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("unexpected request count: %d", len(client.requests))
	}
	if len(client.requests[0].Input) != 1 {
		t.Fatalf("unexpected input count: %d", len(client.requests[0].Input))
	}
	user, ok := client.requests[0].Input[0].(model.UserMessageItem)
	if !ok {
		t.Fatalf("unexpected input type: %T", client.requests[0].Input[0])
	}
	if len(user.Content) != 2 {
		t.Fatalf("unexpected content parts: %+v", user.Content)
	}
	if user.Content[0].Text != "describe this image" || user.Content[1].ImageURL != "https://example.com/cat.png" || user.Content[1].Detail != model.ImageDetailLow {
		t.Fatalf("unexpected content: %+v", user.Content)
	}
}

func TestRunRejectsInputAndInputPartsTogether(t *testing.T) {
	client := &captureClient{}
	runtime := NewEngine(client)

	result, err := runtime.Run(context.Background(), RunRequest{
		Input: "hello",
		InputParts: []ContentPart{
			NewImagePart("https://example.com/cat.png", ImageDetailAuto),
		},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected no model requests, got %d", len(client.requests))
	}
	var engineErr *EngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if engineErr.Kind != EngineErrorInvalidRequest {
		t.Fatalf("expected invalid request error, got %q", engineErr.Kind)
	}
}

func TestRunStreamRejectsInputAndInputPartsTogether(t *testing.T) {
	client := &captureClient{}
	runtime := NewEngine(client)

	result, err := runtime.RunStream(context.Background(), RunRequest{
		Input: "hello",
		InputParts: []ContentPart{
			NewImagePart("https://example.com/cat.png", ImageDetailAuto),
		},
	}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected no model requests, got %d", len(client.requests))
	}
	var engineErr *EngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if engineErr.Kind != EngineErrorInvalidRequest {
		t.Fatalf("expected invalid request error, got %q", engineErr.Kind)
	}
}

type toolCallingClient struct {
	calls    int
	requests []model.ModelRequest
}

func (c *toolCallingClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	c.calls++
	c.requests = append(c.requests, req)
	if c.calls == 1 {
		return &model.ModelResponse{
			Output: []model.OutputItem{
				model.AssistantOutput{
					Role:    "assistant",
					Content: []model.TextPart{{Text: "checking"}},
					ToolCalls: []model.ToolCallOutput{
						{
							CallID:       "call_1",
							Name:         "submit",
							RawArguments: `{"value":"ok"}`,
						},
					},
				},
			},
			FinishReason: "tool_calls",
		}, nil
	}
	return &model.ModelResponse{
		Output: []model.OutputItem{
			model.AssistantOutput{
				Role:    "assistant",
				Content: []model.TextPart{{Text: "done"}},
			},
		},
		FinishReason: "stop",
	}, nil
}

func TestRunInjectsCurrentTimeWithoutPersistingIt(t *testing.T) {
	client := &captureClient{}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	fixed := time.Date(2026, time.July, 20, 7, 30, 45, 0, time.UTC)
	var hookRequest ModelRequest
	runtime := NewEngine(
		client,
		WithInstructions("be helpful"),
		WithCurrentTime(CurrentTimeConfig{Location: location}),
		WithHooks(Hooks{
			OnModelRequest: func(event ModelRequestEvent) error {
				hookRequest = event.Request
				return nil
			},
		}),
	)
	runtime.currentTime.now = func() time.Time { return fixed }

	result, err := runtime.Run(context.Background(), RunRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("unexpected request count: %d", len(client.requests))
	}
	request := client.requests[0]
	if len(request.Input) != 3 {
		t.Fatalf("unexpected input count: %d", len(request.Input))
	}
	first, ok := request.Input[0].(model.SystemMessageItem)
	if !ok || firstText(first.Content) != "be helpful" {
		t.Fatalf("unexpected instructions item: %#v", request.Input[0])
	}
	current, ok := request.Input[1].(model.SystemMessageItem)
	if !ok {
		t.Fatalf("unexpected current time item: %T", request.Input[1])
	}
	want := "Current date and time: 2026-07-20T15:30:45+08:00 (Asia/Shanghai)."
	if got := firstText(current.Content); got != want {
		t.Fatalf("unexpected current time: %q", got)
	}
	if len(hookRequest.Input) != len(request.Input) {
		t.Fatalf("hook did not receive final model request: %+v", hookRequest.Input)
	}
	for _, item := range result.Session.Items {
		if system, ok := item.(model.SystemMessageItem); ok && firstText(system.Content) == want {
			t.Fatal("current time was persisted in the session")
		}
	}
}

func TestRunUsesOneCurrentTimeAcrossToolLoop(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}
	client := &toolCallingClient{}
	nowCalls := 0
	runtime := NewEngine(
		client,
		WithTools(runTool),
		WithCurrentTime(CurrentTimeConfig{}),
	)
	runtime.currentTime.now = func() time.Time {
		nowCalls++
		return time.Date(2026, time.July, 20, 7, 30, nowCalls, 0, time.UTC)
	}

	_, err = runtime.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if nowCalls != 1 {
		t.Fatalf("expected one clock read, got %d", nowCalls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model requests, got %d", len(client.requests))
	}
	first := firstText(client.requests[0].Input[0].(model.SystemMessageItem).Content)
	second := firstText(client.requests[1].Input[0].(model.SystemMessageItem).Content)
	if first != second {
		t.Fatalf("tool loop used different times: %q and %q", first, second)
	}
}

func TestRunRefreshesCurrentTimeWhenReusingSession(t *testing.T) {
	client := &captureClient{}
	runtime := NewEngine(client, WithCurrentTime(CurrentTimeConfig{}))
	nowCalls := 0
	runtime.currentTime.now = func() time.Time {
		nowCalls++
		return time.Date(2026, time.July, 20, 7, 30, nowCalls, 0, time.UTC)
	}
	session := NewSession()

	_, err := runtime.Run(context.Background(), RunRequest{Session: session, Input: "first"})
	if err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	_, err = runtime.Run(context.Background(), RunRequest{Session: session, Input: "second"})
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if nowCalls != 2 {
		t.Fatalf("expected one clock read per run, got %d", nowCalls)
	}
	first := firstText(client.requests[0].Input[0].(model.SystemMessageItem).Content)
	second := firstText(client.requests[1].Input[0].(model.SystemMessageItem).Content)
	if first == second {
		t.Fatalf("reused session did not refresh current time: %q", first)
	}
	for _, item := range session.Items {
		if _, ok := item.(model.SystemMessageItem); ok {
			t.Fatalf("current time was persisted in reused session: %+v", session.Items)
		}
	}
}

func TestRunStreamInjectsCurrentTimeWithRequestLocationOverride(t *testing.T) {
	client := &captureClient{}
	runtime := NewEngine(client, WithCurrentTime(CurrentTimeConfig{}))
	runtime.currentTime.now = func() time.Time {
		return time.Date(2026, time.July, 20, 7, 30, 45, 0, time.UTC)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)

	_, err := runtime.RunStream(context.Background(), RunRequest{
		Input:               "hello",
		CurrentTimeLocation: location,
	}, nil)
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("unexpected request count: %d", len(client.requests))
	}
	current, ok := client.requests[0].Input[0].(model.SystemMessageItem)
	if !ok {
		t.Fatalf("unexpected current time item: %T", client.requests[0].Input[0])
	}
	want := "Current date and time: 2026-07-20T15:30:45+08:00 (Asia/Shanghai)."
	if got := firstText(current.Content); got != want {
		t.Fatalf("unexpected current time: %q", got)
	}
}

func (c *toolCallingClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	resp, err := c.Generate(ctx, req)
	if err != nil {
		return err
	}
	if handler != nil {
		if len(resp.Output) > 0 {
			if msg, ok := resp.Output[0].(model.AssistantOutput); ok && len(msg.Content) > 0 {
				if err := handler(model.StreamEvent{Type: model.StreamEventMessageDelta, Text: msg.Content[0].Text}); err != nil {
					return err
				}
			}
		}
		if err := handler(model.StreamEvent{Type: model.StreamEventDone, Raw: resp}); err != nil {
			return err
		}
	}
	return nil
}

type submitArgs struct {
	Value string `json:"value" tool:"required"`
}

type submitTool struct{}

func (submitTool) Name() string {
	return "submit"
}

func (submitTool) Description() string {
	return "submit payload"
}

func (submitTool) Run(ctx context.Context, call ToolCallContext, args submitArgs) (ToolResult, error) {
	return ToolResult{Message: "submitted"}, nil
}

type capturingSubmitTool struct {
	value    string
	repaired bool
}

func (*capturingSubmitTool) Name() string        { return "submit" }
func (*capturingSubmitTool) Description() string { return "submit payload" }

func (t *capturingSubmitTool) Run(ctx context.Context, call ToolCallContext, args submitArgs) (ToolResult, error) {
	t.value = args.Value
	t.repaired = call.Repaired
	return ToolResult{Message: "submitted"}, nil
}

type rawToolCallingClient struct{}

func (rawToolCallingClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	return rawToolCallResponse(), nil
}

func (rawToolCallingClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	if handler != nil {
		return handler(model.StreamEvent{Type: model.StreamEventDone, Raw: rawToolCallResponse()})
	}
	return nil
}

func rawToolCallResponse() *model.ModelResponse {
	return &model.ModelResponse{
		Output: []model.OutputItem{
			model.AssistantOutput{
				Role: "assistant",
				ToolCalls: []model.ToolCallOutput{{
					CallID:       "call_raw",
					Name:         "submit",
					RawArguments: `{"value":"content includes "quoted" text"}`,
				}},
			},
		},
		FinishReason: "tool_calls",
	}
}

func TestEnginePassesRawArgumentsToTypedRepair(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Engine) error
	}{
		{
			name: "non-streaming",
			run: func(engine *Engine) error {
				_, err := engine.Run(context.Background(), RunRequest{Input: "submit"})
				return err
			},
		},
		{
			name: "streaming",
			run: func(engine *Engine) error {
				_, err := engine.RunStream(context.Background(), RunRequest{Input: "submit"}, nil)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &capturingSubmitTool{}
			tool, err := NewTool[submitArgs](runner)
			if err != nil {
				t.Fatalf("NewTool returned error: %v", err)
			}
			engine := NewEngine(rawToolCallingClient{}, WithTools(tool), WithStopAfterToolCall(true))
			if err := tt.run(engine); err != nil {
				t.Fatalf("run returned error: %v", err)
			}
			if runner.value != `content includes "quoted" text` || !runner.repaired {
				t.Fatalf("unexpected typed repair: value=%q repaired=%v", runner.value, runner.repaired)
			}
		})
	}
}

func TestRunCanStopAfterSuccessfulToolCallFromEngineOption(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client := &toolCallingClient{}
	runtime := NewEngine(
		client,
		WithTools(runTool),
		WithStopAfterToolCall(true),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected one model call, got %d", client.calls)
	}
	if result.StopReason != StopReasonStopOnToolResult {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if result.OutputText != "checking" {
		t.Fatalf("expected pre-tool assistant content, got %q", result.OutputText)
	}
}

func TestRunUsesDefaultMaxModelCallsFromOptions(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client := &toolCallingClient{}
	runtime := NewEngine(
		client,
		WithTools(runTool),
		WithMaxModelCalls(1),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
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
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&toolCallingClient{},
		WithTools(runTool),
		WithStopAfterToolCall(true),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
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
	runtime := NewEngine(fakeClient{})

	result, err := runtime.Run(context.Background(), RunRequest{Input: "hello"})
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
	session.AppendItems(model.ToolResultItem{
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

func (c *usageClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	c.calls++
	if c.calls == 1 {
		return &model.ModelResponse{
			Output: []model.OutputItem{
				model.AssistantOutput{
					Role: "assistant",
					ToolCalls: []model.ToolCallOutput{
						{
							CallID:       "call_1",
							Name:         "submit",
							RawArguments: `{"value":"ok"}`,
						},
					},
				},
			},
			FinishReason: "tool_calls",
			Usage: model.Usage{
				InputTokens:       10,
				OutputTokens:      3,
				TotalTokens:       13,
				CachedInputTokens: 4,
			},
			RetryCount: 1,
		}, nil
	}
	return &model.ModelResponse{
		Output: []model.OutputItem{
			model.AssistantOutput{
				Role:    "assistant",
				Content: []model.TextPart{{Text: "done"}},
			},
		},
		FinishReason: "stop",
		Usage: model.Usage{
			InputTokens:       8,
			OutputTokens:      2,
			TotalTokens:       10,
			CachedInputTokens: 1,
		},
		RetryCount: 2,
	}, nil
}

func (c *usageClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	resp, err := c.Generate(ctx, req)
	if err != nil {
		return err
	}
	if handler != nil {
		if len(resp.Output) > 0 {
			if msg, ok := resp.Output[0].(model.AssistantOutput); ok && len(msg.Content) > 0 {
				if err := handler(model.StreamEvent{Type: model.StreamEventMessageDelta, Text: msg.Content[0].Text}); err != nil {
					return err
				}
			}
		}
		if err := handler(model.StreamEvent{Type: model.StreamEventDone, Raw: resp}); err != nil {
			return err
		}
	}
	return nil
}

type failingTool struct{}

func (failingTool) Name() string {
	return "submit"
}

func (failingTool) Description() string {
	return "submit payload"
}

func (failingTool) Run(ctx context.Context, call ToolCallContext, args submitArgs) (ToolResult, error) {
	return ToolResult{}, errors.New("submit failed")
}

func TestRunReturnsResultWhenToolFails(t *testing.T) {
	runTool, err := NewTool[submitArgs](failingTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&toolCallingClient{},
		WithTools(runTool),
		WithMaxModelCalls(1),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
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
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
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

func TestRunAggregatesRetryCountAcrossModelCalls(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.RetryCount != 3 {
		t.Fatalf("unexpected retry count: %d", result.RetryCount)
	}
}

func TestRunStreamAggregatesRetryCountAcrossModelCalls(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := runtime.RunStream(context.Background(), RunRequest{Input: "submit"}, nil)
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	if result.RetryCount != 3 {
		t.Fatalf("unexpected retry count: %d", result.RetryCount)
	}
}

func TestRunCopiesUsageOntoSessionSnapshot(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
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

func TestRunResultJSONUsesStableFieldNames(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runtime := NewEngine(
		&usageClient{},
		WithTools(runTool),
	)

	result, err := runtime.Run(context.Background(), RunRequest{Input: "submit"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	text := string(data)

	for _, want := range []string{
		`"output_text"`,
		`"model_call_count"`,
		`"tool_call_count"`,
		`"tool_results"`,
		`"stop_reason"`,
		`"retry_count"`,
		`"last_response"`,
		`"finish_reason"`,
		`"response_id"`,
		`"call_id"`,
		`"raw_arguments"`,
		`"input_tokens"`,
		`"cached_input_tokens"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected marshaled JSON to contain %s: %s", want, text)
		}
	}

	for _, unwanted := range []string{
		`"OutputText"`,
		`"ModelCallCount"`,
		`"ToolCallCount"`,
		`"LastResponse"`,
		`"FinishReason"`,
		`"CallID"`,
		`"InputTokens"`,
	} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("expected marshaled JSON to avoid %s: %s", unwanted, text)
		}
	}
}

type errorClient struct{}

func (errorClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	return nil, errors.New("model unavailable")
}

func (errorClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	return errors.New("model unavailable")
}

func TestRunReturnsExecutionErrorWhenModelCallFails(t *testing.T) {
	runtime := NewEngine(errorClient{})

	result, err := runtime.Run(context.Background(), RunRequest{Input: "hello"})
	if err == nil {
		t.Fatalf("expected execution error")
	}
	if result != nil {
		t.Fatalf("expected nil result on execution error, got %+v", result)
	}
	var engineErr *EngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if engineErr.Kind != EngineErrorModelRequest {
		t.Fatalf("expected model request error, got %q", engineErr.Kind)
	}
}

type contextErrorClient struct{}

func (contextErrorClient) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	return nil, context.DeadlineExceeded
}

func (contextErrorClient) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	return context.DeadlineExceeded
}

func TestRunWrapsTimeoutErrors(t *testing.T) {
	runtime := NewEngine(contextErrorClient{})

	_, err := runtime.Run(context.Background(), RunRequest{Input: "hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	var engineErr *EngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if engineErr.Kind != EngineErrorTimeout {
		t.Fatalf("expected timeout error, got %q", engineErr.Kind)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wrapped deadline error, got %v", err)
	}
}

func TestRunWrapsHookErrors(t *testing.T) {
	wantErr := errors.New("hook failed")
	runtime := NewEngine(fakeClient{}, WithHooks(Hooks{
		OnModelRequest: func(ModelRequestEvent) error {
			return wantErr
		},
	}))

	_, err := runtime.Run(context.Background(), RunRequest{Input: "hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	var engineErr *EngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if engineErr.Kind != EngineErrorHook {
		t.Fatalf("expected hook error, got %q", engineErr.Kind)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped hook error, got %v", err)
	}
}

func TestRunStreamEmitsToolLifecycleAndReturnsResult(t *testing.T) {
	runTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	client := &toolCallingClient{}
	runtime := NewEngine(client, WithTools(runTool))

	var events []StreamEvent
	result, err := runtime.RunStream(context.Background(), RunRequest{Input: "submit"}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output text: %q", result.OutputText)
	}
	if len(events) != 5 {
		t.Fatalf("unexpected event count: %d", len(events))
	}
	if events[0].Type != StreamEventMessageDelta || events[0].Text != "checking" {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != StreamEventToolStart || events[1].ToolName != "submit" {
		t.Fatalf("unexpected second event: %+v", events[1])
	}
	if events[2].Type != StreamEventToolFinish || events[2].ToolName != "submit" {
		t.Fatalf("unexpected third event: %+v", events[2])
	}
	if events[3].Type != StreamEventMessageDelta || events[3].Text != "done" {
		t.Fatalf("unexpected fourth event: %+v", events[3])
	}
	if events[4].Type != StreamEventDone {
		t.Fatalf("unexpected final event: %+v", events[4])
	}
}

func TestRunStreamStopsWhenHandlerReturnsError(t *testing.T) {
	runtime := NewEngine(fakeClient{})
	wantErr := errors.New("stop streaming")

	result, err := runtime.RunStream(context.Background(), RunRequest{Input: "hello"}, func(event StreamEvent) error {
		if event.Type == StreamEventMessageDelta {
			return wantErr
		}
		return nil
	})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected handler error, got result=%+v err=%v", result, err)
	}
	var engineErr *EngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("expected EngineError, got %T", err)
	}
	if engineErr.Kind != EngineErrorHandler {
		t.Fatalf("expected handler error, got %q", engineErr.Kind)
	}
	if result != nil {
		t.Fatalf("expected nil result when handler fails, got %+v", result)
	}
}
