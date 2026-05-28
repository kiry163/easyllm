package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		thinking, ok := body["thinking"].(map[string]any)
		if !ok || thinking["type"] != "enabled" {
			t.Fatalf("unexpected thinking payload: %#v", body["thinking"])
		}
		if body["reasoning_effort"] != "high" {
			t.Fatalf("unexpected reasoning_effort: %#v", body["reasoning_effort"])
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

func TestClientSupportsThinkingDefaultsAndOverrides(t *testing.T) {
	t.Run("disable thinking via config", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			thinking, ok := body["thinking"].(map[string]any)
			if !ok || thinking["type"] != "disabled" {
				t.Fatalf("unexpected thinking payload: %#v", body["thinking"])
			}
			if _, ok := body["reasoning_effort"]; ok {
				t.Fatalf("did not expect reasoning_effort when thinking is disabled: %#v", body["reasoning_effort"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "done"}, "finish_reason": "stop"}},
			})
		}))
		defer server.Close()

		thinking := false
		client := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL)).ChatClient(ChatClientConfig{
			Model:    "deepseek-v4-flash",
			Thinking: &thinking,
		})
		_, err := client.Generate(context.Background(), model.ModelRequest{
			Input: []model.InputItem{
				model.UserMessageItem{Content: []model.TextPart{{Text: "hello"}}},
			},
		})
		if err != nil {
			t.Fatalf("Generate returned error: %v", err)
		}
	})

	t.Run("extra body overrides defaults", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			thinking, ok := body["thinking"].(map[string]any)
			if !ok || thinking["type"] != "disabled" {
				t.Fatalf("unexpected thinking payload: %#v", body["thinking"])
			}
			if body["reasoning_effort"] != "low" {
				t.Fatalf("unexpected reasoning_effort override: %#v", body["reasoning_effort"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "done"}, "finish_reason": "stop"}},
			})
		}))
		defer server.Close()

		client := NewProvider(
			WithAPIKey("token"),
			WithBaseURL(server.URL),
			WithExtraBody(map[string]any{
				"thinking":         map[string]any{"type": "disabled"},
				"reasoning_effort": "low",
			}),
		).ChatClient(ChatClientConfig{Model: "deepseek-v4-flash"})
		_, err := client.Generate(context.Background(), model.ModelRequest{
			Input: []model.InputItem{
				model.UserMessageItem{Content: []model.TextPart{{Text: "hello"}}},
			},
		})
		if err != nil {
			t.Fatalf("Generate returned error: %v", err)
		}
	})
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
			MaxRetries:     2,
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

func TestClientGenerateStreamUsesCompatStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_ds","choices":[{"delta":{"content":"deep"},"finish_reason":""}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"seek"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	client := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL)).ChatClient(ChatClientConfig{Model: "deepseek-chat"})
	var gotText string
	var done *model.ModelResponse
	err := client.GenerateStream(context.Background(), model.ModelRequest{
		Input: []model.InputItem{model.UserMessageItem{Content: []model.TextPart{{Text: "hello"}}}},
	}, func(event model.StreamEvent) error {
		switch event.Type {
		case model.StreamEventMessageDelta:
			gotText += event.Text
		case model.StreamEventDone:
			done, _ = event.Raw.(*model.ModelResponse)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if gotText != "deepseek" {
		t.Fatalf("unexpected streamed text: %q", gotText)
	}
	if done == nil || done.Provider != "deepseek" || done.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected done response: %+v", done)
	}
}
