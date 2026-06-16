package compat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kiry163/easyllm/internal/jsonrepair"
	"github.com/kiry163/easyllm/internal/model"
)

func (c *Client) generateStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) (*model.ModelResponse, error) {
	switch c.transport {
	case TransportResponses:
		return c.generateResponsesStream(ctx, req, handler)
	default:
		return c.generateChatStream(ctx, req, handler)
	}
}

func (c *Client) generateChatStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) (*model.ModelResponse, error) {
	body := map[string]any{
		"model":          c.resolveModel(req.Model),
		"messages":       openAIChatMessages(req.Input),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
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
	return c.doStream(ctx, payload, "/chat/completions", c.parseChatStream(handler))
}

func (c *Client) generateResponsesStream(ctx context.Context, req model.ModelRequest, handler model.StreamHandler) (*model.ModelResponse, error) {
	body := map[string]any{
		"model":  c.resolveModel(req.Model),
		"input":  openAIResponsesInput(req.Input),
		"stream": true,
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAIResponsesTools(req.Tools)
	}
	for key, value := range c.mergedBody(req.Options) {
		body[key] = value
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}
	return c.doStream(ctx, payload, "/responses", c.parseResponsesStream(handler))
}

func (c *Client) doStream(ctx context.Context, payload []byte, path string, parse func(context.Context, io.Reader) (*model.ModelResponse, bool, error)) (*model.ModelResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.maxRetries()+1; attempt++ {
		streamCtx, cancel := context.WithCancel(ctx)
		httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			cancel()
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
			cancel()
			if !c.shouldRetry(ctx, attempt, resp.StatusCode, nil) {
				return nil, lastErr
			}
			continue
		}
		out, firstEventReceived, err := parse(streamCtx, resp.Body)
		closeErr := resp.Body.Close()
		cancel()
		if err != nil {
			if !firstEventReceived && c.shouldRetry(ctx, attempt, 0, err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out.Provider = c.providerName
		out.RetryCount = attempt - 1
		return out, nil
	}
	return nil, lastErr
}

func (c *Client) parseChatStream(handler model.StreamHandler) func(context.Context, io.Reader) (*model.ModelResponse, bool, error) {
	return func(ctx context.Context, r io.Reader) (*model.ModelResponse, bool, error) {
		type toolCallState struct {
			CallID    string
			Name      string
			Arguments strings.Builder
		}
		resp := &model.ModelResponse{}
		var message strings.Builder
		var reasoning strings.Builder
		toolCalls := map[int]*toolCallState{}

		firstEventReceived, err := readSSE(ctx, r, c.streamFirstEventTimeout, func(data []byte) error {
			var event struct {
				ID      string `json:"id"`
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
						ToolCalls        []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
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
			if err := json.Unmarshal(data, &event); err != nil {
				return err
			}
			if event.ID != "" {
				resp.ResponseID = event.ID
			}
			if event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 {
				resp.Usage = model.Usage{
					InputTokens:       event.Usage.PromptTokens,
					OutputTokens:      event.Usage.CompletionTokens,
					TotalTokens:       event.Usage.TotalTokens,
					CachedInputTokens: event.Usage.PromptTokensDetails.CachedTokens,
				}
			}
			for _, choice := range event.Choices {
				if choice.FinishReason != "" {
					resp.FinishReason = choice.FinishReason
				}
				if delta := choice.Delta.Content; delta != "" {
					message.WriteString(delta)
					if handler != nil {
						if err := handler(model.StreamEvent{Type: model.StreamEventMessageDelta, Text: delta}); err != nil {
							return err
						}
					}
				}
				if delta := choice.Delta.ReasoningContent; delta != "" {
					reasoning.WriteString(delta)
				}
				for _, rawCall := range choice.Delta.ToolCalls {
					current := toolCalls[rawCall.Index]
					if current == nil {
						current = &toolCallState{}
						toolCalls[rawCall.Index] = current
					}
					if rawCall.ID != "" {
						current.CallID = rawCall.ID
					}
					if rawCall.Function.Name != "" {
						current.Name = rawCall.Function.Name
					}
					if rawCall.Function.Arguments != "" {
						current.Arguments.WriteString(rawCall.Function.Arguments)
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, firstEventReceived, err
		}

		assistant := model.AssistantOutput{
			Role:          "assistant",
			ProviderState: chatProviderState(reasoning.String()),
		}
		if message.Len() > 0 {
			assistant.Content = append(assistant.Content, model.TextPart{Text: message.String()})
		}
		if len(toolCalls) > 0 {
			for i := 0; i < len(toolCalls); i++ {
				current := toolCalls[i]
				if current == nil {
					continue
				}
				rawArgs := current.Arguments.String()
				args, repaired, err := jsonrepair.DecodeJSONObjectString(rawArgs)
				if err != nil {
					args = map[string]any{"raw": rawArgs}
				}
				assistant.ToolCalls = append(assistant.ToolCalls, model.ToolCallOutput{
					CallID:        current.CallID,
					Name:          current.Name,
					Arguments:     args,
					RawArguments:  rawArgs,
					Repaired:      repaired,
					ProviderState: chatProviderState(reasoning.String()),
				})
			}
		}
		if len(assistant.Content) > 0 || len(assistant.ToolCalls) > 0 {
			resp.Output = append(resp.Output, assistant)
		}
		return resp, firstEventReceived, nil
	}
}

func (c *Client) parseResponsesStream(handler model.StreamHandler) func(context.Context, io.Reader) (*model.ModelResponse, bool, error) {
	return func(ctx context.Context, r io.Reader) (*model.ModelResponse, bool, error) {
		resp := &model.ModelResponse{}
		var message strings.Builder
		var completed bool

		firstEventReceived, err := readSSE(ctx, r, c.streamFirstEventTimeout, func(data []byte) error {
			var envelope struct {
				Type     string          `json:"type"`
				Delta    string          `json:"delta"`
				Response json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				return err
			}
			switch envelope.Type {
			case "response.output_text.delta":
				if envelope.Delta != "" {
					message.WriteString(envelope.Delta)
					if handler != nil {
						if err := handler(model.StreamEvent{Type: model.StreamEventMessageDelta, Text: envelope.Delta}); err != nil {
							return err
						}
					}
				}
			case "response.completed":
				completed = true
				if len(envelope.Response) == 0 {
					return nil
				}
				parsed, err := parseResponsesResponse(bytes.NewReader(envelope.Response))
				if err != nil {
					return err
				}
				*resp = *parsed
			}
			return nil
		})
		if err != nil {
			return nil, firstEventReceived, err
		}
		if completed {
			return resp, firstEventReceived, nil
		}
		if message.Len() > 0 {
			resp.Output = append(resp.Output, model.AssistantOutput{
				Role:    "assistant",
				Content: []model.TextPart{{Text: message.String()}},
			})
		}
		return resp, firstEventReceived, nil
	}
}

func readSSE(ctx context.Context, r io.Reader, firstEventTimeout time.Duration, handle func([]byte) error) (bool, error) {
	if firstEventTimeout <= 0 {
		return readSSESync(r, handle)
	}
	errCh := make(chan error, 1)
	payloadCh := make(chan []byte)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(payloadCh)
		_, err := readSSESync(r, func(data []byte) error {
			select {
			case payloadCh <- data:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		errCh <- err
	}()

	timer := time.NewTimer(firstEventTimeout)
	defer timer.Stop()
	firstEventReceived := false
	for {
		if firstEventReceived {
			select {
			case <-ctx.Done():
				return true, ctx.Err()
			case data, ok := <-payloadCh:
				if !ok {
					return true, <-errCh
				}
				if err := handle(data); err != nil {
					cancel()
					return true, err
				}
			}
			continue
		}

		select {
		case <-timer.C:
			cancel()
			return false, ErrStreamFirstEventTimeout
		case <-ctx.Done():
			return false, ctx.Err()
		case data, ok := <-payloadCh:
			if !ok {
				return false, <-errCh
			}
			firstEventReceived = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if err := handle(data); err != nil {
				cancel()
				return true, err
			}
		}
	}
}

func readSSESync(r io.Reader, handle func([]byte) error) (bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var dataLines []string
	var firstEventReceived bool
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		payload = strings.TrimSpace(payload)
		if payload == "" {
			return nil
		}
		if payload == "[DONE]" {
			return nil
		}
		firstEventReceived = true
		return handle([]byte(payload))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return firstEventReceived, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
	}
	if err := scanner.Err(); err != nil {
		return firstEventReceived, err
	}
	return firstEventReceived, flush()
}
