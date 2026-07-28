package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

func TestEmbeddingClientMapsRequestAndParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "request-model" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		if body["dimensions"] != float64(512) {
			t.Fatalf("unexpected dimensions: %#v", body["dimensions"])
		}
		input, ok := body["input"].([]any)
		if !ok || len(input) != 2 || input[0] != "first" || input[1] != "second" {
			t.Fatalf("unexpected input: %#v", body["input"])
		}
		if body["custom"] != "request" {
			t.Fatalf("request options did not override extra body: %#v", body["custom"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "response-model",
			"data": []map[string]any{
				{"object": "embedding", "index": 1, "embedding": []float64{0.3, 0.4}},
				{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2}},
			},
			"usage": map[string]any{"prompt_tokens": 7, "total_tokens": 7},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithProviderName("openai_compatible"),
		WithExtraBody(map[string]any{"custom": "config"}),
	).EmbeddingClient(EmbeddingClientConfig{Model: "default-model"})

	resp, err := client.Embed(context.Background(), model.EmbeddingRequest{
		Model:      "request-model",
		Input:      []string{"first", "second"},
		Dimensions: 256,
		Options: map[string]any{
			"dimensions": 512,
			"custom":     "request",
		},
	})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if resp.Provider != "openai_compatible" || resp.Model != "response-model" {
		t.Fatalf("unexpected response metadata: %+v", resp)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.TotalTokens != 7 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if len(resp.Embeddings) != 2 || resp.Embeddings[0].Index != 1 || resp.Embeddings[0].Vector[1] != float32(0.4) {
		t.Fatalf("unexpected embeddings: %+v", resp.Embeddings)
	}
	if _, ok := resp.Raw.(map[string]any); !ok {
		t.Fatalf("unexpected raw response type: %T", resp.Raw)
	}
}

func TestEmbeddingClientRetriesRateLimit(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"index": 0, "embedding": []float64{0.1}}},
			"model": "embedding-model",
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithRetry(RetryConfig{MaxRetries: 1, InitialBackoff: time.Millisecond}),
	).EmbeddingClient(EmbeddingClientConfig{Model: "embedding-model"})
	resp, err := client.Embed(context.Background(), model.EmbeddingRequest{Input: []string{"text"}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if attempts != 2 || resp.RetryCount != 1 {
		t.Fatalf("attempts=%d retry_count=%d", attempts, resp.RetryCount)
	}
}

func TestEmbeddingClientValidatesRequest(t *testing.T) {
	client := NewProvider(WithAPIKey("token")).EmbeddingClient(EmbeddingClientConfig{Model: "embedding-model"})
	if _, err := client.Embed(context.Background(), model.EmbeddingRequest{}); err == nil || err.Error() != "input is required" {
		t.Fatalf("unexpected missing input error: %v", err)
	}
	if _, err := client.Embed(context.Background(), model.EmbeddingRequest{Input: []string{"text"}, Dimensions: -1}); err == nil || err.Error() != "dimensions cannot be negative" {
		t.Fatalf("unexpected dimensions error: %v", err)
	}
}
