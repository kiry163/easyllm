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

func TestImageClientMapsRequestAndParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		defer r.Body.Close()

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "gpt-image-1" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		if body["prompt"] != "diagram" {
			t.Fatalf("unexpected prompt: %#v", body["prompt"])
		}
		if body["size"] != "1024x1024" {
			t.Fatalf("unexpected size: %#v", body["size"])
		}
		if body["quality"] != "high" {
			t.Fatalf("unexpected quality: %#v", body["quality"])
		}
		if body["background"] != "opaque" {
			t.Fatalf("unexpected background: %#v", body["background"])
		}
		if body["output_format"] != "png" {
			t.Fatalf("unexpected output_format: %#v", body["output_format"])
		}
		if body["n"] != float64(1) {
			t.Fatalf("unexpected n: %#v", body["n"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 123,
			"data": []map[string]any{{
				"b64_json":       "aGVsbG8=",
				"revised_prompt": "revised",
			}},
			"usage": map[string]any{
				"total_tokens":  12,
				"input_tokens":  7,
				"output_tokens": 5,
			},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	).ImageClient(ImageClientConfig{Model: "gpt-image-1"})

	resp, err := client.GenerateImage(context.Background(), model.ImageRequest{
		Prompt:       "diagram",
		Size:         "1024x1024",
		Quality:      "high",
		Background:   "opaque",
		OutputFormat: "png",
	})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if resp.Created != 123 {
		t.Fatalf("unexpected created: %d", resp.Created)
	}
	if len(resp.Images) != 1 || resp.Images[0].B64JSON != "aGVsbG8=" {
		t.Fatalf("unexpected images: %#v", resp.Images)
	}
	if resp.Images[0].RevisedPrompt != "revised" {
		t.Fatalf("unexpected revised prompt: %#v", resp.Images[0].RevisedPrompt)
	}
	if resp.Usage.TotalTokens != 12 || resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
	if resp.Provider != "openai" {
		t.Fatalf("unexpected provider: %q", resp.Provider)
	}
}

func TestImageClientMergesExtraBodyAndPerRequestOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["background"] != "transparent" {
			t.Fatalf("unexpected background override: %#v", body["background"])
		}
		if body["quality"] != "medium" {
			t.Fatalf("unexpected quality override: %#v", body["quality"])
		}
		if body["custom_flag"] != true {
			t.Fatalf("unexpected custom flag: %#v", body["custom_flag"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data":    []map[string]any{{"b64_json": "aGVsbG8="}},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithExtraBody(map[string]any{
			"background":  "opaque",
			"quality":     "low",
			"custom_flag": true,
		}),
	).ImageClient(ImageClientConfig{Model: "gpt-image-1"})

	_, err := client.GenerateImage(context.Background(), model.ImageRequest{
		Prompt:     "diagram",
		Background: "transparent",
		Options: map[string]any{
			"quality": "medium",
		},
	})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
}

func TestImageClientRejectsMissingPrompt(t *testing.T) {
	client := NewProvider(WithAPIKey("token")).ImageClient(ImageClientConfig{Model: "gpt-image-1"})
	_, err := client.GenerateImage(context.Background(), model.ImageRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "prompt is required" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestImageClientRetriesServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data":    []map[string]any{{"b64_json": "aGVsbG8="}},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithRetry(RetryConfig{MaxAttempts: 2}),
	).ImageClient(ImageClientConfig{Model: "gpt-image-1"})

	resp, err := client.GenerateImage(context.Background(), model.ImageRequest{Prompt: "diagram"})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if len(resp.Images) != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestImageClientReturnsNon2xxPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad input", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	).ImageClient(ImageClientConfig{Model: "gpt-image-1"})

	_, err := client.GenerateImage(context.Background(), model.ImageRequest{Prompt: "diagram"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "request failed: status=400 body=bad input\n" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestImageClientUsesCustomHTTPClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data":    []map[string]any{{"b64_json": "aGVsbG8="}},
		})
	}))
	defer server.Close()

	httpClient := &http.Client{Timeout: 10 * time.Millisecond}
	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithHTTPClient(httpClient),
	).ImageClient(ImageClientConfig{Model: "gpt-image-1"})

	_, err := client.GenerateImage(context.Background(), model.ImageRequest{Prompt: "diagram"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
