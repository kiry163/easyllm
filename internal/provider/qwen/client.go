package qwen

import (
	"net/http"
	"time"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/provider/openai/compat"
)

const DefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

type RetryConfig = compat.RetryConfig

type Provider struct {
	apiKey  string
	baseURL string
	options []compat.Option
}

type ProviderOption func(*Provider)

type ChatClientConfig struct {
	Model       string
	Thinking    *bool
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
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

func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithTimeout(timeout)) }
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

func (p *Provider) ChatClient(config ChatClientConfig) provider.Client {
	return p.model(config, compat.TransportChat)
}

func (p *Provider) ResponsesClient(config ResponsesClientConfig) provider.Client {
	return p.model(ChatClientConfig(config), compat.TransportResponses)
}

func (p *Provider) model(config ChatClientConfig, transport compat.Transport) provider.Client {
	extraBody := map[string]any{}
	if config.Thinking != nil {
		extraBody["enable_thinking"] = *config.Thinking
	}
	compatOpts := []compat.Option{
		compat.WithProviderName("qwen"),
		compat.WithDefaultModel(config.Model),
		compat.WithExtraBody(extraBody),
	}
	compatOpts = append(compatOpts, p.options...)
	if config.Temperature != nil {
		compatOpts = append(compatOpts, compat.WithTemperature(*config.Temperature))
	}
	if config.TopP != nil {
		compatOpts = append(compatOpts, compat.WithTopP(*config.TopP))
	}
	if config.MaxTokens != nil {
		compatOpts = append(compatOpts, compat.WithMaxTokens(*config.MaxTokens))
	}
	if transport == compat.TransportResponses {
		return compat.NewResponsesClient(p.apiKey, p.baseURL, compatOpts...)
	}
	return compat.NewChatClient(p.apiKey, p.baseURL, compatOpts...)
}
