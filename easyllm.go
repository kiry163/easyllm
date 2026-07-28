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
type ModelInfo = model.ModelInfo
type ListModelsResponse = model.ListModelsResponse
type ImageClient = model.ImageClient
type ImageRequest = model.ImageRequest
type ImageResponse = model.ImageResponse
type GeneratedImage = model.GeneratedImage
type EmbeddingClient = model.EmbeddingClient
type EmbeddingRequest = model.EmbeddingRequest
type EmbeddingResponse = model.EmbeddingResponse
type Embedding = model.Embedding
type RerankClient = model.RerankClient
type RerankRequest = model.RerankRequest
type RerankResponse = model.RerankResponse
type RerankResult = model.RerankResult
type InputItem = model.InputItem
type OutputItem = model.OutputItem
type Usage = model.Usage
type ContentPart = model.ContentPart
type ContentPartType = model.ContentPartType
type ImageDetail = model.ImageDetail
type TextPart = model.TextPart
type SystemMessageItem = model.SystemMessageItem
type UserMessageItem = model.UserMessageItem
type AssistantMessageItem = model.AssistantMessageItem
type ToolCallItem = model.ToolCallItem
type ToolResultItem = model.ToolResultItem
type AssistantOutput = model.AssistantOutput
type ToolCallOutput = model.ToolCallOutput
type StreamEventType = model.StreamEventType
type StreamEvent = model.StreamEvent
type StreamHandler = model.StreamHandler

var ErrStreamFirstEventTimeout = compat.ErrStreamFirstEventTimeout

func NewTextPart(text string) ContentPart {
	return model.NewTextPart(text)
}

func NewImagePart(imageURL string, detail ImageDetail) ContentPart {
	return model.NewImagePart(imageURL, detail)
}

const (
	ContentPartTypeText  = model.ContentPartTypeText
	ContentPartTypeImage = model.ContentPartTypeImage
	ImageDetailAuto      = model.ImageDetailAuto
	ImageDetailLow       = model.ImageDetailLow
	ImageDetailHigh      = model.ImageDetailHigh
)

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
	EnableThinking          *bool
	Timeout                 time.Duration
	StreamFirstEventTimeout time.Duration
	MaxRetries              int
	InitialBackoff          time.Duration
	MaxBackoff              time.Duration
	// HTTPMiddleware wraps the HTTP executor used for provider requests.
	// Work before calling the wrapped HTTPDoer is not counted toward Timeout.
	HTTPMiddleware HTTPMiddleware
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
	doer, err := newHTTPDoer(config)
	if err != nil {
		return nil, err
	}

	switch config.Provider {
	case ProviderOpenAI:
		return newOpenAIClient(config, doer), nil
	case ProviderOpenAICompatible:
		return newOpenAICompatibleClient(config, doer), nil
	case ProviderQwen:
		return newQwenClient(config, doer)
	case ProviderDeepSeek:
		return newDeepSeekClient(config, doer)
	default:
		return nil, fmt.Errorf("unsupported provider %q", config.Provider)
	}
}

func NewImageClient(config Config) (ImageClient, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	switch config.Provider {
	case ProviderOpenAI:
		doer, err := newHTTPDoer(config)
		if err != nil {
			return nil, err
		}
		return newOpenAIImageClient(config, doer), nil
	default:
		return nil, fmt.Errorf("provider %q does not support image generation", config.Provider)
	}
}

func NewEmbeddingClient(config Config) (EmbeddingClient, error) {
	if err := validateCapabilityConfig(config); err != nil {
		return nil, err
	}
	if config.Provider != ProviderOpenAI && config.Provider != ProviderOpenAICompatible {
		return nil, fmt.Errorf("provider %q does not support embeddings", config.Provider)
	}

	doer, err := newHTTPDoer(config)
	if err != nil {
		return nil, err
	}
	return newOpenAIEmbeddingClient(config, doer), nil
}

func NewRerankClient(config Config) (RerankClient, error) {
	if err := validateCapabilityConfig(config); err != nil {
		return nil, err
	}
	if config.Provider != ProviderQwen {
		return nil, fmt.Errorf("provider %q does not support reranking", config.Provider)
	}
	if strings.TrimSpace(config.Model) != "qwen3-rerank" {
		return nil, fmt.Errorf("model %q is not supported for qwen reranking", config.Model)
	}

	doer, err := newHTTPDoer(config)
	if err != nil {
		return nil, err
	}
	return newQwenRerankClient(config, doer), nil
}

func ListModels(ctx context.Context, config Config) (*ListModelsResponse, error) {
	if err := validateListModelsConfig(config); err != nil {
		return nil, err
	}
	doer, err := newHTTPDoer(config)
	if err != nil {
		return nil, err
	}
	return newModelListClient(config, doer).ListModels(ctx)
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
	switch config.Provider {
	case ProviderOpenAI, ProviderOpenAICompatible, ProviderQwen, ProviderDeepSeek:
	default:
		return fmt.Errorf("unsupported provider %q", config.Provider)
	}
	switch transportOrDefault(config.Transport) {
	case TransportChat, TransportResponses:
	default:
		return fmt.Errorf("unsupported transport %q", config.Transport)
	}
	return nil
}

func validateListModelsConfig(config Config) error {
	if strings.TrimSpace(config.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(config.APIKey) == "" && config.Provider != ProviderOpenAICompatible {
		return fmt.Errorf("api key is required")
	}
	if config.Provider == ProviderOpenAICompatible && strings.TrimSpace(config.BaseURL) == "" {
		return fmt.Errorf("base url is required for provider %q", ProviderOpenAICompatible)
	}
	switch config.Provider {
	case ProviderOpenAI, ProviderOpenAICompatible, ProviderQwen, ProviderDeepSeek:
	default:
		return fmt.Errorf("unsupported provider %q", config.Provider)
	}
	return nil
}

func validateCapabilityConfig(config Config) error {
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
	switch config.Provider {
	case ProviderOpenAI, ProviderOpenAICompatible, ProviderQwen, ProviderDeepSeek:
		return nil
	default:
		return fmt.Errorf("unsupported provider %q", config.Provider)
	}
}

func newOpenAIClient(config Config, doer HTTPDoer) Client {
	opts := []openai.ProviderOption{
		openai.WithAPIKey(config.APIKey),
		openai.WithExtraBody(configExtraBody(config)),
		openai.WithHTTPDoer(doer),
	}
	if config.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(config.BaseURL))
	}
	if config.StreamFirstEventTimeout > 0 {
		opts = append(opts, openai.WithStreamFirstEventTimeout(config.StreamFirstEventTimeout))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, openai.WithRetry(openai.RetryConfig{
			MaxRetries:     config.MaxRetries,
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

func newModelListClient(config Config, doer HTTPDoer) *compat.Client {
	baseURL := config.BaseURL
	provider := config.Provider
	switch config.Provider {
	case ProviderOpenAI:
		if baseURL == "" {
			baseURL = openai.DefaultBaseURL
		}
	case ProviderQwen:
		if baseURL == "" {
			baseURL = qwen.DefaultBaseURL
		}
	case ProviderDeepSeek:
		if baseURL == "" {
			baseURL = deepseek.DefaultBaseURL
		}
	case ProviderOpenAICompatible:
		provider = ProviderOpenAICompatible
	}
	opts := []compat.Option{
		compat.WithProviderName(provider),
		compat.WithHTTPDoer(doer),
	}
	if config.MaxRetries > 0 {
		opts = append(opts, compat.WithRetry(compat.RetryConfig{
			MaxRetries:     config.MaxRetries,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
		}))
	}
	return compat.NewChatClient(config.APIKey, baseURL, opts...)
}

func newOpenAIImageClient(config Config, doer HTTPDoer) ImageClient {
	opts := []openai.ProviderOption{
		openai.WithAPIKey(config.APIKey),
		openai.WithExtraBody(configExtraBody(config)),
		openai.WithHTTPDoer(doer),
	}
	if config.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(config.BaseURL))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, openai.WithRetry(openai.RetryConfig{
			MaxRetries:     config.MaxRetries,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
		}))
	}
	return openai.NewProvider(opts...).ImageClient(openai.ImageClientConfig{
		Model: config.Model,
	})
}

func newOpenAIEmbeddingClient(config Config, doer HTTPDoer) EmbeddingClient {
	providerName := config.Provider
	opts := []openai.ProviderOption{
		openai.WithAPIKey(config.APIKey),
		openai.WithProviderName(providerName),
		openai.WithExtraBody(configExtraBody(config)),
		openai.WithHTTPDoer(doer),
	}
	if config.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(config.BaseURL))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, openai.WithRetry(openai.RetryConfig{
			MaxRetries:     config.MaxRetries,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
		}))
	}
	return openai.NewProvider(opts...).EmbeddingClient(openai.EmbeddingClientConfig{Model: config.Model})
}

func newOpenAICompatibleClient(config Config, doer HTTPDoer) Client {
	opts := []compat.Option{
		compat.WithProviderName(ProviderOpenAICompatible),
		compat.WithDefaultModel(config.Model),
		compat.WithExtraBody(configExtraBody(config)),
		compat.WithHTTPDoer(doer),
	}
	if config.StreamFirstEventTimeout > 0 {
		opts = append(opts, compat.WithStreamFirstEventTimeout(config.StreamFirstEventTimeout))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, compat.WithRetry(compat.RetryConfig{
			MaxRetries:     config.MaxRetries,
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

func newQwenClient(config Config, doer HTTPDoer) (Client, error) {
	thinking, err := configEnableThinking(config)
	if err != nil {
		return nil, err
	}

	opts := []qwen.ProviderOption{
		qwen.WithAPIKey(config.APIKey),
		qwen.WithExtraBody(configExtraBody(config)),
		qwen.WithHTTPDoer(doer),
	}
	if config.BaseURL != "" {
		opts = append(opts, qwen.WithBaseURL(config.BaseURL))
	}
	if config.StreamFirstEventTimeout > 0 {
		opts = append(opts, qwen.WithStreamFirstEventTimeout(config.StreamFirstEventTimeout))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, qwen.WithRetry(qwen.RetryConfig{
			MaxRetries:     config.MaxRetries,
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

func newQwenRerankClient(config Config, doer HTTPDoer) RerankClient {
	opts := []qwen.ProviderOption{
		qwen.WithAPIKey(config.APIKey),
		qwen.WithExtraBody(configExtraBody(config)),
		qwen.WithHTTPDoer(doer),
	}
	if config.BaseURL != "" {
		opts = append(opts, qwen.WithBaseURL(config.BaseURL))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, qwen.WithRetry(qwen.RetryConfig{
			MaxRetries:     config.MaxRetries,
			InitialBackoff: config.InitialBackoff,
			MaxBackoff:     config.MaxBackoff,
		}))
	}
	return qwen.NewProvider(opts...).RerankClient(qwen.RerankClientConfig{Model: config.Model})
}

func newDeepSeekClient(config Config, doer HTTPDoer) (Client, error) {
	if transportOrDefault(config.Transport) == TransportResponses {
		return nil, fmt.Errorf("provider %q does not support transport %q", config.Provider, TransportResponses)
	}

	opts := []deepseek.ProviderOption{
		deepseek.WithAPIKey(config.APIKey),
		deepseek.WithExtraBody(configExtraBody(config)),
		deepseek.WithHTTPDoer(doer),
	}
	if config.BaseURL != "" {
		opts = append(opts, deepseek.WithBaseURL(config.BaseURL))
	}
	if config.StreamFirstEventTimeout > 0 {
		opts = append(opts, deepseek.WithStreamFirstEventTimeout(config.StreamFirstEventTimeout))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, deepseek.WithRetry(deepseek.RetryConfig{
			MaxRetries:     config.MaxRetries,
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
