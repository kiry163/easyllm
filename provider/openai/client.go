package openai

import (
	"net/http"
	"strings"
	"time"
)

type transport string

const (
	transportChat      transport = "chat"
	transportResponses transport = "responses"
)

type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type Option func(*Client)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	retry      RetryConfig
	transport  transport
}

func WithRetry(config RetryConfig) Option {
	return func(c *Client) {
		if config.MaxAttempts > 0 {
			c.retry.MaxAttempts = config.MaxAttempts
		}
		if config.InitialBackoff > 0 {
			c.retry.InitialBackoff = config.InitialBackoff
		}
		if config.MaxBackoff > 0 {
			c.retry.MaxBackoff = config.MaxBackoff
		}
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func NewChatClient(apiKey, baseURL string, opts ...Option) *Client {
	return newClient(apiKey, baseURL, transportChat, opts...)
}

func NewResponsesClient(apiKey, baseURL string, opts ...Option) *Client {
	return newClient(apiKey, baseURL, transportResponses, opts...)
}

func newClient(apiKey, baseURL string, transport transport, opts ...Option) *Client {
	client := &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retry: RetryConfig{
			MaxAttempts:    1,
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     time.Second,
		},
		transport: transport,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}
