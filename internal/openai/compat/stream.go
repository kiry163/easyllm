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

func (c *Client) doStream(ctx context.Context, payload []byte, path string, parse func(io.Reader) (*model.ModelResponse, error)) (*model.ModelResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.maxAttempts(); attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
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
		out.Provider = c.providerName
		return out, nil
	}
	return nil, lastErr
}

func (c *Client) parseChatStream(handler model.StreamHandler) func(io.Reader) (*model.ModelResponse, error) {
	return func(r io.Reader) (*model.ModelResponse, error) {
		type toolCallState struct {
			CallID    string
			Name      string
			Arguments strings.Builder
		}
		resp := &model.ModelResponse{}
		var message strings.Builder
		toolCalls := map[int]*toolCallState{}

		err := readSSE(r, func(data []byte) error {
			var event struct {
				ID      string `json:"id"`
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
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
			return nil, err
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
				resp.Output = append(resp.Output, model.ToolCallOutput{
					CallID:       current.CallID,
					Name:         current.Name,
					Arguments:    args,
					RawArguments: rawArgs,
					Repaired:     repaired,
				})
			}
			return resp, nil
		}
		if message.Len() > 0 {
			resp.Output = append(resp.Output, model.MessageOutput{
				Role:    "assistant",
				Content: []model.TextPart{{Text: message.String()}},
			})
		}
		return resp, nil
	}
}

func (c *Client) parseResponsesStream(handler model.StreamHandler) func(io.Reader) (*model.ModelResponse, error) {
	return func(r io.Reader) (*model.ModelResponse, error) {
		resp := &model.ModelResponse{}
		var message strings.Builder
		var completed bool

		err := readSSE(r, func(data []byte) error {
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
			return nil, err
		}
		if completed {
			return resp, nil
		}
		if message.Len() > 0 {
			resp.Output = append(resp.Output, model.MessageOutput{
				Role:    "assistant",
				Content: []model.TextPart{{Text: message.String()}},
			})
		}
		return resp, nil
	}
}

func readSSE(r io.Reader, handle func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var dataLines []string
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
		return handle([]byte(payload))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
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
		return err
	}
	return flush()
}
