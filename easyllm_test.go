package easyllm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientBuildsOpenAIClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "token",
		Model:    "gpt-4.1-mini",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
	if _, ok := any(client).(Client); !ok {
		t.Fatalf("unexpected client type: %T", client)
	}
}

func TestNewImageClientBuildsOpenAIImageClient(t *testing.T) {
	client, err := NewImageClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "token",
		Model:    "gpt-image-1",
	})
	if err != nil {
		t.Fatalf("NewImageClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("expected image client")
	}
}

func TestNewImageClientRejectsUnsupportedProvider(t *testing.T) {
	_, err := NewImageClient(Config{
		Provider: ProviderDeepSeek,
		APIKey:   "token",
		Model:    "deepseek-v4-flash",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != `provider "deepseek" does not support image generation` {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestNewImageClientRequiresPromptAtCallTime(t *testing.T) {
	client, err := NewImageClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "token",
		Model:    "gpt-image-1",
	})
	if err != nil {
		t.Fatalf("NewImageClient returned error: %v", err)
	}

	_, err = client.GenerateImage(context.Background(), ImageRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "prompt is required" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestNewImageClientUsesExtraBodyAndRequestOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["quality"] != "medium" {
			t.Fatalf("unexpected quality: %#v", body["quality"])
		}
		if body["background"] != "transparent" {
			t.Fatalf("unexpected background: %#v", body["background"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data":    []map[string]any{{"b64_json": "aGVsbG8="}},
		})
	}))
	defer server.Close()

	client, err := NewImageClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "token",
		BaseURL:  server.URL,
		Model:    "gpt-image-1",
		ExtraBody: map[string]any{
			"quality":    "low",
			"background": "opaque",
		},
	})
	if err != nil {
		t.Fatalf("NewImageClient returned error: %v", err)
	}

	_, err = client.GenerateImage(context.Background(), ImageRequest{
		Prompt:     "diagram",
		Background: "transparent",
		Options: map[string]any{
			"quality": "medium",
		},
	})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
}

func TestNewClientBuildsOpenAICompatibleClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  "http://localhost:12345/v1",
		Model:    "custom-model",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
}

func TestNewClientBuildsQwenClientWithThinkingOption(t *testing.T) {
	client, err := NewClient(Config{
		Provider: ProviderQwen,
		APIKey:   "token",
		Model:    "qwen-plus",
		Options: map[string]any{
			"thinking": false,
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
}

func TestNewClientMapsEnableThinkingAndExtraBodyOverridesRequestFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["enable_thinking"] != true {
			t.Fatalf("expected ExtraBody to override enable_thinking, got %#v", body["enable_thinking"])
		}
		if body["temperature"] != 0.9 {
			t.Fatalf("expected ExtraBody to override temperature, got %#v", body["temperature"])
		}
		if body["custom_flag"] != "on" {
			t.Fatalf("expected custom flag in request body, got %#v", body["custom_flag"])
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

	enableThinking := false
	temperature := 0.3
	client, err := NewClient(Config{
		Provider:       ProviderQwen,
		APIKey:         "token",
		BaseURL:        server.URL,
		Model:          "qwen-plus",
		Temperature:    &temperature,
		EnableThinking: &enableThinking,
		ExtraBody: map[string]any{
			"enable_thinking": true,
			"temperature":     0.9,
			"custom_flag":     "on",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	_, err = client.Generate(context.Background(), ModelRequest{
		Input: []InputItem{
			UserMessageItem{Content: []TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
}

func TestNewClientOpenAICompatibleUsesConfiguredEndpointAndExtraBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["model"] != "custom-model" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		if body["provider_flag"] != "custom" {
			t.Fatalf("expected provider flag, got %#v", body["provider_flag"])
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

	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "custom-model",
		ExtraBody: map[string]any{
			"provider_flag": "custom",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	resp, err := client.Generate(context.Background(), ModelRequest{
		Input: []InputItem{
			UserMessageItem{Content: []TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.Provider != ProviderOpenAICompatible {
		t.Fatalf("unexpected provider: %q", resp.Provider)
	}
}

func TestNewClientAppliesRetryBackoffConfig(t *testing.T) {
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

	start := time.Now()
	client, err := NewClient(Config{
		Provider:       ProviderOpenAICompatible,
		BaseURL:        server.URL,
		Model:          "custom-model",
		MaxRetries:     1,
		InitialBackoff: 40 * time.Millisecond,
		MaxBackoff:     40 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	_, err = client.Generate(context.Background(), ModelRequest{
		Input: []InputItem{
			UserMessageItem{Content: []TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("expected retry backoff to be applied, elapsed=%v", elapsed)
	}
}

func TestNewClientOpenAICompatibleGenerateStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["stream"] != true {
			t.Fatalf("expected stream=true, got %#v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_123","choices":[{"delta":{"content":"hel"},"finish_reason":""}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "custom-model",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	var done *ModelResponse
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventMessageDelta:
			text.WriteString(event.Text)
		case StreamEventDone:
			if current, ok := event.Raw.(*ModelResponse); ok {
				done = current
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if text.String() != "hello" {
		t.Fatalf("unexpected streamed text: %q", text.String())
	}
	if done == nil || done.Usage.TotalTokens != 5 || done.Provider != ProviderOpenAICompatible {
		t.Fatalf("unexpected done response: %+v", done)
	}
}

func TestNewClientOpenAICompatibleGenerateStreamResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"done"}`,
			``,
			`data: {"type":"response.completed","response":{"id":"resp_123","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider:  ProviderOpenAICompatible,
		BaseURL:   server.URL,
		Model:     "custom-model",
		Transport: TransportResponses,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var gotText string
	var done *ModelResponse
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventMessageDelta:
			gotText += event.Text
		case StreamEventDone:
			done, _ = event.Raw.(*ModelResponse)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if gotText != "done" {
		t.Fatalf("unexpected streamed text: %q", gotText)
	}
	if done == nil || done.ResponseID != "resp_123" || done.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected done response: %+v", done)
	}
}

func TestGenerateStreamReturnsIdleTimeoutWhenStreamStalls(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)

	client, err := NewClient(Config{
		Provider:                ProviderOpenAICompatible,
		BaseURL:                 server.URL,
		Model:                   "custom-model",
		StreamFirstEventTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, nil)
	if err == nil {
		t.Fatal("expected stream first event timeout error")
	}
	if !errors.Is(err, ErrStreamFirstEventTimeout) {
		t.Fatalf("expected ErrStreamFirstEventTimeout, got %v", err)
	}
	<-started
}

func TestGenerateStreamRetriesFirstEventTimeoutBeforeEmittingEvents(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if attempts == 1 {
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(50 * time.Millisecond)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"id":"chatcmpl_123","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider:                ProviderOpenAICompatible,
		BaseURL:                 server.URL,
		Model:                   "custom-model",
		StreamFirstEventTimeout: 10 * time.Millisecond,
		MaxRetries:              1,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		if event.Type == StreamEventMessageDelta {
			text.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if text.String() != "ok" {
		t.Fatalf("unexpected streamed text: %q", text.String())
	}
}

func TestGenerateStreamRetriesRequestTimeoutBeforeFirstEvent(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if attempts == 1 {
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(50 * time.Millisecond)
			return
		}
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_123","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider:   ProviderOpenAICompatible,
		BaseURL:    server.URL,
		Model:      "custom-model",
		Timeout:    10 * time.Millisecond,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		if event.Type == StreamEventMessageDelta {
			text.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if text.String() != "ok" {
		t.Fatalf("unexpected streamed text: %q", text.String())
	}
}

func TestGenerateStreamDoesNotApplyFirstEventTimeoutAfterFirstEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_123\",\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":\"\"}]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider:                ProviderOpenAICompatible,
		BaseURL:                 server.URL,
		Model:                   "custom-model",
		StreamFirstEventTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		if event.Type == StreamEventMessageDelta {
			text.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if text.String() != "hello" {
		t.Fatalf("unexpected streamed text: %q", text.String())
	}
}

func TestGenerateStreamDoesNotRetryRequestTimeoutAfterFirstEvent(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_123\",\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"\"}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider:   ProviderOpenAICompatible,
		BaseURL:    server.URL,
		Model:      "custom-model",
		Timeout:    10 * time.Millisecond,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		if event.Type == StreamEventMessageDelta {
			text.WriteString(event.Text)
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if attempts != 1 {
		t.Fatalf("expected no retry after first event, got %d attempts", attempts)
	}
	if text.String() != "partial" {
		t.Fatalf("unexpected streamed text: %q", text.String())
	}
}

func TestNewClientOpenAIGenerateStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_oa","choices":[{"delta":{"content":"open"},"finish_reason":""}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"ai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "token",
		BaseURL:  server.URL,
		Model:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	var done *ModelResponse
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventMessageDelta:
			text.WriteString(event.Text)
		case StreamEventDone:
			done, _ = event.Raw.(*ModelResponse)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if text.String() != "openai" {
		t.Fatalf("unexpected streamed text: %q", text.String())
	}
	if done == nil || done.Provider != ProviderOpenAI || done.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected done response: %+v", done)
	}
}

func TestNewClientQwenGenerateStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_qwen","choices":[{"delta":{"content":"qw"},"finish_reason":""}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"en"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: ProviderQwen,
		APIKey:   "token",
		BaseURL:  server.URL,
		Model:    "qwen-plus",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var gotText string
	var done *ModelResponse
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventMessageDelta:
			gotText += event.Text
		case StreamEventDone:
			done, _ = event.Raw.(*ModelResponse)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if gotText != "qwen" {
		t.Fatalf("unexpected streamed text: %q", gotText)
	}
	if done == nil || done.Provider != ProviderQwen || done.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected done response: %+v", done)
	}
}

func TestNewClientDeepSeekGenerateStreamChat(t *testing.T) {
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

	client, err := NewClient(Config{
		Provider: ProviderDeepSeek,
		APIKey:   "token",
		BaseURL:  server.URL,
		Model:    "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var gotText string
	var done *ModelResponse
	err = client.GenerateStream(context.Background(), ModelRequest{
		Input: []InputItem{UserMessageItem{Content: []TextPart{{Text: "hello"}}}},
	}, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventMessageDelta:
			gotText += event.Text
		case StreamEventDone:
			done, _ = event.Raw.(*ModelResponse)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if gotText != "deepseek" {
		t.Fatalf("unexpected streamed text: %q", gotText)
	}
	if done == nil || done.Provider != ProviderDeepSeek || done.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected done response: %+v", done)
	}
}

func TestEngineRunKeepsClientExtraBodyAcrossToolCalls(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		requests++
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["custom_flag"] != "kept" {
			t.Fatalf("request %d missing custom_flag: %#v", requests, body["custom_flag"])
		}
		if body["temperature"] != 0.9 {
			t.Fatalf("request %d missing overridden temperature: %#v", requests, body["temperature"])
		}
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": "pre-tool",
							"tool_calls": []map[string]any{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]any{
										"name":      "submit",
										"arguments": `{"value":"ok"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			})
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

	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "custom-model",
		ExtraBody: map[string]any{
			"custom_flag": "kept",
			"temperature": 0.9,
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	submitTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}
	engine := NewEngine(client, WithTools(submitTool))

	result, err := engine.Run(context.Background(), RunRequest{Input: "submit ok"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected two model requests, got %d", requests)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
	if result.ToolCallCount != 1 {
		t.Fatalf("unexpected tool call count: %d", result.ToolCallCount)
	}
}

func TestRunStreamDeepSeekThinkingCarriesReasoningContentAcrossToolCall(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		requests++
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["reasoning_effort"] != "high" {
			t.Fatalf("request %d missing reasoning_effort: %#v", requests, body["reasoning_effort"])
		}
		thinking, ok := body["thinking"].(map[string]any)
		if !ok || thinking["type"] != "enabled" {
			t.Fatalf("request %d missing thinking body: %#v", requests, body["thinking"])
		}
		if requests == 2 {
			messages, ok := body["messages"].([]any)
			if !ok {
				t.Fatalf("unexpected messages payload: %#v", body["messages"])
			}
			found := false
			for _, raw := range messages {
				msg, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if msg["role"] != "assistant" {
					continue
				}
				if msg["reasoning_content"] == "step-1step-2" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("second request missing reasoning_content in assistant messages: %#v", messages)
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"id":"chatcmpl_1","choices":[{"delta":{"reasoning_content":"step-1","tool_calls":[{"index":0,"id":"call_1","function":{"name":"submit","arguments":"{\"value\":\""}}]}}]}`,
				``,
				`data: {"choices":[{"delta":{"reasoning_content":"step-2","tool_calls":[{"index":0,"function":{"arguments":"ok\"}"}}],"content":""},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")))
			return
		}
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_2","choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	enableThinking := false
	client, err := NewClient(Config{
		Provider:       ProviderDeepSeek,
		APIKey:         "token",
		BaseURL:        server.URL,
		Model:          "deepseek-v4-flash",
		Transport:      TransportChat,
		EnableThinking: &enableThinking,
		ExtraBody: map[string]any{
			"thinking":         map[string]any{"type": "enabled"},
			"reasoning_effort": "high",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	submitTool, err := NewTool[submitArgs](submitTool{})
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}
	engine := NewEngine(client, WithTools(submitTool))

	result, err := engine.RunStream(context.Background(), RunRequest{Input: "submit ok"}, nil)
	if err != nil {
		t.Fatalf("RunStream returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected two model requests, got %d", requests)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
}

func TestNewClientBuildsDeepSeekClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider: ProviderDeepSeek,
		APIKey:   "token",
		Model:    "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
}

func TestNewClientBuildsResponsesClientWhenRequested(t *testing.T) {
	client, err := NewClient(Config{
		Provider:  ProviderOpenAI,
		APIKey:    "token",
		Model:     "gpt-4.1-mini",
		Transport: "responses",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
}

func TestNewClientRejectsUnsupportedProvider(t *testing.T) {
	_, err := NewClient(Config{
		Provider: "anthropic",
		APIKey:   "token",
		Model:    "claude-sonnet",
	})
	if err == nil {
		t.Fatalf("expected unsupported provider error")
	}
}

func TestNewClientRejectsOpenAICompatibleWithoutBaseURL(t *testing.T) {
	_, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		Model:    "custom-model",
	})
	if err == nil {
		t.Fatalf("expected missing base url error")
	}
}

func TestNewClientRejectsUnsupportedTransport(t *testing.T) {
	_, err := NewClient(Config{
		Provider:  ProviderOpenAI,
		APIKey:    "token",
		Model:     "gpt-4.1-mini",
		Transport: "stream",
	})
	if err == nil {
		t.Fatalf("expected unsupported transport error")
	}
}

func TestNewClientRejectsMissingAPIKey(t *testing.T) {
	_, err := NewClient(Config{
		Provider: ProviderOpenAI,
		Model:    "gpt-4.1-mini",
	})
	if err == nil {
		t.Fatalf("expected missing API key error")
	}
}

func TestNewClientRejectsMissingModel(t *testing.T) {
	_, err := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "token",
	})
	if err == nil {
		t.Fatalf("expected missing model error")
	}
}

func TestNewClientRejectsUnknownQwenOption(t *testing.T) {
	_, err := NewClient(Config{
		Provider: ProviderQwen,
		APIKey:   "token",
		Model:    "qwen-plus",
		Options: map[string]any{
			"unknown": true,
		},
	})
	if err == nil {
		t.Fatalf("expected unknown option error")
	}
}

func TestNewClientRejectsWrongOptionType(t *testing.T) {
	_, err := NewClient(Config{
		Provider: ProviderQwen,
		APIKey:   "token",
		Model:    "qwen-plus",
		Options: map[string]any{
			"thinking": "false",
		},
	})
	if err == nil {
		t.Fatalf("expected option type error")
	}
}
