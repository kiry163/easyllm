package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

func TestChatClientRetriesAndNormalizesToolCalls(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := ioReadAll(r)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if attempts == 0 && !bytes.Contains(body, []byte(`"messages"`)) {
			t.Fatalf("expected messages in request body: %s", string(body))
		}
		if !bytes.Contains(body, []byte(`"hello"`)) {
			t.Fatalf("expected prompt in request body: %s", string(body))
		}
		if attempts == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "tool call",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "submit",
									"arguments": "\"{\\\"value\\\":\\\"ok\\\"}\"",
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     3,
				"completion_tokens": 5,
				"total_tokens":      8,
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL), WithRetry(RetryConfig{MaxAttempts: 2}))
	client := openaiProvider.ChatModel(ChatModelConfig{})
	resp, err := client.Generate(context.Background(), provider.ModelRequest{
		Model: "gpt-test",
		Input: []provider.InputItem{
			provider.UserMessageItem{
				Content: []provider.TextPart{{Text: "hello"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %q", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
	call, ok := resp.Output[0].(provider.ToolCallOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", resp.Output[0])
	}
	if call.Name != "submit" || call.Arguments["value"] != "ok" || !call.Repaired {
		t.Fatalf("unexpected tool call output: %+v", call)
	}
}

func TestResponsesClientNormalizesAssistantMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := ioReadAll(r)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !bytes.Contains(body, []byte(`"input"`)) || !bytes.Contains(body, []byte(`"hello"`)) {
			t.Fatalf("expected input payload in request body: %s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_123",
			"output": []map[string]any{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "done",
						},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  7,
				"output_tokens": 2,
				"total_tokens":  9,
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL))
	client := openaiProvider.ResponsesModel(ResponsesModelConfig{})
	resp, err := client.Generate(context.Background(), provider.ModelRequest{
		Model: "gpt-test",
		Input: []provider.InputItem{
			provider.UserMessageItem{
				Content: []provider.TextPart{{Text: "hello"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.ResponseID != "resp_123" {
		t.Fatalf("unexpected response id: %q", resp.ResponseID)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
	msg, ok := resp.Output[0].(provider.MessageOutput)
	if !ok {
		t.Fatalf("unexpected output type: %T", resp.Output[0])
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "done" {
		t.Fatalf("unexpected message output: %+v", msg)
	}
}

func TestChatClientUsesDefaultModelAndExtraBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAll(r)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !bytes.Contains(body, []byte(`"model":"default-model"`)) {
			t.Fatalf("expected default model in request body: %s", string(body))
		}
		if !bytes.Contains(body, []byte(`"enable_thinking":false`)) {
			t.Fatalf("expected extra body in request: %s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithExtraBody(map[string]any{"enable_thinking": false}),
	)
	client := openaiProvider.ChatModel(ChatModelConfig{Model: "default-model"})
	resp, err := client.Generate(context.Background(), provider.ModelRequest{
		Input: []provider.InputItem{
			provider.UserMessageItem{Content: []provider.TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("unexpected output count: %d", len(resp.Output))
	}
}

func TestChatClientUsesDefaultSamplingOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAll(r)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !bytes.Contains(body, []byte(`"temperature":0.7`)) {
			t.Fatalf("expected temperature in request body: %s", string(body))
		}
		if !bytes.Contains(body, []byte(`"top_p":0.8`)) {
			t.Fatalf("expected top_p in request body: %s", string(body))
		}
		if !bytes.Contains(body, []byte(`"max_tokens":256`)) {
			t.Fatalf("expected max_tokens in request body: %s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL))
	temperature := 0.7
	topP := 0.8
	maxTokens := 256
	client := openaiProvider.ChatModel(ChatModelConfig{
		Model:       "default-model",
		Temperature: &temperature,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
	})
	_, err := client.Generate(context.Background(), provider.ModelRequest{
		Input: []provider.InputItem{
			provider.UserMessageItem{Content: []provider.TextPart{{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
}

func TestProviderSupportsTimeoutOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithTimeout(10*time.Millisecond),
	)
	client := openaiProvider.ChatModel(ChatModelConfig{Model: "gpt-test"})
	_, err := client.Generate(context.Background(), provider.ModelRequest{
		Input: []provider.InputItem{
			provider.UserMessageItem{Content: []provider.TextPart{{Text: "hello"}}},
		},
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestChatClientRendersToolsAsNestedFunctionDefinitions(t *testing.T) {
	strict := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools: %#v", body["tools"])
		}
		rendered, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected tool shape: %#v", tools[0])
		}
		if rendered["type"] != "function" {
			t.Fatalf("unexpected tool type: %#v", rendered["type"])
		}
		if _, ok := rendered["name"]; ok {
			t.Fatalf("chat tool should not use flat name field: %#v", rendered)
		}
		fn, ok := rendered["function"].(map[string]any)
		if !ok {
			t.Fatalf("chat tool should nest function definition: %#v", rendered)
		}
		if fn["name"] != "get_weather" || fn["description"] != "Get weather" {
			t.Fatalf("unexpected function definition: %#v", fn)
		}
		if fn["strict"] != true {
			t.Fatalf("expected strict=true in nested function definition, got %#v", fn["strict"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL))
	client := openaiProvider.ChatModel(ChatModelConfig{Model: "gpt-test"})
	_, err := client.Generate(context.Background(), provider.ModelRequest{
		Input: []provider.InputItem{
			provider.UserMessageItem{Content: []provider.TextPart{{Text: "hello"}}},
		},
		Tools: []tool.Definition{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]any{"type": "object"},
				Strict:      &strict,
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
}

func TestResponsesClientRendersToolsAsFlatFunctionDefinitions(t *testing.T) {
	strict := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools: %#v", body["tools"])
		}
		rendered, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected tool shape: %#v", tools[0])
		}
		if rendered["type"] != "function" {
			t.Fatalf("unexpected tool type: %#v", rendered["type"])
		}
		if _, ok := rendered["function"]; ok {
			t.Fatalf("responses tool should not nest function definition: %#v", rendered)
		}
		if rendered["name"] != "get_weather" || rendered["description"] != "Get weather" {
			t.Fatalf("unexpected function definition: %#v", rendered)
		}
		if rendered["strict"] != true {
			t.Fatalf("expected strict=true in flat function definition, got %#v", rendered["strict"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_123",
			"output": []map[string]any{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "done",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	openaiProvider := NewProvider(WithAPIKey("token"), WithBaseURL(server.URL))
	client := openaiProvider.ResponsesModel(ResponsesModelConfig{Model: "gpt-test"})
	_, err := client.Generate(context.Background(), provider.ModelRequest{
		Input: []provider.InputItem{
			provider.UserMessageItem{Content: []provider.TextPart{{Text: "hello"}}},
		},
		Tools: []tool.Definition{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]any{"type": "object"},
				Strict:      &strict,
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
