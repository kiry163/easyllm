package openai

import (
	"context"
	"net/http"
	"time"

	"github.com/kiry163/easyllm/internal/model"
	"github.com/kiry163/easyllm/internal/openai/compat"
)

type ModelRequest = model.ModelRequest
type ModelResponse = model.ModelResponse
type InputItem = model.InputItem
type OutputItem = model.OutputItem
type Usage = model.Usage
type TextPart = model.TextPart
type SystemMessageItem = model.SystemMessageItem
type UserMessageItem = model.UserMessageItem
type AssistantMessageItem = model.AssistantMessageItem
type ToolCallItem = model.ToolCallItem
type ToolResultItem = model.ToolResultItem
type MessageOutput = model.MessageOutput
type ToolCallOutput = model.ToolCallOutput
type ToolDefinition = model.ToolDefinition

type RetryConfig = compat.RetryConfig

const DefaultBaseURL = "https://api.openai.com/v1"

type Client struct {
	inner *compat.Client
}

type Provider struct {
	apiKey                  string
	baseURL                 string
	httpClient              *http.Client
	streamFirstEventTimeout time.Duration
	retry                   compat.RetryConfig
	extraBody               map[string]any
	defaultOptions          map[string]any
}

type ProviderOption func(*Provider)

type ChatClientConfig struct {
	Model       string
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
	}
}

func WithHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) {
		if timeout > 0 {
			p.httpClient.Timeout = timeout
		}
	}
}

func WithStreamFirstEventTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) {
		if timeout > 0 {
			p.streamFirstEventTimeout = timeout
		}
	}
}

func WithExtraBody(extra map[string]any) ProviderOption {
	return func(p *Provider) {
		p.extraBody = cloneMap(extra)
	}
}

func WithDefaultOptions(options map[string]any) ProviderOption {
	return func(p *Provider) {
		p.defaultOptions = cloneMap(options)
	}
}

func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{
		baseURL: DefaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retry: compat.RetryConfig{
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     time.Second,
		},
		extraBody:      map[string]any{},
		defaultOptions: map[string]any{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *Provider) ChatClient(config ChatClientConfig) *Client {
	opts := p.modelOptions(config)
	return &Client{inner: compat.NewChatClient(p.apiKey, p.baseURL, opts...)}
}

func (p *Provider) ResponsesClient(config ResponsesClientConfig) *Client {
	opts := p.modelOptions(ChatClientConfig(config))
	return &Client{inner: compat.NewResponsesClient(p.apiKey, p.baseURL, opts...)}
}

func (c *Client) Generate(ctx context.Context, req ModelRequest) (*ModelResponse, error) {
	return c.inner.Generate(ctx, req)
}

func (c *Client) GenerateStream(ctx context.Context, req ModelRequest, handler model.StreamHandler) error {
	return c.inner.GenerateStream(ctx, req, handler)
}

func (p *Provider) modelOptions(config ChatClientConfig) []compat.Option {
	opts := []compat.Option{
		compat.WithProviderName("openai"),
		compat.WithDefaultModel(config.Model),
		compat.WithHTTPClient(p.httpClient),
		compat.WithRetry(p.retry),
		compat.WithExtraBody(p.extraBody),
		compat.WithDefaultOptions(p.defaultOptions),
		compat.WithStreamFirstEventTimeout(p.streamFirstEventTimeout),
	}
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

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
