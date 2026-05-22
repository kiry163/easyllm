package easyllm

import (
	"testing"
)

func TestNewClientBuildsOpenAIClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider: "openai",
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

func TestNewClientBuildsQwenClientWithThinkingOption(t *testing.T) {
	client, err := NewClient(Config{
		Provider: "qwen",
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

func TestNewClientBuildsDeepSeekClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider: "deepseek",
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
		Provider:  "openai",
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

func TestNewClientRejectsUnsupportedTransport(t *testing.T) {
	_, err := NewClient(Config{
		Provider:  "openai",
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
		Provider: "openai",
		Model:    "gpt-4.1-mini",
	})
	if err == nil {
		t.Fatalf("expected missing API key error")
	}
}

func TestNewClientRejectsMissingModel(t *testing.T) {
	_, err := NewClient(Config{
		Provider: "openai",
		APIKey:   "token",
	})
	if err == nil {
		t.Fatalf("expected missing model error")
	}
}

func TestNewClientRejectsUnknownQwenOption(t *testing.T) {
	_, err := NewClient(Config{
		Provider: "qwen",
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
		Provider: "qwen",
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
