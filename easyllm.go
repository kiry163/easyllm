package easyllm

import (
	"context"
	"fmt"
	"strings"
	"time"

	provider "github.com/kiry163/easyllm/internal/model"
	"github.com/kiry163/easyllm/internal/provider/deepseek"
	"github.com/kiry163/easyllm/internal/provider/qwen"
	"github.com/kiry163/easyllm/provider/openai"
)

const (
	ProviderOpenAI   = "openai"
	ProviderQwen     = "qwen"
	ProviderDeepSeek = "deepseek"

	TransportChat      = "chat"
	TransportResponses = "responses"
)

type Client interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
}

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

type Config struct {
	Provider    string
	APIKey      string
	BaseURL     string
	Model       string
	Transport   string
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
	Timeout     time.Duration
	MaxAttempts int
	Options     map[string]any
}

func NewClient(config Config) (Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	switch config.Provider {
	case ProviderOpenAI:
		return newOpenAIClient(config), nil
	case ProviderQwen:
		return newQwenClient(config)
	case ProviderDeepSeek:
		return newDeepSeekClient(config)
	default:
		return nil, fmt.Errorf("unsupported provider %q", config.Provider)
	}
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return fmt.Errorf("api key is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return fmt.Errorf("model is required")
	}
	switch transportOrDefault(config.Transport) {
	case TransportChat, TransportResponses:
	default:
		return fmt.Errorf("unsupported transport %q", config.Transport)
	}
	return nil
}

func newOpenAIClient(config Config) Client {
	opts := []openai.ProviderOption{openai.WithAPIKey(config.APIKey)}
	if config.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(config.BaseURL))
	}
	if config.Timeout > 0 {
		opts = append(opts, openai.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, openai.WithRetry(openai.RetryConfig{
			MaxAttempts: config.MaxAttempts,
		}))
	}
	p := openai.NewProvider(opts...)
	clientConfig := openai.ChatClientConfig{
		Model:       config.Model,
		Temperature: config.Temperature,
		TopP:        config.TopP,
		MaxTokens:   config.MaxTokens,
	}
	if transportOrDefault(config.Transport) == TransportResponses {
		return p.ResponsesClient(openai.ResponsesClientConfig(clientConfig))
	}
	return p.ChatClient(clientConfig)
}

func newQwenClient(config Config) (Client, error) {
	thinking, err := parseQwenOptions(config.Options)
	if err != nil {
		return nil, err
	}

	opts := []qwen.ProviderOption{qwen.WithAPIKey(config.APIKey)}
	if config.BaseURL != "" {
		opts = append(opts, qwen.WithBaseURL(config.BaseURL))
	}
	if config.Timeout > 0 {
		opts = append(opts, qwen.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, qwen.WithRetry(qwen.RetryConfig{
			MaxAttempts: config.MaxAttempts,
		}))
	}
	p := qwen.NewProvider(opts...)
	clientConfig := qwen.ChatClientConfig{
		Model:       config.Model,
		Thinking:    thinking,
		Temperature: config.Temperature,
		TopP:        config.TopP,
		MaxTokens:   config.MaxTokens,
	}
	if transportOrDefault(config.Transport) == TransportResponses {
		return p.ResponsesClient(qwen.ResponsesClientConfig(clientConfig)), nil
	}
	return p.ChatClient(clientConfig), nil
}

func newDeepSeekClient(config Config) (Client, error) {
	if len(config.Options) != 0 {
		return nil, fmt.Errorf("deepseek does not support provider-specific options")
	}
	if transportOrDefault(config.Transport) == TransportResponses {
		return nil, fmt.Errorf("provider %q does not support transport %q", config.Provider, TransportResponses)
	}

	opts := []deepseek.ProviderOption{deepseek.WithAPIKey(config.APIKey)}
	if config.BaseURL != "" {
		opts = append(opts, deepseek.WithBaseURL(config.BaseURL))
	}
	if config.Timeout > 0 {
		opts = append(opts, deepseek.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, deepseek.WithRetry(deepseek.RetryConfig{
			MaxAttempts: config.MaxAttempts,
		}))
	}
	p := deepseek.NewProvider(opts...)
	clientConfig := deepseek.ChatClientConfig{
		Model:       config.Model,
		Temperature: config.Temperature,
		TopP:        config.TopP,
		MaxTokens:   config.MaxTokens,
	}
	return p.ChatClient(clientConfig), nil
}

func parseQwenOptions(options map[string]any) (*bool, error) {
	if len(options) == 0 {
		return nil, nil
	}

	var thinking *bool
	for key, value := range options {
		switch key {
		case "thinking":
			parsed, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("qwen option %q must be bool", key)
			}
			thinking = &parsed
		default:
			return nil, fmt.Errorf("unsupported qwen option %q", key)
		}
	}
	return thinking, nil
}

func transportOrDefault(value string) string {
	if strings.TrimSpace(value) == "" {
		return TransportChat
	}
	return value
}
