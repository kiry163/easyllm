package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

type ImageClientConfig struct {
	Model string
}

type ImageClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	retry      RetryConfig
	extraBody  map[string]any
	model      string
}

func (p *Provider) ImageClient(config ImageClientConfig) model.ImageClient {
	return &ImageClient{
		apiKey:     p.apiKey,
		baseURL:    p.baseURL,
		httpClient: p.httpClient,
		retry:      p.retry,
		extraBody:  cloneMap(p.extraBody),
		model:      config.Model,
	}
}

func (c *ImageClient) GenerateImage(ctx context.Context, req model.ImageRequest) (*model.ImageResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	body := cloneMap(c.extraBody)
	if body == nil {
		body = map[string]any{}
	}

	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(c.model)
	}
	body["model"] = modelName
	body["prompt"] = req.Prompt
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.Quality != "" {
		body["quality"] = req.Quality
	}
	if req.Background != "" {
		body["background"] = req.Background
	}
	if req.OutputFormat != "" {
		body["output_format"] = req.OutputFormat
	}
	if req.Count <= 0 {
		body["n"] = 1
	} else {
		body["n"] = req.Count
	}
	for key, value := range req.Options {
		body[key] = value
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal image request: %w", err)
	}

	resp, err := c.do(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw struct {
		Created int64 `json:"created"`
		Data    []struct {
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Usage struct {
			TotalTokens  int `json:"total_tokens"`
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := &model.ImageResponse{
		Created:  raw.Created,
		Provider: "openai",
		Raw:      raw,
		Usage: model.Usage{
			TotalTokens:  raw.Usage.TotalTokens,
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
		},
	}
	for _, item := range raw.Data {
		out.Images = append(out.Images, model.GeneratedImage{
			B64JSON:       item.B64JSON,
			RevisedPrompt: item.RevisedPrompt,
		})
	}
	return out, nil
}

func (c *ImageClient) do(ctx context.Context, payload []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts(c.retry); attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/generations", bytes.NewReader(payload))
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
			if !shouldRetryImageRequest(ctx, c.retry, attempt, 0, err) {
				return nil, lastErr
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("request failed: status=%d body=%s", resp.StatusCode, string(preview))
			if !shouldRetryImageRequest(ctx, c.retry, attempt, resp.StatusCode, nil) {
				return nil, lastErr
			}
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

func maxAttempts(cfg RetryConfig) int {
	if cfg.MaxAttempts > 0 {
		return cfg.MaxAttempts
	}
	return 1
}

func shouldRetryImageRequest(ctx context.Context, cfg RetryConfig, attempt int, statusCode int, err error) bool {
	if attempt >= maxAttempts(cfg) {
		return false
	}
	if err == nil && (statusCode < 500 || statusCode >= 600) {
		return false
	}
	delay := retryDelay(cfg, attempt)
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func retryDelay(cfg RetryConfig, attempt int) time.Duration {
	delay := cfg.InitialBackoff
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
		return cfg.MaxBackoff
	}
	return delay
}
