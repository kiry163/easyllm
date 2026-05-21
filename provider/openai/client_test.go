package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kiry163/easyllm/provider"
)

func TestChatClientRetriesAndNormalizesToolCalls(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if attempts == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "tool call",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "submit",
									"arguments": "\"{\\\"value\\\":\\\"ok\\\"}\"",
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     3,
				"completion_tokens": 5,
				"total_tokens":      8,
			},
		})
	}))
	defer server.Close()

	client := NewChatClient("token", server.URL, WithRetry(RetryConfig{MaxAttempts: 2}))
	resp, err := client.Generate(context.Background(), provider.ModelRequest{Model: "gpt-test"})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %q", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
	call, ok := resp.Output[0].(provider.ToolCallOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", resp.Output[0])
	}
	if call.Name != "submit" || call.Arguments["value"] != "ok" || !call.Repaired {
		t.Fatalf("unexpected tool call output: %+v", call)
	}
}

func TestResponsesClientNormalizesAssistantMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_123",
			"output": []map[string]any{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "done",
						},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  7,
				"output_tokens": 2,
				"total_tokens":  9,
			},
		})
	}))
	defer server.Close()

	client := NewResponsesClient("token", server.URL)
	resp, err := client.Generate(context.Background(), provider.ModelRequest{Model: "gpt-test"})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.ResponseID != "resp_123" {
		t.Fatalf("unexpected response id: %q", resp.ResponseID)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
	msg, ok := resp.Output[0].(provider.MessageOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", resp.Output[0])
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "done" {
		t.Fatalf("unexpected message output: %+v", msg)
	}
}
