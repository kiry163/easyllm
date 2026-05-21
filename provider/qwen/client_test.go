package qwen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kiry163/easyllm/provider"
)

func TestClientUsesProviderOwnedModelAndThinkingDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "qwen-plus" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		if body["enable_thinking"] != false {
			t.Fatalf("unexpected enable_thinking: %#v", body["enable_thinking"])
		}
		if body["temperature"] != 0.6 {
			t.Fatalf("unexpected temperature: %#v", body["temperature"])
		}
		if body["top_p"] != 0.7 {
			t.Fatalf("unexpected top_p: %#v", body["top_p"])
		}
		if body["max_tokens"] != float64(512) {
			t.Fatalf("unexpected max_tokens: %#v", body["max_tokens"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	qwenProvider := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	)
	thinking := false
	temperature := 0.6
	topP := 0.7
	maxTokens := 512
	client := qwenProvider.ChatModel(ChatModelConfig{
		Model:       "qwen-plus",
		Thinking:    &thinking,
		Temperature: &temperature,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
	})
	resp, err := client.Generate(context.Background(), provider.ModelRequest{
		Input: []provider.InputItem{
			provider.UserMessageItem{Content: []provider.TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
}
