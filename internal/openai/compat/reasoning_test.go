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

func TestChatResponseParsesReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer","reasoning_content":"weigh options","tool_calls":[{"id":"call_1","function":{"name":"submit","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer server.Close()

	resp, err := NewChatClient("token", server.URL).Generate(context.Background(), model.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output: %+v", resp.Output)
	}
	assistant, ok := resp.Output[0].(model.AssistantOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", resp.Output[0])
	}
	if assistant.ReasoningContent != "weigh options" {
		t.Fatalf("unexpected reasoning content: %q", assistant.ReasoningContent)
	}
}

func TestChatRequestReplaysReasoningContent(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	req := model.ModelRequest{Input: []model.InputItem{
		model.UserMessageItem{Content: []model.TextPart{model.NewTextPart("hi")}},
		model.AssistantMessageItem{
			ToolCalls:        []model.ToolCallItem{{CallID: "call_1", Name: "submit", RawArguments: "{}"}},
			ReasoningContent: "step-1",
		},
		model.ToolResultItem{CallID: "call_1", Content: "{}"},
		model.AssistantMessageItem{Content: []model.TextPart{model.NewTextPart("plain")}},
	}}
	if _, err := NewChatClient("token", server.URL).Generate(context.Background(), req); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("unexpected messages payload: %#v", body["messages"])
	}
	carrier, ok := messages[1].(map[string]any)
	if !ok {
		t.Fatalf("unexpected assistant message: %#v", messages[1])
	}
	if carrier["reasoning_content"] != "step-1" {
		t.Fatalf("expected reasoning_content on the assistant message, got %#v", carrier)
	}
	plain, ok := messages[3].(map[string]any)
	if !ok {
		t.Fatalf("unexpected assistant message: %#v", messages[3])
	}
	if _, ok := plain["reasoning_content"]; ok {
		t.Fatalf("expected no reasoning_content without reasoning, got %#v", plain)
	}
}

func TestReasoningOnlyResponseKeepsAssistantOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"cut off\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"reasoning_content":"cut off"},"finish_reason":"length"}]}`))
	}))
	defer server.Close()

	for _, stream := range []bool{false, true} {
		client := NewChatClient("token", server.URL)
		var resp *model.ModelResponse
		if stream {
			if err := client.GenerateStream(context.Background(), model.ModelRequest{}, func(event model.StreamEvent) error {
				if event.Type == model.StreamEventDone {
					resp, _ = event.Raw.(*model.ModelResponse)
				}
				return nil
			}); err != nil {
				t.Fatalf("GenerateStream returned error: %v", err)
			}
		} else {
			var err error
			resp, err = client.Generate(context.Background(), model.ModelRequest{})
			if err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
		}
		if resp == nil || len(resp.Output) != 1 {
			t.Fatalf("stream=%v: reasoning-only response lost its output: %+v", stream, resp)
		}
		assistant, ok := resp.Output[0].(model.AssistantOutput)
		if !ok || assistant.ReasoningContent != "cut off" {
			t.Fatalf("stream=%v: unexpected output: %+v", stream, resp.Output)
		}
	}
}

func TestChatStreamEmitsReasoningDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"reasoning_content":"weigh "}}]}`,
			``,
			`data: {"choices":[{"delta":{"reasoning_content":"options"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	var events []model.StreamEvent
	var done *model.ModelResponse
	err := NewChatClient("token", server.URL).GenerateStream(context.Background(), model.ModelRequest{}, func(event model.StreamEvent) error {
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
		{Type: model.StreamEventMessageDelta, Text: "answer"},
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
}
