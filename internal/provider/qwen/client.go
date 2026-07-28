package qwen

import (
	"net/http"
	"time"

	"github.com/kiry163/easyllm/internal/model"
	"github.com/kiry163/easyllm/internal/openai/compat"
)

const DefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

type RetryConfig = compat.RetryConfig

type Provider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	httpDoer   model.HTTPDoer
	retry      compat.RetryConfig
	extraBody  map[string]any
	options    []compat.Option
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
	return func(p *Provider) {
		if config.MaxRetries > 0 {
			p.retry.MaxRetries = config.MaxRetries
		}
		if config.InitialBackoff > 0 {
			p.retry.InitialBackoff = config.InitialBackoff
		}
		if config.MaxBackoff > 0 {
			p.retry.MaxBackoff = config.MaxBackoff
		}
		p.options = append(p.options, compat.WithRetry(config))
	}
}

func WithHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
		p.options = append(p.options, compat.WithHTTPClient(client))
	}
}

func WithHTTPDoer(doer model.HTTPDoer) ProviderOption {
	return func(p *Provider) {
		if doer != nil {
			p.httpDoer = doer
		}
		p.options = append(p.options, compat.WithHTTPDoer(doer))
	}
}

func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) {
		if timeout > 0 {
			p.httpClient.Timeout = timeout
		}
		p.options = append(p.options, compat.WithTimeout(timeout))
	}
}

func WithStreamFirstEventTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithStreamFirstEventTimeout(timeout)) }
}

func WithExtraBody(extra map[string]any) ProviderOption {
	return func(p *Provider) { p.extraBody = cloneMap(extra) }
}

func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		retry: compat.RetryConfig{
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     time.Second,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *Provider) resolvedHTTPDoer() model.HTTPDoer {
	if p.httpDoer != nil {
		return p.httpDoer
	}
	return p.httpClient
}

func (p *Provider) ChatClient(config ChatClientConfig) model.Client {
	return p.model(config, compat.TransportChat)
}

func (p *Provider) ResponsesClient(config ResponsesClientConfig) model.Client {
	return p.model(ChatClientConfig(config), compat.TransportResponses)
}

func (p *Provider) model(config ChatClientConfig, transport compat.Transport) model.Client {
	extraBody := map[string]any{}
	if config.Thinking != nil {
		extraBody["enable_thinking"] = *config.Thinking
	}
	for key, value := range p.extraBody {
		extraBody[key] = value
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
