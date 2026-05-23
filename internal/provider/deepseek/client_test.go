package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

func TestClientUsesProviderOwnedModelAndSamplingOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "deepseek-v4-flash" {
			t.Fatalf("unexpected model: %#v", body["model"])
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

	deepseekProvider := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	)
	temperature := 0.6
	topP := 0.7
	maxTokens := 512
	client := deepseekProvider.ChatClient(ChatClientConfig{
		Model:       "deepseek-v4-flash",
		Temperature: &temperature,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
	})
	resp, err := client.Generate(context.Background(), model.ModelRequest{
		Input: []model.InputItem{
			model.UserMessageItem{Content: []model.TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
	if resp.Provider != "deepseek" {
		t.Fatalf("unexpected provider: %q", resp.Provider)
	}
}

func TestProviderSupportsRetryOption(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
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

	deepseekProvider := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithRetry(RetryConfig{
			MaxAttempts:    2,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		}),
	)
	client := deepseekProvider.ChatClient(ChatClientConfig{Model: "deepseek-v4-flash"})
	_, err := client.Generate(context.Background(), model.ModelRequest{
		Input: []model.InputItem{
			model.UserMessageItem{Content: []model.TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestProviderSupportsTimeoutOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
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

	deepseekProvider := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithTimeout(10*time.Millisecond),
	)
	client := deepseekProvider.ChatClient(ChatClientConfig{Model: "deepseek-v4-flash"})
	_, err := client.Generate(context.Background(), model.ModelRequest{
		Input: []model.InputItem{
			model.UserMessageItem{Content: []model.TextPart{{Text: "hello"}}},
		},
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}
