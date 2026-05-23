package easyllm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kiry163/easyllm/internal/model"
	"github.com/kiry163/easyllm/internal/openai/compat"
	"github.com/kiry163/easyllm/internal/provider/deepseek"
	"github.com/kiry163/easyllm/internal/provider/openai"
	"github.com/kiry163/easyllm/internal/provider/qwen"
)

const (
	ProviderOpenAI           = "openai"
	ProviderOpenAICompatible = "openai_compatible"
	ProviderQwen             = "qwen"
	ProviderDeepSeek         = "deepseek"

	TransportChat      = "chat"
	TransportResponses = "responses"
)

type Client interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
	GenerateStream(context.Context, ModelRequest, StreamHandler) error
}

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
type StreamEventType = model.StreamEventType
type StreamEvent = model.StreamEvent
type StreamHandler = model.StreamHandler

const (
	StreamEventMessageDelta = model.StreamEventMessageDelta
	StreamEventToolStart    = model.StreamEventToolStart
	StreamEventToolFinish   = model.StreamEventToolFinish
	StreamEventDone         = model.StreamEventDone
)

type Config struct {
	Provider    string
	APIKey      string
	BaseURL     string
	Model       string
	Transport   string
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
	// EnableThinking is a provider-neutral reasoning switch. Providers map it to
	// their request-body field when supported.
	EnableThinking *bool
	Timeout        time.Duration
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// ExtraBody is merged into the final model request body after normalized
	// fields, so it can override provider-specific request parameters.
	ExtraBody map[string]any
	// Options is kept for compatibility. Prefer ExtraBody for new code.
	Options map[string]any
}

func NewClient(config Config) (Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	switch config.Provider {
	case ProviderOpenAI:
		return newOpenAIClient(config), nil
	case ProviderOpenAICompatible:
		return newOpenAICompatibleClient(config), nil
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
	if strings.TrimSpace(config.APIKey) == "" && config.Provider != ProviderOpenAICompatible {
		return fmt.Errorf("api key is required")
	}
	if config.Provider == ProviderOpenAICompatible && strings.TrimSpace(config.BaseURL) == "" {
		return fmt.Errorf("base url is required for provider %q", ProviderOpenAICompatible)
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
	opts := []openai.ProviderOption{
		openai.WithAPIKey(config.APIKey),
		openai.WithExtraBody(configExtraBody(config)),
	}
	if config.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(config.BaseURL))
	}
	if config.Timeout > 0 {
		opts = append(opts, openai.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, openai.WithRetry(openai.RetryConfig{
			MaxAttempts:    config.MaxAttempts,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
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

func newOpenAICompatibleClient(config Config) Client {
	opts := []compat.Option{
		compat.WithProviderName(ProviderOpenAICompatible),
		compat.WithDefaultModel(config.Model),
		compat.WithExtraBody(configExtraBody(config)),
	}
	if config.Timeout > 0 {
		opts = append(opts, compat.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, compat.WithRetry(compat.RetryConfig{
			MaxAttempts:    config.MaxAttempts,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
		}))
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
	if transportOrDefault(config.Transport) == TransportResponses {
		return compat.NewResponsesClient(config.APIKey, config.BaseURL, opts...)
	}
	return compat.NewChatClient(config.APIKey, config.BaseURL, opts...)
}

func newQwenClient(config Config) (Client, error) {
	thinking, err := configEnableThinking(config)
	if err != nil {
		return nil, err
	}

	opts := []qwen.ProviderOption{
		qwen.WithAPIKey(config.APIKey),
		qwen.WithExtraBody(configExtraBody(config)),
	}
	if config.BaseURL != "" {
		opts = append(opts, qwen.WithBaseURL(config.BaseURL))
	}
	if config.Timeout > 0 {
		opts = append(opts, qwen.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, qwen.WithRetry(qwen.RetryConfig{
			MaxAttempts:    config.MaxAttempts,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
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
	if transportOrDefault(config.Transport) == TransportResponses {
		return nil, fmt.Errorf("provider %q does not support transport %q", config.Provider, TransportResponses)
	}

	opts := []deepseek.ProviderOption{
		deepseek.WithAPIKey(config.APIKey),
		deepseek.WithExtraBody(configExtraBody(config)),
	}
	if config.BaseURL != "" {
		opts = append(opts, deepseek.WithBaseURL(config.BaseURL))
	}
	if config.Timeout > 0 {
		opts = append(opts, deepseek.WithTimeout(config.Timeout))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, deepseek.WithRetry(deepseek.RetryConfig{
			MaxAttempts:    config.MaxAttempts,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
		}))
	}
	p := deepseek.NewProvider(opts...)
	clientConfig := deepseek.ChatClientConfig{
		Thinking:    config.EnableThinking,
		Model:       config.Model,
		Temperature: config.Temperature,
		TopP:        config.TopP,
		MaxTokens:   config.MaxTokens,
	}
	return p.ChatClient(clientConfig), nil
}

func configEnableThinking(config Config) (*bool, error) {
	thinking := config.EnableThinking
	if len(config.Options) == 0 {
		return thinking, nil
	}
	for key, value := range config.Options {
		switch key {
		case "thinking":
			parsed, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("qwen option %q must be bool", key)
			}
			thinking = &parsed
		case "extra_body":
			continue
		default:
			return nil, fmt.Errorf("unsupported qwen option %q", key)
		}
	}
	return thinking, nil
}

func configExtraBody(config Config) map[string]any {
	merged := cloneMap(config.ExtraBody)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range config.Options {
		if key == "extra_body" {
			extra, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for extraKey, extraValue := range extra {
				merged[extraKey] = extraValue
			}
			continue
		}
		if key == "thinking" {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func transportOrDefault(value string) string {
	if strings.TrimSpace(value) == "" {
		return TransportChat
	}
	return value
}
