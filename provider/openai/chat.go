package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

func (c *Client) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	switch c.transport {
	case transportResponses:
		return c.generateResponses(ctx, req)
	default:
		return c.generateChat(ctx, req)
	}
}

func (c *Client) generateChat(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	body := map[string]any{
		"model": req.Model,
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAITools(req.Tools)
		body["tool_choice"] = "auto"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}
	var lastErr error
	for attempt := 1; attempt <= c.retry.maxAttempts(); attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create chat request: %w", err)
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
			lastErr = fmt.Errorf("chat request failed: status=%d body=%s", resp.StatusCode, string(preview))
			if !c.shouldRetry(ctx, attempt, resp.StatusCode, nil) {
				return nil, lastErr
			}
			continue
		}
		defer resp.Body.Close()
		return parseChatResponse(resp.Body)
	}
	return nil, lastErr
}

func parseChatResponse(r io.Reader) (*provider.ModelResponse, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := &provider.ModelResponse{
		Provider:     "openai",
		FinishReason: "",
		Usage: provider.Usage{
			InputTokens:  raw.Usage.PromptTokens,
			OutputTokens: raw.Usage.CompletionTokens,
			TotalTokens:  raw.Usage.TotalTokens,
		},
	}
	if len(raw.Choices) == 0 {
		return out, nil
	}
	out.FinishReason = raw.Choices[0].FinishReason
	for _, call := range raw.Choices[0].Message.ToolCalls {
		args, repaired, err := tool.DecodeJSONObjectString(call.Function.Arguments)
		if err != nil {
			args = map[string]any{"raw": call.Function.Arguments}
		}
		out.Output = append(out.Output, provider.ToolCallOutput{
			CallID:       call.ID,
			Name:         call.Function.Name,
			Arguments:    args,
			RawArguments: call.Function.Arguments,
			Repaired:     repaired,
		})
	}
	if len(out.Output) == 0 && raw.Choices[0].Message.Content != "" {
		out.Output = append(out.Output, provider.MessageOutput{
			Role: "assistant",
			Content: []provider.TextPart{
				{Text: raw.Choices[0].Message.Content},
			},
		})
	}
	return out, nil
}

func openAITools(tools []tool.Definition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, current := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        current.Name,
				"description": current.Description,
				"parameters":  current.Parameters,
			},
		})
	}
	return out
}
