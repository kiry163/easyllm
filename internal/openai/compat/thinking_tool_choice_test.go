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

// writeCompatFakeResponse answers a request with a minimal valid payload for
// the given transport/streaming combination so Generate/GenerateStream return
// successfully and the test can inspect the request body instead.
func writeCompatFakeResponse(w http.ResponseWriter, transport Transport, stream bool) {
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		if transport == TransportResponses {
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[]}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		return
	}
	if transport == TransportResponses {
		_, _ = fmt.Fprint(w, `{"id":"resp_1","output":[]}`)
		return
	}
	_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
}

func TestThinkingModeDowngradesForcedToolChoice(t *testing.T) {
	type scenario struct {
		name      string
		extraBody map[string]any // merged into the client extra body
		choice    *model.ToolChoice
		options   map[string]any // request-level options (also merged into the body)
		// wantToolChoice is the expected wire value. wantAbsent expects the key
		// to be removed from the request body entirely.
		wantToolChoice any
		wantAbsent     bool
	}
	named := model.NewToolChoice(model.ToolChoiceModeNamed, "get_weather")
	required := model.NewToolChoice(model.ToolChoiceModeRequired, "")
	auto := model.NewToolChoice(model.ToolChoiceModeAuto, "")
	none := model.NewToolChoice(model.ToolChoiceModeNone, "")

	scenarios := []scenario{
		{
			name:       "required + qwen thinking is downgraded",
			extraBody:  map[string]any{"enable_thinking": true},
			choice:     &required,
			wantAbsent: true,
		},
		{
			name:       "named tool + qwen thinking is downgraded",
			extraBody:  map[string]any{"enable_thinking": true},
			choice:     &named,
			wantAbsent: true,
		},
		{
			name:       "required + deepseek thinking is downgraded",
			extraBody:  map[string]any{"thinking": map[string]any{"type": "enabled"}},
			choice:     &required,
			wantAbsent: true,
		},
		{
			name:           "required + thinking disabled is preserved",
			extraBody:      map[string]any{"enable_thinking": false},
			choice:         &required,
			wantToolChoice: "required",
		},
		{
			name:           "required without thinking flag is preserved",
			choice:         &required,
			wantToolChoice: "required",
		},
		{
			name:           "auto + thinking stays auto",
			extraBody:      map[string]any{"enable_thinking": true},
			choice:         &auto,
			wantToolChoice: "auto",
		},
		{
			name:           "none + thinking stays none",
			extraBody:      map[string]any{"enable_thinking": true},
			choice:         &none,
			wantToolChoice: "none",
		},
		{
			name: "extra-body forced choice + thinking is downgraded",
			extraBody: map[string]any{
				"enable_thinking": true,
				"tool_choice":     "required",
			},
			wantAbsent: true,
		},
		{
			name:       "thinking via request options also downgrades",
			choice:     &required,
			options:    map[string]any{"enable_thinking": true},
			wantAbsent: true,
		},
		{
			name:           "extra-body forced choice without thinking is preserved",
			extraBody:      map[string]any{"tool_choice": "required"},
			wantToolChoice: "required",
		},
	}

	transports := []struct {
		name      string
		transport Transport
		stream    bool
	}{
		{name: "chat", transport: TransportChat},
		{name: "chat stream", transport: TransportChat, stream: true},
		{name: "responses", transport: TransportResponses},
		{name: "responses stream", transport: TransportResponses, stream: true},
	}

	for _, tr := range transports {
		t.Run(tr.name, func(t *testing.T) {
			for _, sc := range scenarios {
				t.Run(sc.name, func(t *testing.T) {
					var body map[string]any
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Fatalf("decode request body: %v", err)
						}
						writeCompatFakeResponse(w, tr.transport, tr.stream)
					}))
					defer server.Close()

					opts := []Option{WithExtraBody(sc.extraBody)}
					var client *Client
					if tr.transport == TransportResponses {
						client = NewResponsesClient("token", server.URL, opts...)
					} else {
						client = NewChatClient("token", server.URL, opts...)
					}
					req := model.ModelRequest{
						Tools: []model.ToolDefinition{
							{Name: "get_weather", Parameters: map[string]any{"type": "object"}},
						},
						ToolChoice: sc.choice,
						Options:    sc.options,
					}
					if tr.stream {
						if err := client.GenerateStream(context.Background(), req, nil); err != nil {
							t.Fatalf("GenerateStream returned error: %v", err)
						}
					} else if _, err := client.Generate(context.Background(), req); err != nil {
						t.Fatalf("Generate returned error: %v", err)
					}

					got, ok := body["tool_choice"]
					switch {
					case sc.wantAbsent:
						if ok {
							t.Fatalf("expected tool_choice to be removed in thinking mode, got %#v", got)
						}
					case !ok:
						t.Fatalf("expected tool_choice %#v on the wire, got absent", sc.wantToolChoice)
					default:
						if got != sc.wantToolChoice {
							t.Fatalf("unexpected tool_choice: got %#v want %#v", got, sc.wantToolChoice)
						}
					}
					if _, ok := body["tools"]; !ok {
						t.Fatalf("expected tools to remain on the wire, got %#v", body["tools"])
					}
				})
			}
		})
	}
}

func TestThinkingExplicitlyEnabledDetection(t *testing.T) {
	tests := []struct {
		name      string
		transport Transport
		body      map[string]any
		want      bool
	}{
		{name: "qwen thinking on", transport: TransportChat, body: map[string]any{"enable_thinking": true}, want: true},
		{name: "qwen thinking off", transport: TransportChat, body: map[string]any{"enable_thinking": false}},
		{name: "deepseek thinking on", transport: TransportChat, body: map[string]any{"thinking": map[string]any{"type": "enabled"}}, want: true},
		{name: "deepseek thinking off", transport: TransportChat, body: map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		{name: "no thinking flags", transport: TransportChat, body: map[string]any{"model": "x"}},
		{name: "non-bool enable_thinking", transport: TransportChat, body: map[string]any{"enable_thinking": "true"}},
		{name: "responses reasoning high", transport: TransportResponses, body: map[string]any{"reasoning": map[string]any{"effort": "high"}}, want: true},
		{name: "responses reasoning max", transport: TransportResponses, body: map[string]any{"reasoning": map[string]any{"effort": "max"}}, want: true},
		{name: "responses reasoning none", transport: TransportResponses, body: map[string]any{"reasoning": map[string]any{"effort": "none"}}},
		{name: "responses reasoning empty effort", transport: TransportResponses, body: map[string]any{"reasoning": map[string]any{"effort": ""}}},
		{name: "responses without reasoning", transport: TransportResponses, body: map[string]any{"model": "x"}},
		{name: "chat reasoning stays unrecognized", transport: TransportChat, body: map[string]any{"reasoning": map[string]any{"effort": "high"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := thinkingExplicitlyEnabled(tt.transport, tt.body); got != tt.want {
				t.Fatalf("thinkingExplicitlyEnabled(%v, %#v) = %v, want %v", tt.transport, tt.body, got, tt.want)
			}
		})
	}
}

// TestForcedToolChoiceDowngradeIsTransportScoped pins the boundary of the
// thinking-mode tool choice shim: a reasoning.effort object means thinking on
// for the responses transport only, because chat endpoints such as OpenRouter
// accept reasoning next to a forced tool choice.
func TestForcedToolChoiceDowngradeIsTransportScoped(t *testing.T) {
	required := model.NewToolChoice(model.ToolChoiceModeRequired, "")
	for _, tt := range []struct {
		name       string
		transport  Transport
		wantAbsent bool
	}{
		{name: "responses drops the forced choice", transport: TransportResponses, wantAbsent: true},
		{name: "chat preserves the forced choice", transport: TransportChat},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				writeCompatFakeResponse(w, tt.transport, false)
			}))
			defer server.Close()

			opts := []Option{WithExtraBody(map[string]any{"reasoning": map[string]any{"effort": "high"}})}
			var client *Client
			if tt.transport == TransportResponses {
				client = NewResponsesClient("token", server.URL, opts...)
			} else {
				client = NewChatClient("token", server.URL, opts...)
			}
			req := model.ModelRequest{
				Tools:      []model.ToolDefinition{{Name: "get_weather", Parameters: map[string]any{"type": "object"}}},
				ToolChoice: &required,
			}
			if _, err := client.Generate(context.Background(), req); err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
			got, ok := body["tool_choice"]
			if ok == tt.wantAbsent {
				t.Fatalf("tool_choice presence = %v, want absent = %v (got %#v)", ok, tt.wantAbsent, got)
			}
		})
	}
}
