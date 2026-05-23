package deepseek

import (
	"net/http"
	"time"

	"github.com/kiry163/easyllm/internal/model"
	"github.com/kiry163/easyllm/internal/openai/compat"
)

const DefaultBaseURL = "https://api.deepseek.com"

type RetryConfig = compat.RetryConfig

type Provider struct {
	apiKey    string
	baseURL   string
	extraBody map[string]any
	options   []compat.Option
}

type ProviderOption func(*Provider)

type ChatClientConfig struct {
	Thinking    *bool
	Model       string
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
}

func WithAPIKey(apiKey string) ProviderOption {
	return func(p *Provider) { p.apiKey = apiKey }
}

func WithBaseURL(baseURL string) ProviderOption {
	return func(p *Provider) { p.baseURL = baseURL }
}

func WithRetry(config RetryConfig) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithRetry(config)) }
}

func WithHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithHTTPClient(client)) }
}

func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithTimeout(timeout)) }
}

func WithExtraBody(extra map[string]any) ProviderOption {
	return func(p *Provider) { p.extraBody = cloneMap(extra) }
}

func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{
		baseURL: DefaultBaseURL,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *Provider) ChatClient(config ChatClientConfig) model.Client {
	extraBody := deepSeekExtraBody(config.Thinking, p.extraBody)
	opts := []compat.Option{
		compat.WithProviderName("deepseek"),
		compat.WithDefaultModel(config.Model),
		compat.WithExtraBody(extraBody),
	}
	opts = append(opts, p.options...)
	if config.Temperature != nil {
		opts = append(opts, compat.WithTemperature(*config.Temperature))
	}
	if config.TopP != nil {
		opts = append(opts, compat.WithTopP(*config.TopP))
	}
	if config.MaxTokens != nil {
		opts = append(opts, compat.WithMaxTokens(*config.MaxTokens))
	}
	return compat.NewChatClient(p.apiKey, p.baseURL, opts...)
}

func deepSeekExtraBody(thinking *bool, overrides map[string]any) map[string]any {
	extraBody := map[string]any{
		"thinking":         map[string]any{"type": "enabled"},
		"reasoning_effort": "high",
	}
	if thinking != nil && !*thinking {
		extraBody["thinking"] = map[string]any{"type": "disabled"}
		delete(extraBody, "reasoning_effort")
	}
	for key, value := range overrides {
		extraBody[key] = value
	}
	if thinkingValue, ok := extraBody["thinking"].(map[string]any); ok {
		if thinkingType, ok := thinkingValue["type"].(string); ok && thinkingType == "disabled" {
			if _, overridden := overrides["reasoning_effort"]; !overridden {
				delete(extraBody, "reasoning_effort")
			}
		}
	}
	return extraBody
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
