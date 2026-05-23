package easyllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
