package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPostDoesNotRetryBadRequestAndLimitsPreview(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("x", 600)))
	}))
	defer server.Close()

	client := Client{
		BaseURL:  server.URL,
		HTTPDoer: server.Client(),
		Retry:    RetryConfig{MaxRetries: 2, InitialBackoff: time.Millisecond},
	}
	_, err := client.Post(context.Background(), "/request", map[string]any{"value": true})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "request failed: status=400 body=" + strings.Repeat("x", 512)
	if err.Error() != want {
		t.Fatalf("unexpected error: %q", err)
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}

func TestPostStopsDuringCanceledRetryBackoff(t *testing.T) {
	requestErr := errors.New("network unavailable")
	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	client := Client{
		BaseURL: "https://example.com",
		HTTPDoer: doerFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			cancel()
			return nil, requestErr
		}),
		Retry: RetryConfig{MaxRetries: 2, InitialBackoff: time.Second},
	}

	_, err := client.Post(ctx, "/request", map[string]any{"value": true})
	if !errors.Is(err, requestErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}
