package compat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kiry163/easyllm/internal/model"
)

// The payloads below mirror the DeepSeek responses API: a thinking turn returns
// a reasoning item followed by a message or function_call item, and streaming
// ends with response.completed / response.incomplete / response.failed without a
// data: [DONE] marker.

func TestResponsesRequestReplaysReasoningItem(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[]}`))
	}))
	defer server.Close()

	req := model.ModelRequest{Input: []model.InputItem{
		model.UserMessageItem{Content: []model.TextPart{model.NewTextPart("call ping")}},
		model.AssistantMessageItem{
			ToolCalls:        []model.ToolCallItem{{CallID: "call_1", Name: "ping", RawArguments: `{"value":1}`}},
			ReasoningContent: "weigh options",
		},
		model.ToolResultItem{CallID: "call_1", Content: `{"ok":true}`},
		model.AssistantMessageItem{Content: []model.TextPart{model.NewTextPart("done")}},
	}}
	if _, err := NewResponsesClient("token", server.URL).Generate(context.Background(), req); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	input, ok := body["input"].([]any)
	if !ok || len(input) != 6 {
		t.Fatalf("unexpected input payload: %#v", body["input"])
	}
	reasoning, ok := input[1].(map[string]any)
	if !ok || reasoning["type"] != "reasoning" {
		t.Fatalf("expected a reasoning item before the assistant turn, got %#v", input[1])
	}
	parts, ok := reasoning["content"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("unexpected reasoning content: %#v", reasoning["content"])
	}
	part, ok := parts[0].(map[string]any)
	if !ok || part["type"] != "reasoning_text" || part["text"] != "weigh options" {
		t.Fatalf("unexpected reasoning part: %#v", parts[0])
	}
	message, ok := input[2].(map[string]any)
	if !ok || message["role"] != "assistant" {
		t.Fatalf("expected the assistant message after the reasoning item, got %#v", input[2])
	}
	call, ok := input[3].(map[string]any)
	if !ok || call["type"] != "function_call" || call["call_id"] != "call_1" {
		t.Fatalf("expected the function call after the assistant message, got %#v", input[3])
	}
	result, ok := input[4].(map[string]any)
	if !ok || result["type"] != "function_call_output" || result["call_id"] != "call_1" {
		t.Fatalf("expected the function call output after the call, got %#v", input[4])
	}
	trailing, ok := input[5].(map[string]any)
	if !ok || trailing["role"] != "assistant" {
		t.Fatalf("expected the trailing assistant message, got %#v", input[5])
	}
}

func TestResponsesResponseFoldsTurnItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[
			{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"weigh "},{"type":"reasoning_text","text":"options"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"calling ping"}]},
			{"type":"function_call","call_id":"call_1","name":"ping","arguments":"{\"value\":1}"}
		],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14,"input_tokens_details":{"cached_tokens":6},"output_tokens_details":{"reasoning_tokens":3}}}`))
	}))
	defer server.Close()

	resp, err := NewResponsesClient("token", server.URL).Generate(context.Background(), model.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("expected one assistant turn, got %+v", resp.Output)
	}
	assistant, ok := resp.Output[0].(model.AssistantOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", resp.Output[0])
	}
	if assistant.ReasoningContent != "weigh options" {
		t.Fatalf("unexpected reasoning content: %q", assistant.ReasoningContent)
	}
	if len(assistant.Content) != 1 || assistant.Content[0].Text != "calling ping" {
		t.Fatalf("unexpected content: %+v", assistant.Content)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].CallID != "call_1" || assistant.ToolCalls[0].RawArguments != `{"value":1}` {
		t.Fatalf("unexpected tool calls: %+v", assistant.ToolCalls)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %q", resp.FinishReason)
	}
	if resp.Usage.CachedInputTokens != 6 {
		t.Fatalf("unexpected cached input tokens: %d", resp.Usage.CachedInputTokens)
	}
	if got := resp.Usage.Details["reasoning_tokens"]; got != 3 {
		t.Fatalf("unexpected reasoning tokens: %#v", got)
	}
}

func TestResponsesResponseHandlesIncompleteAndFailed(t *testing.T) {
	t.Run("incomplete keeps the truncated turn", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[
				{"type":"reasoning","content":[{"type":"reasoning_text","text":"cut off"}]}
			],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
		}))
		defer server.Close()

		resp, err := NewResponsesClient("token", server.URL).Generate(context.Background(), model.ModelRequest{})
		if err != nil {
			t.Fatalf("Generate returned error: %v", err)
		}
		if resp.FinishReason != "length" {
			t.Fatalf("unexpected finish reason: %q", resp.FinishReason)
		}
		if len(resp.Output) != 1 {
			t.Fatalf("expected the truncated turn to survive, got %+v", resp.Output)
		}
		assistant, ok := resp.Output[0].(model.AssistantOutput)
		if !ok || assistant.ReasoningContent != "cut off" {
			t.Fatalf("unexpected output: %+v", resp.Output)
		}
	})

	t.Run("failed surfaces the provider error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","status":"failed","error":{"code":"server_error","message":"upstream exploded"}}`))
		}))
		defer server.Close()

		_, err := NewResponsesClient("token", server.URL).Generate(context.Background(), model.ModelRequest{})
		if err == nil {
			t.Fatal("expected an error for a failed response")
		}
		if !strings.Contains(err.Error(), "upstream exploded") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResponsesStreamEmitsReasoningDeltasAndTerminalResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.created"}`,
			``,
			`data: {"type":"response.reasoning_text.delta","delta":"weigh "}`,
			``,
			`data: {"type":"response.reasoning_text.delta","delta":"options"}`,
			``,
			`data: {"type":"response.output_text.delta","delta":"calling ping"}`,
			``,
			`data: {"type":"response.function_call_arguments.delta","delta":"{\"value\":1}"}`,
			``,
			`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","content":[{"type":"reasoning_text","text":"weigh options"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"calling ping"}]},{"type":"function_call","call_id":"call_1","name":"ping","arguments":"{\"value\":1}"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"output_tokens_details":{"reasoning_tokens":2}}}}`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	var events []model.StreamEvent
	var done *model.ModelResponse
	err := NewResponsesClient("token", server.URL).GenerateStream(context.Background(), model.ModelRequest{}, func(event model.StreamEvent) error {
		events = append(events, event)
		if event.Type == model.StreamEventDone {
			done, _ = event.Raw.(*model.ModelResponse)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	want := []model.StreamEvent{
		{Type: model.StreamEventReasoningDelta, Text: "weigh "},
		{Type: model.StreamEventReasoningDelta, Text: "options"},
		{Type: model.StreamEventMessageDelta, Text: "calling ping"},
	}
	if len(events) != len(want)+1 {
		t.Fatalf("unexpected events: %+v", events)
	}
	for i, expected := range want {
		if events[i].Type != expected.Type || events[i].Text != expected.Text {
			t.Fatalf("unexpected event %d: %+v", i, events[i])
		}
	}
	if events[len(events)-1].Type != model.StreamEventDone {
		t.Fatalf("unexpected final event: %+v", events[len(events)-1])
	}
	if done == nil || len(done.Output) != 1 {
		t.Fatalf("missing done response: %+v", done)
	}
	assistant, ok := done.Output[0].(model.AssistantOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", done.Output[0])
	}
	if assistant.ReasoningContent != "weigh options" {
		t.Fatalf("unexpected reasoning content: %q", assistant.ReasoningContent)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Name != "ping" {
		t.Fatalf("expected the tool call from the terminal response, got %+v", assistant.ToolCalls)
	}
}

func TestResponsesStreamTerminalEvents(t *testing.T) {
	t.Run("incomplete keeps the truncated turn", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"type":"response.reasoning_text.delta","delta":"cut off"}`,
				``,
				`data: {"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning","content":[{"type":"reasoning_text","text":"cut off"}]}]}}`,
				``,
			}, "\n")))
		}))
		defer server.Close()

		var done *model.ModelResponse
		err := NewResponsesClient("token", server.URL).GenerateStream(context.Background(), model.ModelRequest{}, func(event model.StreamEvent) error {
			if event.Type == model.StreamEventDone {
				done, _ = event.Raw.(*model.ModelResponse)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("GenerateStream returned error: %v", err)
		}
		if done == nil || done.FinishReason != "length" || len(done.Output) != 1 {
			t.Fatalf("unexpected done response: %+v", done)
		}
	})

	t.Run("failed surfaces the provider error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"server_error","message":"upstream exploded"}}}`,
				``,
			}, "\n")))
		}))
		defer server.Close()

		err := NewResponsesClient("token", server.URL).GenerateStream(context.Background(), model.ModelRequest{}, nil)
		if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
			t.Fatalf("expected the provider error, got %v", err)
		}
	})

	t.Run("stream without a terminal event keeps accumulated reasoning", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"type":"response.reasoning_text.delta","delta":"partial"}`,
				``,
				`data: {"type":"response.output_text.delta","delta":"answer"}`,
				``,
			}, "\n")))
		}))
		defer server.Close()

		var done *model.ModelResponse
		err := NewResponsesClient("token", server.URL).GenerateStream(context.Background(), model.ModelRequest{}, func(event model.StreamEvent) error {
			if event.Type == model.StreamEventDone {
				done, _ = event.Raw.(*model.ModelResponse)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("GenerateStream returned error: %v", err)
		}
		if done == nil || len(done.Output) != 1 {
			t.Fatalf("unexpected done response: %+v", done)
		}
		assistant, ok := done.Output[0].(model.AssistantOutput)
		if !ok || assistant.ReasoningContent != "partial" {
			t.Fatalf("unexpected output: %+v", done.Output)
		}
	})
}
