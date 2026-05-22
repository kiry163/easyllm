package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kiry163/easyllm"
	"github.com/kiry163/easyllm/tool"
)

func TestParseConfigLoadsQwenValuesFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(`
QWEN_API_KEY=token
QWEN_BASE_URL=https://example.test/v1
QWEN_MODEL=qwen-plus
EASYLLM_TRANSPORT=responses
EASYLLM_PROMPT=今天北京天气怎么样？
QWEN_ENABLE_THINKING=false
QWEN_TEMPERATURE=0.6
QWEN_TOP_P=0.7
QWEN_MAX_TOKENS=512
`), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := parseConfig([]string{"-env", envPath}, func(key string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Transport != "responses" {
		t.Fatalf("unexpected transport: %q", cfg.Transport)
	}
	if cfg.BaseURL != "https://example.test/v1" {
		t.Fatalf("unexpected base url: %q", cfg.BaseURL)
	}
	if cfg.APIKey != "token" || cfg.Model != "qwen-plus" {
		t.Fatalf("unexpected qwen config: %+v", cfg)
	}
	if cfg.Prompt != "今天北京天气怎么样？" {
		t.Fatalf("unexpected prompt: %q", cfg.Prompt)
	}
	if cfg.Thinking {
		t.Fatalf("expected thinking to be disabled")
	}
	if cfg.Temperature != 0.6 || cfg.TopP != 0.7 || cfg.MaxTokens != 512 {
		t.Fatalf("unexpected sampling config: %+v", cfg)
	}
}

func TestParseConfigUsesDefaultsForWeatherAcceptance(t *testing.T) {
	cfg, err := parseConfig(nil, func(key string) string {
		switch key {
		case "QWEN_API_KEY":
			return "token"
		case "QWEN_MODEL":
			return "qwen-plus"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Transport != "chat" {
		t.Fatalf("unexpected default transport: %q", cfg.Transport)
	}
	if cfg.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("unexpected default base url: %q", cfg.BaseURL)
	}
	if cfg.Prompt != "请查询今天北京的天气。" {
		t.Fatalf("unexpected default prompt: %q", cfg.Prompt)
	}
}

func TestParseConfigRejectsUnsupportedTransport(t *testing.T) {
	_, err := parseConfig([]string{"-transport", "stream"}, func(key string) string {
		switch key {
		case "QWEN_API_KEY":
			return "token"
		case "QWEN_MODEL":
			return "qwen-plus"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatalf("expected error for unsupported transport")
	}
}

func TestWeatherToolReturnsFixedBeijingWeather(t *testing.T) {
	runTool := weatherTool()
	result, err := runTool.Invoke(nil, tool.CallContext{Name: "get_weather"}, map[string]any{
		"city": "北京",
	})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result.Message != "北京今天晴，气温 25C，微风。" {
		t.Fatalf("unexpected weather message: %q", result.Message)
	}
	if result.Data["city"] != "北京" {
		t.Fatalf("unexpected city: %#v", result.Data["city"])
	}
}

func TestWeatherToolDefinitionUsesCityOnly(t *testing.T) {
	def := weatherTool().Definition()
	if def.Name != "get_weather" {
		t.Fatalf("unexpected tool name: %q", def.Name)
	}
	properties, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected properties: %#v", def.Parameters["properties"])
	}
	if len(properties) != 1 {
		t.Fatalf("expected only city property, got %#v", properties)
	}
	city, ok := properties["city"].(map[string]any)
	if !ok || city["type"] != "string" {
		t.Fatalf("unexpected city schema: %#v", properties["city"])
	}
}

func TestBuildClientSupportsChatAndResponses(t *testing.T) {
	base := config{
		APIKey:  "token",
		BaseURL: "https://example.test/v1",
		Model:   "qwen-plus",
	}
	for _, transport := range []string{"chat", "responses"} {
		cfg := base
		cfg.Transport = transport
		client, err := buildClient(cfg)
		if err != nil {
			t.Fatalf("buildClient(%s) returned error: %v", transport, err)
		}
		if client == nil {
			t.Fatalf("buildClient(%s) returned nil client", transport)
		}
		if _, ok := any(client).(easyllm.Client); !ok {
			t.Fatalf("buildClient(%s) returned non-model client: %T", transport, client)
		}
	}
}

func TestBuildClientSupportsThinkingOption(t *testing.T) {
	client, err := buildClient(config{
		Transport: "chat",
		APIKey:    "token",
		BaseURL:   "https://example.test/v1",
		Model:     "qwen-plus",
		Thinking:  true,
	})
	if err != nil {
		t.Fatalf("buildClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
}
