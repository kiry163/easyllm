package compat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

type countingDoer struct {
	next  model.HTTPDoer
	calls atomic.Int32
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return d.next.Do(req)
}

func TestHTTPDoerWrapsEveryRetryAttempt(t *testing.T) {
	var serverCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverCalls.Add(1) == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"content": "done"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	doer := &countingDoer{next: http.DefaultClient}
	client := NewChatClient(
		"token",
		server.URL,
		WithHTTPDoer(doer),
		WithRetry(RetryConfig{
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		}),
	)
	_, err := client.Generate(context.Background(), model.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if got := doer.calls.Load(); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}

func TestHTTPDoerWrapsEveryModelListRetryAttempt(t *testing.T) {
	var serverCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverCalls.Add(1) == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "test-model"}},
		})
	}))
	defer server.Close()

	doer := &countingDoer{next: http.DefaultClient}
	client := NewChatClient(
		"token",
		server.URL,
		WithHTTPDoer(doer),
		WithRetry(RetryConfig{
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		}),
	)
	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if got := doer.calls.Load(); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}
