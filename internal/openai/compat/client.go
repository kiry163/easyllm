package compat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kiry163/easyllm/internal/jsonrepair"
	"github.com/kiry163/easyllm/internal/model"
)

type Transport string

const (
	TransportChat      Transport = "chat"
	TransportResponses Transport = "responses"
)

type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type Option func(*Client)

type Client struct {
	apiKey                  string
	baseURL                 string
	defaultModel            string
	providerName            string
	httpClient              *http.Client
	streamFirstEventTimeout time.Duration
	retry                   RetryConfig
	transport               Transport
	extraBody               map[string]any
	defaultOptions          map[string]any
}

var ErrStreamFirstEventTimeout = errors.New("stream first event timeout")

func WithRetry(config RetryConfig) Option {
	return func(c *Client) {
		if config.MaxRetries > 0 {
			c.retry.MaxRetries = config.MaxRetries
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

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.httpClient.Timeout = timeout
		}
	}
}

func WithStreamFirstEventTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.streamFirstEventTimeout = timeout
		}
	}
}

func WithDefaultModel(model string) Option {
	return func(c *Client) {
		c.defaultModel = strings.TrimSpace(model)
	}
}

func WithExtraBody(extra map[string]any) Option {
	return func(c *Client) {
		c.extraBody = cloneMap(extra)
	}
}

func WithDefaultOptions(options map[string]any) Option {
	return func(c *Client) {
		c.defaultOptions = cloneMap(options)
	}
}

func WithTemperature(value float64) Option {
	return func(c *Client) {
		if c.defaultOptions == nil {
			c.defaultOptions = map[string]any{}
		}
		c.defaultOptions["temperature"] = value
	}
}

func WithTopP(value float64) Option {
	return func(c *Client) {
		if c.defaultOptions == nil {
			c.defaultOptions = map[string]any{}
		}
		c.defaultOptions["top_p"] = value
	}
}

func WithMaxTokens(value int) Option {
	return func(c *Client) {
		if c.defaultOptions == nil {
			c.defaultOptions = map[string]any{}
		}
		c.defaultOptions["max_tokens"] = value
	}
}

func WithProviderName(name string) Option {
	return func(c *Client) {
		c.providerName = strings.TrimSpace(name)
	}
}

func NewChatClient(apiKey, baseURL string, opts ...Option) *Client {
	return newClient(apiKey, baseURL, TransportChat, opts...)
}

func NewResponsesClient(apiKey, baseURL string, opts ...Option) *Client {
	return newClient(apiKey, baseURL, TransportResponses, opts...)
}

func newClient(apiKey, baseURL string, transport Transport, opts ...Option) *Client {
	client := &Client{
		apiKey:       strings.TrimSpace(apiKey),
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		defaultModel: "",
		providerName: "openai_compat",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retry: RetryConfig{
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     time.Second,
		},
		transport:      transport,
		extraBody:      map[string]any{},
		defaultOptions: map[string]any{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

func (c *Client) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	switch c.transport {
	case TransportResponses:
		return c.generateResponses(ctx, req)
	default:
		return c.generateChat(ctx, req)
	}
}

func (c *Client) GenerateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) error {
	resp, err := c.generateStream(ctx, req, handler)
	if err != nil {
		return err
	}
	if handler != nil {
		if err := handler(model.StreamEvent{Type: model.StreamEventDone, Raw: resp}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) generateChat(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	body := map[string]any{
		"model":    c.resolveModel(req.Model),
		"messages": openAIChatMessages(req.Input),
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAIChatTools(req.Tools)
		body["tool_choice"] = "auto"
	}
	for key, value := range c.mergedBody(req.Options) {
		body[key] = value
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}
	return c.do(ctx, payload, "/chat/completions", parseChatResponse)
}

func (c *Client) generateResponses(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	body := map[string]any{
		"model": c.resolveModel(req.Model),
		"input": openAIResponsesInput(req.Input),
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAIResponsesTools(req.Tools)
		// keep room for compat expansion; not all providers accept this on responses yet
	}
	for key, value := range c.mergedBody(req.Options) {
		body[key] = value
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}
	return c.do(ctx, payload, "/responses", parseResponsesResponse)
}

func (c *Client) do(ctx context.Context, payload []byte, path string, parse func(io.Reader) (*model.ModelResponse, error)) (*model.ModelResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.maxRetries()+1; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if !c.shouldRetry(ctx, attempt, 0, err) {
				return nil, lastErr
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("request failed: status=%d body=%s", resp.StatusCode, string(preview))
			if !c.shouldRetry(ctx, attempt, resp.StatusCode, nil) {
				return nil, lastErr
			}
			continue
		}
		defer resp.Body.Close()
		out, err := parse(resp.Body)
		if err != nil {
			return nil, err
		}
		if out.Provider == "" {
			out.Provider = c.providerName
		} else {
			out.Provider = c.providerName
		}
		out.RetryCount = attempt - 1
		return out, nil
	}
	return nil, lastErr
}

func (c *Client) resolveModel(model string) string {
	model = strings.TrimSpace(model)
	if model != "" {
		return model
	}
	return c.defaultModel
}

func (c *Client) mergedBody(options map[string]any) map[string]any {
	merged := cloneMap(c.defaultOptions)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range options {
		if key == "extra_body" {
			continue
		}
		merged[key] = value
	}
	for key, value := range c.extraBody {
		merged[key] = value
	}
	if raw, ok := options["extra_body"]; ok {
		if extra, ok := raw.(map[string]any); ok {
			for key, value := range extra {
				merged[key] = value
			}
		}
	}
	return merged
}

func openAIChatMessages(items []model.InputItem) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch current := item.(type) {
		case model.SystemMessageItem:
			out = append(out, map[string]any{"role": "system", "content": textPartsToString(current.Content)})
		case model.UserMessageItem:
			out = append(out, map[string]any{"role": "user", "content": openAIChatContent(current.Content)})
		case model.AssistantMessageItem:
			msg := map[string]any{"role": "assistant", "content": textPartsToString(current.Content)}
			if len(current.ToolCalls) > 0 {
				msg["tool_calls"] = openAIChatToolCallItems(current.ToolCalls)
			}
			applyProviderStateToChatMessage(msg, current.ProviderState)
			out = append(out, msg)
		case model.ToolResultItem:
			out = append(out, map[string]any{
				"role":         "tool",
				"content":      current.Content,
				"tool_call_id": current.CallID,
			})
		}
	}
	return out
}

func openAIChatToolCallItems(items []model.ToolCallItem) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, current := range items {
		out = append(out, map[string]any{
			"id":   current.CallID,
			"type": "function",
			"function": map[string]any{
				"name":      current.Name,
				"arguments": toolArgumentsString(current.Arguments, current.RawArguments),
			},
		})
	}
	return out
}

func openAIResponsesInput(items []model.InputItem) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch current := item.(type) {
		case model.SystemMessageItem:
			out = append(out, map[string]any{"role": "system", "content": []map[string]any{{"type": "input_text", "text": textPartsToString(current.Content)}}})
		case model.UserMessageItem:
			out = append(out, map[string]any{"role": "user", "content": openAIResponsesInputContent(current.Content)})
		case model.AssistantMessageItem:
			out = append(out, map[string]any{"role": "assistant", "content": []map[string]any{{"type": "output_text", "text": textPartsToString(current.Content)}}})
			for _, call := range current.ToolCalls {
				out = append(out, map[string]any{"type": "function_call", "call_id": call.CallID, "name": call.Name, "arguments": toolArgumentsString(call.Arguments, call.RawArguments)})
			}
		case model.ToolResultItem:
			out = append(out, map[string]any{"type": "function_call_output", "call_id": current.CallID, "output": current.Content})
		}
	}
	return out
}

func openAIChatContent(parts []model.ContentPart) any {
	if !hasNonTextPart(parts) {
		return textPartsToString(parts)
	}
	content := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch contentPartType(part) {
		case model.ContentPartTypeImage:
			if part.ImageURL == "" {
				continue
			}
			imageURL := map[string]any{"url": part.ImageURL}
			if part.Detail != "" {
				imageURL["detail"] = string(part.Detail)
			}
			content = append(content, map[string]any{
				"type":      "image_url",
				"image_url": imageURL,
			})
		default:
			if part.Text == "" {
				continue
			}
			content = append(content, map[string]any{
				"type": "text",
				"text": part.Text,
			})
		}
	}
	return content
}

func openAIResponsesInputContent(parts []model.ContentPart) []map[string]any {
	content := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch contentPartType(part) {
		case model.ContentPartTypeImage:
			if part.ImageURL == "" {
				continue
			}
			image := map[string]any{
				"type":      "input_image",
				"image_url": part.ImageURL,
			}
			if part.Detail != "" {
				image["detail"] = string(part.Detail)
			}
			content = append(content, image)
		default:
			if part.Text == "" {
				continue
			}
			content = append(content, map[string]any{
				"type": "input_text",
				"text": part.Text,
			})
		}
	}
	if len(content) == 0 {
		return []map[string]any{{"type": "input_text", "text": ""}}
	}
	return content
}

func contentPartType(part model.ContentPart) model.ContentPartType {
	if part.Type != "" {
		return part.Type
	}
	if part.ImageURL != "" {
		return model.ContentPartTypeImage
	}
	return model.ContentPartTypeText
}

func hasNonTextPart(parts []model.ContentPart) bool {
	for _, part := range parts {
		if contentPartType(part) != model.ContentPartTypeText {
			return true
		}
	}
	return false
}

func parseChatResponse(r io.Reader) (*model.ModelResponse, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := &model.ModelResponse{
		Usage: model.Usage{
			InputTokens:       raw.Usage.PromptTokens,
			OutputTokens:      raw.Usage.CompletionTokens,
			TotalTokens:       raw.Usage.TotalTokens,
			CachedInputTokens: raw.Usage.PromptTokensDetails.CachedTokens,
		},
	}
	if len(raw.Choices) == 0 {
		return out, nil
	}
	out.FinishReason = raw.Choices[0].FinishReason
	assistant := model.AssistantOutput{
		Role:          "assistant",
		ProviderState: chatProviderState(raw.Choices[0].Message.ReasoningContent),
	}
	if raw.Choices[0].Message.Content != "" {
		assistant.Content = append(assistant.Content, model.TextPart{Text: raw.Choices[0].Message.Content})
	}
	for _, call := range raw.Choices[0].Message.ToolCalls {
		args, repaired, err := jsonrepair.DecodeJSONObjectString(call.Function.Arguments)
		if err != nil {
			args = map[string]any{"raw": call.Function.Arguments}
		}
		assistant.ToolCalls = append(assistant.ToolCalls, model.ToolCallOutput{
			CallID:        call.ID,
			Name:          call.Function.Name,
			Arguments:     args,
			RawArguments:  call.Function.Arguments,
			Repaired:      repaired,
			ProviderState: chatProviderState(raw.Choices[0].Message.ReasoningContent),
		})
	}
	if len(assistant.Content) > 0 || len(assistant.ToolCalls) > 0 {
		out.Output = append(out.Output, assistant)
	}
	return out, nil
}

func parseResponsesResponse(r io.Reader) (*model.ModelResponse, error) {
	var raw struct {
		ID     string `json:"id"`
		Output []struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Name      string `json:"name"`
			CallID    string `json:"call_id"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens        int `json:"input_tokens"`
			OutputTokens       int `json:"output_tokens"`
			TotalTokens        int `json:"total_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := &model.ModelResponse{
		ResponseID: raw.ID,
		Usage: model.Usage{
			InputTokens:       raw.Usage.InputTokens,
			OutputTokens:      raw.Usage.OutputTokens,
			TotalTokens:       raw.Usage.TotalTokens,
			CachedInputTokens: raw.Usage.InputTokensDetails.CachedTokens,
		},
	}
	for _, item := range raw.Output {
		switch item.Type {
		case "function_call":
			args, repaired, err := jsonrepair.DecodeJSONObjectString(item.Arguments)
			if err != nil {
				args = map[string]any{"raw": item.Arguments}
			}
			out.Output = append(out.Output, model.AssistantOutput{
				Role: "assistant",
				ToolCalls: []model.ToolCallOutput{{
					CallID:       item.CallID,
					Name:         item.Name,
					Arguments:    args,
					RawArguments: item.Arguments,
					Repaired:     repaired,
				}},
			})
		case "message":
			parts := make([]model.TextPart, 0, len(item.Content))
			for _, part := range item.Content {
				if part.Text != "" {
					parts = append(parts, model.TextPart{Text: part.Text})
				}
			}
			out.Output = append(out.Output, model.AssistantOutput{Role: item.Role, Content: parts})
		}
	}
	return out, nil
}

func openAIChatTools(tools []model.ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, current := range tools {
		function := map[string]any{
			"name":        current.Name,
			"description": current.Description,
			"parameters":  current.Parameters,
		}
		if current.Strict != nil {
			function["strict"] = *current.Strict
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": function,
		})
	}
	return out
}

func openAIResponsesTools(tools []model.ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, current := range tools {
		function := map[string]any{
			"type":        "function",
			"name":        current.Name,
			"description": current.Description,
			"parameters":  current.Parameters,
		}
		if current.Strict != nil {
			function["strict"] = *current.Strict
		}
		out = append(out, function)
	}
	return out
}

func textPartsToString(parts []model.TextPart) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0].Text
	}
	buf := bytes.Buffer{}
	for i, part := range parts {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(part.Text)
	}
	return buf.String()
}

func toolArgumentsString(args map[string]any, raw string) string {
	if raw != "" {
		return raw
	}
	if len(args) == 0 {
		return "{}"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
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

func chatProviderState(reasoningContent string) map[string]any {
	if reasoningContent == "" {
		return nil
	}
	return map[string]any{"reasoning_content": reasoningContent}
}

func applyProviderStateToChatMessage(msg map[string]any, state map[string]any) {
	if len(state) == 0 {
		return
	}
	if reasoningContent, ok := state["reasoning_content"].(string); ok && reasoningContent != "" {
		msg["reasoning_content"] = reasoningContent
	}
}
