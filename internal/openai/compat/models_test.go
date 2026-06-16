package compat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListModelsRetriesAndParsesModels(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if attempts == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":       "qwen-plus",
					"object":   "model",
					"created":  1710000000,
					"owned_by": "dashscope",
				},
			},
		})
	}))
	defer server.Close()

	client := NewChatClient(
		"token",
		server.URL,
		WithProviderName("qwen"),
		WithRetry(RetryConfig{
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		}),
	)
	resp, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if resp.Provider != "qwen" {
		t.Fatalf("unexpected provider: %q", resp.Provider)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("unexpected model count: %d", len(resp.Models))
	}
	if resp.Models[0].ID != "qwen-plus" || resp.Models[0].Provider != "qwen" {
		t.Fatalf("unexpected model: %+v", resp.Models[0])
	}
}
