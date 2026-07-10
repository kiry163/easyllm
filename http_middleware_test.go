package easyllm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewHTTPDoerAppliesMiddlewareAfterTimeoutConfiguration(t *testing.T) {
	applications := 0
	var observed time.Duration
	doer, err := newHTTPDoer(Config{
		Timeout: 75 * time.Millisecond,
		HTTPMiddleware: func(next HTTPDoer) HTTPDoer {
			applications++
			client, ok := next.(*http.Client)
			if !ok {
				t.Fatalf("expected *http.Client, got %T", next)
			}
			observed = client.Timeout
			return next
		},
	})
	if err != nil {
		t.Fatalf("newHTTPDoer returned error: %v", err)
	}
	if doer == nil || applications != 1 {
		t.Fatalf("unexpected doer=%v applications=%d", doer, applications)
	}
	if observed != 75*time.Millisecond {
		t.Fatalf("expected 75ms timeout, got %v", observed)
	}
}

func TestNewHTTPDoerUsesExistingDefaultTimeout(t *testing.T) {
	doer, err := newHTTPDoer(Config{})
	if err != nil {
		t.Fatalf("newHTTPDoer returned error: %v", err)
	}
	client, ok := doer.(*http.Client)
	if !ok {
		t.Fatalf("expected *http.Client, got %T", doer)
	}
	if client.Timeout != 30*time.Second {
		t.Fatalf("expected 30s timeout, got %v", client.Timeout)
	}
}

func TestNewHTTPDoerRejectsNilMiddlewareResult(t *testing.T) {
	_, err := newHTTPDoer(Config{
		HTTPMiddleware: func(HTTPDoer) HTTPDoer { return nil },
	})
	if err == nil || err.Error() != "http middleware returned nil doer" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func countingHTTPMiddleware(applications, calls *atomic.Int32) HTTPMiddleware {
	return func(next HTTPDoer) HTTPDoer {
		applications.Add(1)
		return httpDoerFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return next.Do(req)
		})
	}
}

func TestHTTPMiddlewareAppliesToEveryBuiltInModelProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"content": "done"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	for _, provider := range []string{
		ProviderOpenAI,
		ProviderOpenAICompatible,
		ProviderQwen,
		ProviderDeepSeek,
	} {
		t.Run(provider, func(t *testing.T) {
			var applications atomic.Int32
			var calls atomic.Int32
			client, err := NewClient(Config{
				Provider:       provider,
				APIKey:         "token",
				BaseURL:        server.URL,
				Model:          "test-model",
				HTTPMiddleware: countingHTTPMiddleware(&applications, &calls),
			})
			if err != nil {
				t.Fatalf("NewClient returned error: %v", err)
			}
			if _, err := client.Generate(context.Background(), ModelRequest{}); err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
			if applications.Load() != 1 || calls.Load() != 1 {
				t.Fatalf("applications=%d calls=%d", applications.Load(), calls.Load())
			}
		})
	}
}

func TestHTTPMiddlewareWrapsEveryPublicRetryAttempt(t *testing.T) {
	var serverCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverCalls.Add(1) == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
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

	var applications atomic.Int32
	var calls atomic.Int32
	client, err := NewClient(Config{
		Provider:       ProviderOpenAICompatible,
		APIKey:         "token",
		BaseURL:        server.URL,
		Model:          "test-model",
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		HTTPMiddleware: countingHTTPMiddleware(&applications, &calls),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if _, err := client.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if applications.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("applications=%d calls=%d", applications.Load(), calls.Load())
	}
}

func TestHTTPMiddlewareAppliesToImageAndModelList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/images/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"created": 1,
				"data":    []map[string]any{{"b64_json": "aGVsbG8="}},
			})
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"id": "test-model"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var applications atomic.Int32
	var calls atomic.Int32
	middleware := countingHTTPMiddleware(&applications, &calls)

	imageClient, err := NewImageClient(Config{
		Provider:       ProviderOpenAI,
		APIKey:         "token",
		BaseURL:        server.URL,
		Model:          "gpt-image-1",
		HTTPMiddleware: middleware,
	})
	if err != nil {
		t.Fatalf("NewImageClient returned error: %v", err)
	}
	if _, err := imageClient.GenerateImage(context.Background(), ImageRequest{Prompt: "diagram"}); err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}

	if _, err := ListModels(context.Background(), Config{
		Provider:       ProviderOpenAI,
		APIKey:         "token",
		BaseURL:        server.URL,
		HTTPMiddleware: middleware,
	}); err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if applications.Load() != 2 || calls.Load() != 2 {
		t.Fatalf("applications=%d calls=%d", applications.Load(), calls.Load())
	}
}

func TestPublicOperationsRejectNilHTTPDoer(t *testing.T) {
	nilMiddleware := func(HTTPDoer) HTTPDoer { return nil }
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	operations := []func() error{
		func() error {
			_, err := NewClient(Config{
				Provider:       ProviderOpenAICompatible,
				BaseURL:        server.URL,
				Model:          "test-model",
				HTTPMiddleware: nilMiddleware,
			})
			return err
		},
		func() error {
			_, err := NewImageClient(Config{
				Provider:       ProviderOpenAI,
				APIKey:         "token",
				BaseURL:        server.URL,
				Model:          "gpt-image-1",
				HTTPMiddleware: nilMiddleware,
			})
			return err
		},
		func() error {
			_, err := ListModels(context.Background(), Config{
				Provider:       ProviderOpenAI,
				APIKey:         "token",
				BaseURL:        server.URL,
				HTTPMiddleware: nilMiddleware,
			})
			return err
		},
	}
	for i, operation := range operations {
		err := operation()
		if err == nil || err.Error() != "http middleware returned nil doer" {
			t.Fatalf("case %d unexpected error: %v", i, err)
		}
	}
}

func TestHTTPMiddlewareWaitIsExcludedFromConfigTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"content": "done"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "test-model",
		Timeout:  25 * time.Millisecond,
		HTTPMiddleware: func(next HTTPDoer) HTTPDoer {
			return httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				time.Sleep(75 * time.Millisecond)
				return next.Do(req)
			})
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	started := time.Now()
	if _, err := client.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 70*time.Millisecond {
		t.Fatalf("expected middleware wait, elapsed=%v", elapsed)
	}
}

func TestHTTPMiddlewarePreservesConfigTimeoutForRealRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(75 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"content": "done"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "test-model",
		Timeout:  20 * time.Millisecond,
		HTTPMiddleware: func(next HTTPDoer) HTTPDoer {
			return httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				return next.Do(req)
			})
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if _, err := client.Generate(context.Background(), ModelRequest{}); err == nil {
		t.Fatal("expected Config.Timeout error")
	}
}

func TestHTTPMiddlewareWaitRespectsCallerContext(t *testing.T) {
	var serverCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "test-model",
		HTTPMiddleware: func(next HTTPDoer) HTTPDoer {
			return httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(time.Second):
					return next.Do(req)
				}
			})
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.Generate(ctx, ModelRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %v", err)
	}
	if serverCalls.Load() != 0 {
		t.Fatalf("expected no provider request, got %d", serverCalls.Load())
	}
}

type closeCountingBody struct {
	io.ReadCloser
	closes *atomic.Int32
}

func (b *closeCountingBody) Close() error {
	b.closes.Add(1)
	return b.ReadCloser.Close()
}

func TestHTTPMiddlewareWrapsStreamingRetriesAndBodies(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_1","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	var doerCalls atomic.Int32
	var closes atomic.Int32
	client, err := NewClient(Config{
		Provider:       ProviderOpenAICompatible,
		BaseURL:        server.URL,
		Model:          "test-model",
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		HTTPMiddleware: func(next HTTPDoer) HTTPDoer {
			return httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				doerCalls.Add(1)
				resp, err := next.Do(req)
				if resp != nil && resp.Body != nil {
					resp.Body = &closeCountingBody{ReadCloser: resp.Body, closes: &closes}
				}
				return resp, err
			})
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	var text strings.Builder
	err = client.GenerateStream(context.Background(), ModelRequest{}, func(event StreamEvent) error {
		if event.Type == StreamEventMessageDelta {
			text.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream returned error: %v", err)
	}
	if text.String() != "ok" || doerCalls.Load() != 2 || closes.Load() != 2 {
		t.Fatalf("text=%q calls=%d closes=%d", text.String(), doerCalls.Load(), closes.Load())
	}
}
