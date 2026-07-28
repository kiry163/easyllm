package httpjson

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

type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type Client struct {
	APIKey   string
	BaseURL  string
	HTTPDoer model.HTTPDoer
	Retry    RetryConfig
}

type Response struct {
	Body       []byte
	RetryCount int
}

func (c Client) Post(ctx context.Context, path string, body map[string]any) (*Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if c.HTTPDoer == nil {
		return nil, fmt.Errorf("http doer is required")
	}

	url := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/" + strings.TrimLeft(path, "/")
	var lastErr error
	for attempt := 1; attempt <= c.maxRetries()+1; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if apiKey := strings.TrimSpace(c.APIKey); apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := c.HTTPDoer.Do(httpReq)
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

		responseBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		return &Response{Body: responseBody, RetryCount: attempt - 1}, nil
	}
	return nil, lastErr
}

func (c Client) maxRetries() int {
	if c.Retry.MaxRetries < 0 {
		return 0
	}
	return c.Retry.MaxRetries
}

func (c Client) shouldRetry(ctx context.Context, attempt, statusCode int, requestErr error) bool {
	if attempt > c.maxRetries() {
		return false
	}
	if requestErr == nil {
		switch statusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		default:
			return false
		}
	}

	timer := time.NewTimer(c.backoffForAttempt(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c Client) backoffForAttempt(attempt int) time.Duration {
	wait := c.Retry.InitialBackoff
	if wait <= 0 {
		wait = 100 * time.Millisecond
	}
	for i := 1; i < attempt; i++ {
		wait *= 2
	}
	if c.Retry.MaxBackoff > 0 && wait > c.Retry.MaxBackoff {
		return c.Retry.MaxBackoff
	}
	return wait
}
