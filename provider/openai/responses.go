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

func (c *Client) generateResponses(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	body := map[string]any{
		"model": req.Model,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}
	var lastErr error
	for attempt := 1; attempt <= c.retry.maxAttempts(); attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create responses request: %w", err)
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
			lastErr = fmt.Errorf("responses request failed: status=%d body=%s", resp.StatusCode, string(preview))
			if !c.shouldRetry(ctx, attempt, resp.StatusCode, nil) {
				return nil, lastErr
			}
			continue
		}
		defer resp.Body.Close()
		return parseResponsesResponse(resp.Body)
	}
	return nil, lastErr
}

func parseResponsesResponse(r io.Reader) (*provider.ModelResponse, error) {
	var raw struct {
		ID     string `json:"id"`
		Output []struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Name      string `json:"name"`
			CallID    string `json:"call_id"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := &provider.ModelResponse{
		ResponseID: raw.ID,
		Provider:   "openai",
		Usage: provider.Usage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
			TotalTokens:  raw.Usage.TotalTokens,
		},
	}
	for _, item := range raw.Output {
		switch item.Type {
		case "function_call":
			args, repaired, err := tool.DecodeJSONObjectString(item.Arguments)
			if err != nil {
				args = map[string]any{"raw": item.Arguments}
			}
			out.Output = append(out.Output, provider.ToolCallOutput{
				CallID:       item.CallID,
				Name:         item.Name,
				Arguments:    args,
				RawArguments: item.Arguments,
				Repaired:     repaired,
			})
		case "message":
			parts := make([]provider.TextPart, 0, len(item.Content))
			for _, part := range item.Content {
				if part.Text == "" {
					continue
				}
				parts = append(parts, provider.TextPart{Text: part.Text})
			}
			out.Output = append(out.Output, provider.MessageOutput{
				Role:    item.Role,
				Content: parts,
			})
		}
	}
	return out, nil
}
