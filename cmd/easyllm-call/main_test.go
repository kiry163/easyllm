package main

import (
	"testing"
)

func TestParseConfigUsesFlagsAndEnv(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-provider", "openai",
		"-transport", "responses",
		"-model", "gpt-4.1-mini",
		"-prompt", "hello",
		"-base-url", "https://api.openai.com/v1",
	}, func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "token"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Transport != "responses" {
		t.Fatalf("unexpected provider config: %+v", cfg)
	}
	if cfg.APIKey != "token" {
		t.Fatalf("expected api key from env")
	}
}

func TestParseConfigUsesQwenDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-provider", "qwen",
		"-transport", "chat",
		"-model", "qwen-plus",
		"-prompt", "hello",
		"-enable-thinking",
		"-temperature", "0.6",
		"-top-p", "0.7",
		"-max-tokens", "512",
	}, func(key string) string {
		if key == "DASHSCOPE_API_KEY" {
			return "dashscope-token"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("unexpected qwen base url: %q", cfg.BaseURL)
	}
	if cfg.APIKey != "dashscope-token" || !cfg.Thinking {
		t.Fatalf("unexpected qwen config: %+v", cfg)
	}
	if cfg.Temperature != 0.6 || cfg.TopP != 0.7 || cfg.MaxTokens != 512 {
		t.Fatalf("unexpected sampling config: %+v", cfg)
	}
}

func TestParseConfigRejectsMissingPrompt(t *testing.T) {
	_, err := parseConfig([]string{
		"-provider", "openai",
		"-transport", "chat",
		"-model", "gpt-4.1-mini",
		"-base-url", "https://api.openai.com/v1",
	}, func(key string) string {
		return "token"
	})
	if err == nil {
		t.Fatalf("expected error for missing prompt")
	}
}
