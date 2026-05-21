package qwen

import (
	baseprovider "github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/provider/openai/compat"
)

const DefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

type Provider struct {
	apiKey  string
	baseURL string
}

type ProviderOption func(*Provider)

type ChatModelConfig struct {
	Model       string
	Thinking    *bool
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

func (p *Provider) ChatModel(config ChatModelConfig) baseprovider.ModelClient {
	return p.model(config, compat.TransportChat)
}

func (p *Provider) ResponsesModel(config ResponsesModelConfig) baseprovider.ModelClient {
	return p.model(ChatModelConfig(config), compat.TransportResponses)
}

func (p *Provider) model(config ChatModelConfig, transport compat.Transport) baseprovider.ModelClient {
	extraBody := map[string]any{}
	if config.Thinking != nil {
		extraBody["enable_thinking"] = *config.Thinking
	}
	compatOpts := []compat.Option{
		compat.WithProviderName("qwen"),
		compat.WithDefaultModel(config.Model),
		compat.WithExtraBody(extraBody),
	}
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
