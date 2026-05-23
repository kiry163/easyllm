package openai

import (
	"context"
	"net/http"
	"time"

	provider "github.com/kiry163/easyllm/internal/model"
	"github.com/kiry163/easyllm/internal/openai/compat"
)

type ModelRequest = provider.ModelRequest
type ModelResponse = provider.ModelResponse
type InputItem = provider.InputItem
type OutputItem = provider.OutputItem
type Usage = provider.Usage
type TextPart = provider.TextPart
type SystemMessageItem = provider.SystemMessageItem
type UserMessageItem = provider.UserMessageItem
type AssistantMessageItem = provider.AssistantMessageItem
type ToolCallItem = provider.ToolCallItem
type ToolResultItem = provider.ToolResultItem
type MessageOutput = provider.MessageOutput
type ToolCallOutput = provider.ToolCallOutput
type ToolDefinition = provider.ToolDefinition

type RetryConfig = compat.RetryConfig

type Client struct {
	inner *compat.Client
}

type Provider struct {
	apiKey  string
	baseURL string
	options []compat.Option
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
	return func(p *Provider) { p.options = append(p.options, compat.WithRetry(config)) }
}

func WithHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithHTTPClient(client)) }
}

func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *Provider) { p.options = append(p.options, compat.WithTimeout(timeout)) }
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

func (p *Provider) modelOptions(config ChatClientConfig) []compat.Option {
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
