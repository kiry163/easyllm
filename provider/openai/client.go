package openai

import (
	"net/http"

	baseprovider "github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/provider/openai/compat"
)

type Client = compat.Client
type RetryConfig = compat.RetryConfig

type Provider struct {
	apiKey  string
	baseURL string
	options []compat.Option
}

type ProviderOption func(*Provider)

type ChatModelConfig struct {
	Model       string
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
}

type ResponsesModelConfig = ChatModelConfig

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

func WithExtraBody(extra map[string]any) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithExtraBody(extra)) }
}

func WithDefaultOptions(options map[string]any) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithDefaultOptions(options)) }
}

func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *Provider) ChatModel(config ChatModelConfig) baseprovider.ModelClient {
	opts := p.modelOptions(config)
	return compat.NewChatClient(p.apiKey, p.baseURL, opts...)
}

func (p *Provider) ResponsesModel(config ResponsesModelConfig) baseprovider.ModelClient {
	opts := p.modelOptions(ChatModelConfig(config))
	return compat.NewResponsesClient(p.apiKey, p.baseURL, opts...)
}

func (p *Provider) modelOptions(config ChatModelConfig) []compat.Option {
	opts := []compat.Option{compat.WithProviderName("openai"), compat.WithDefaultModel(config.Model)}
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
	return opts
}
