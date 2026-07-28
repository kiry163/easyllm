package qwen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

func TestRerankClientMapsRequestAndParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/reranks" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "qwen3-rerank" || body["query"] != "what is reranking?" {
			t.Fatalf("unexpected request body: %#v", body)
		}
		if body["top_n"] != float64(1) || body["instruct"] != "rank passages" {
			t.Fatalf("unexpected rerank options: %#v", body)
		}
		if body["custom"] != "request" {
			t.Fatalf("request options did not override extra body: %#v", body["custom"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "rerank_123",
			"model": "qwen3-rerank",
			"results": []map[string]any{
				{"index": 2, "relevance_score": 0.91},
				{"index": 0, "relevance_score": 0.42},
			},
			"usage": map[string]any{"total_tokens": 19},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithExtraBody(map[string]any{"custom": "config"}),
	).RerankClient(RerankClientConfig{Model: "qwen3-rerank"})
	documents := []string{"first", "second", "third"}
	resp, err := client.Rerank(context.Background(), model.RerankRequest{
		Query:       "what is reranking?",
		Documents:   documents,
		TopN:        1,
		Instruction: "rank passages",
		Options:     map[string]any{"custom": "request"},
	})
	if err != nil {
		t.Fatalf("Rerank returned error: %v", err)
	}
	if resp.Provider != "qwen" || resp.Model != "qwen3-rerank" || resp.ResponseID != "rerank_123" {
		t.Fatalf("unexpected response metadata: %+v", resp)
	}
	if resp.Usage.TotalTokens != 19 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if len(resp.Results) != 2 || resp.Results[0].Index != 2 || resp.Results[0].Document != "third" || resp.Results[1].Document != "first" {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
}

func TestRerankClientRejectsOutOfRangeResponseIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"index": 2, "relevance_score": 0.9}},
		})
	}))
	defer server.Close()

	client := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL)).RerankClient(RerankClientConfig{Model: "qwen3-rerank"})
	_, err := client.Rerank(context.Background(), model.RerankRequest{Query: "query", Documents: []string{"only"}})
	if err == nil || err.Error() != "rerank response index 2 is out of range" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRerankClientRetriesServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"index": 0, "relevance_score": 0.9}}})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithRetry(RetryConfig{MaxRetries: 1, InitialBackoff: time.Millisecond}),
	).RerankClient(RerankClientConfig{Model: "qwen3-rerank"})
	resp, err := client.Rerank(context.Background(), model.RerankRequest{Query: "query", Documents: []string{"document"}})
	if err != nil {
		t.Fatalf("Rerank returned error: %v", err)
	}
	if attempts != 2 || resp.RetryCount != 1 {
		t.Fatalf("attempts=%d retry_count=%d", attempts, resp.RetryCount)
	}
}

func TestRerankClientValidatesRequest(t *testing.T) {
	client := NewProvider(WithAPIKey("token")).RerankClient(RerankClientConfig{Model: "qwen3-rerank"})
	tests := []struct {
		name string
		req  model.RerankRequest
		want string
	}{
		{name: "query", req: model.RerankRequest{Documents: []string{"document"}}, want: "query is required"},
		{name: "documents", req: model.RerankRequest{Query: "query"}, want: "documents are required"},
		{name: "empty document", req: model.RerankRequest{Query: "query", Documents: []string{""}}, want: "document at index 0 is required"},
		{name: "negative top n", req: model.RerankRequest{Query: "query", Documents: []string{"document"}, TopN: -1}, want: "top_n cannot be negative"},
		{name: "large top n", req: model.RerankRequest{Query: "query", Documents: []string{"document"}, TopN: 2}, want: "top_n cannot exceed document count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Rerank(context.Background(), tt.req)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRerankClientRejectsUnsupportedRequestModel(t *testing.T) {
	client := NewProvider(WithAPIKey("token")).RerankClient(RerankClientConfig{Model: "qwen3-rerank"})
	_, err := client.Rerank(context.Background(), model.RerankRequest{
		Model:     "gte-rerank-v2",
		Query:     "query",
		Documents: []string{"document"},
	})
	if err == nil || err.Error() != `model "gte-rerank-v2" is not supported for qwen reranking` {
		t.Fatalf("unexpected error: %v", err)
	}
}
