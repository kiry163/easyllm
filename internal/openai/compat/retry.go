package compat

import (
	"context"
	"net/http"
	"time"
)

func (c *Client) shouldRetry(ctx context.Context, attempt int, statusCode int, err error) bool {
	if attempt > c.retry.maxRetries() {
		return false
	}
	if err == nil {
		switch statusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		default:
			return false
		}
	}
	wait := c.retry.backoffForAttempt(attempt)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r RetryConfig) maxRetries() int {
	if r.MaxRetries < 0 {
		return 0
	}
	return r.MaxRetries
}

func (r RetryConfig) backoffForAttempt(attempt int) time.Duration {
	wait := r.InitialBackoff
	if wait <= 0 {
		wait = 100 * time.Millisecond
	}
	for i := 1; i < attempt; i++ {
		wait *= 2
	}
	if r.MaxBackoff > 0 && wait > r.MaxBackoff {
		return r.MaxBackoff
	}
	return wait
}
