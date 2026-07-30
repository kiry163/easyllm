package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kiry163/easyllm/internal/model"
)

func TestTypedToolChoiceOverridesExtraBodyForEveryTransport(t *testing.T) {
	tests := []struct {
		name      string
		transport Transport
		stream    bool
	}{
		{name: "chat", transport: TransportChat},
		{name: "chat stream", transport: TransportChat, stream: true},
		{name: "responses", transport: TransportResponses},
		{name: "responses stream", transport: TransportResponses, stream: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				if tt.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					if tt.transport == TransportResponses {
						_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[]}}\n\n")
					} else {
						_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					}
					return
				}
				if tt.transport == TransportResponses {
					_, _ = fmt.Fprint(w, `{"id":"resp_1","output":[]}`)
				} else {
					_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()

			var client *Client
			opts := []Option{WithExtraBody(map[string]any{"tool_choice": "required"})}
			if tt.transport == TransportResponses {
				client = NewResponsesClient("token", server.URL, opts...)
			} else {
				client = NewChatClient("token", server.URL, opts...)
			}
			choice := model.NewToolChoice(model.ToolChoiceModeNamed, "get_weather")
			req := model.ModelRequest{
				Tools:      []model.ToolDefinition{{Name: "get_weather", Parameters: map[string]any{"type": "object"}}},
				ToolChoice: &choice,
			}
			if tt.stream {
				if err := client.GenerateStream(context.Background(), req, nil); err != nil {
					t.Fatalf("GenerateStream returned error: %v", err)
				}
			} else if _, err := client.Generate(context.Background(), req); err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}

			rendered, ok := body["tool_choice"].(map[string]any)
			if !ok || rendered["type"] != "function" {
				t.Fatalf("unexpected tool choice: %#v", body["tool_choice"])
			}
			if tt.transport == TransportResponses {
				if rendered["name"] != "get_weather" {
					t.Fatalf("unexpected responses tool choice: %#v", rendered)
				}
				if _, nested := rendered["function"]; nested {
					t.Fatalf("responses tool choice must be flat: %#v", rendered)
				}
				return
			}
			function, ok := rendered["function"].(map[string]any)
			if !ok || function["name"] != "get_weather" {
				t.Fatalf("unexpected chat tool choice: %#v", rendered)
			}
		})
	}
}

func TestToolChoiceStringModes(t *testing.T) {
	for _, mode := range []model.ToolChoiceMode{
		model.ToolChoiceModeAuto,
		model.ToolChoiceModeRequired,
		model.ToolChoiceModeNone,
	} {
		choice := model.NewToolChoice(mode, "")
		if got := openAIChatToolChoice(choice); got != string(mode) {
			t.Fatalf("chat mode %q rendered as %#v", mode, got)
		}
		if got := openAIResponsesToolChoice(choice); got != string(mode) {
			t.Fatalf("responses mode %q rendered as %#v", mode, got)
		}
	}
}

func TestExtraBodyToolChoiceIsPreservedWithoutTypedChoice(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := NewChatClient("token", server.URL, WithExtraBody(map[string]any{"tool_choice": "required"}))
	if _, err := client.Generate(context.Background(), model.ModelRequest{}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if body["tool_choice"] != "required" {
		t.Fatalf("unexpected tool choice: %#v", body["tool_choice"])
	}
}
