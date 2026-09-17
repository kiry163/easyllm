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
	Thinking          *bool
	Model             string
	Temperature       *float64
	TopP              *float64
	MaxTokens         *int
	ParallelToolCalls *bool
}

type ResponsesClientConfig = ChatClientConfig

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

func WithHTTPDoer(doer model.HTTPDoer) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithHTTPDoer(doer)) }
}

func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithTimeout(timeout)) }
}

func WithStreamFirstEventTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithStreamFirstEventTimeout(timeout)) }
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
	return p.model(config, compat.TransportChat)
}

func (p *Provider) ResponsesClient(config ResponsesClientConfig) model.Client {
	return p.model(ChatClientConfig(config), compat.TransportResponses)
}

func (p *Provider) model(config ChatClientConfig, transport compat.Transport) model.Client {
	extraBody := deepSeekExtraBody(config.Thinking, p.extraBody)
	if transport == compat.TransportResponses {
		extraBody = deepSeekResponsesExtraBody(config.Thinking, p.extraBody)
	}
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
	if config.ParallelToolCalls != nil {
		opts = append(opts, compat.WithParallelToolCalls(*config.ParallelToolCalls))
	}
	if transport == compat.TransportResponses {
		return compat.NewResponsesClient(p.apiKey, p.baseURL, opts...)
	}
	return compat.NewChatClient(p.apiKey, p.baseURL, opts...)
}

// deepSeekResponsesExtraBody maps the provider-neutral thinking switch onto the
// responses transport, which configures reasoning through reasoning.effort
// instead of the chat transport's thinking / reasoning_effort pair. A nil
// switch keeps the chat transport's default of thinking enabled at high effort.
func deepSeekResponsesExtraBody(thinking *bool, overrides map[string]any) map[string]any {
	effort := "high"
	if thinking != nil && !*thinking {
		effort = "none"
	}
	extraBody := map[string]any{
		"reasoning": map[string]any{"effort": effort},
	}
	for key, value := range overrides {
		extraBody[key] = value
	}
	return extraBody
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
