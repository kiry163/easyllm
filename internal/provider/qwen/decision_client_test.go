package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kiry163/easyllm/internal/model"
)

type decisionTestState struct {
	TicketID string `json:"ticket_id"`
	Content  string `json:"content"`
}

func TestDecisionClientMapsRequestAndParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/systemone" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("unexpected content type: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "decision-model-preview" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		state, ok := body["state"].(map[string]any)
		if !ok || state["ticket_id"] != "T-1001" || state["content"] != "paid but not delivered" {
			t.Fatalf("unexpected state: %#v", body["state"])
		}
		questions, ok := body["questions"].(map[string]any)
		if !ok || len(questions) != 3 {
			t.Fatalf("unexpected questions: %#v", body["questions"])
		}

		department, ok := questions["department"].(map[string]any)
		if !ok || department["type"] != "choice" {
			t.Fatalf("unexpected choice question: %#v", questions["department"])
		}
		if _, ok := department["instructions"]; ok {
			t.Fatalf("empty instructions must be omitted: %#v", department)
		}
		departmentCriteria, ok := department["criteria"].(map[string]any)
		if !ok || len(departmentCriteria) != 2 {
			t.Fatalf("unexpected choice criteria: %#v", department["criteria"])
		}
		if departmentCriteria["billing"] != "billing, refunds and invoices" {
			t.Fatalf("unexpected choice criteria: %#v", departmentCriteria)
		}

		escalate, ok := questions["escalate"].(map[string]any)
		if !ok || escalate["type"] != "noul" {
			t.Fatalf("unexpected bool question: %#v", questions["escalate"])
		}
		if escalate["instructions"] != "notify the on-call engineer?" {
			t.Fatalf("unexpected bool instructions: %#v", escalate["instructions"])
		}
		escalateCriteria, ok := escalate["criteria"].(map[string]any)
		if !ok || len(escalateCriteria) != 1 || escalateCriteria["true"] != "escalate now" {
			t.Fatalf("unexpected bool criteria: %#v", escalate["criteria"])
		}

		severity, ok := questions["severity"].(map[string]any)
		if !ok || severity["type"] != "score" {
			t.Fatalf("unexpected score question: %#v", questions["severity"])
		}
		severityCriteria, ok := severity["criteria"].([]any)
		if !ok || len(severityCriteria) != 4 || severityCriteria[0] != "minor" || severityCriteria[3] != "severe" {
			t.Fatalf("unexpected score criteria: %#v", severity["criteria"])
		}

		if body["custom"] != "request" {
			t.Fatalf("request options did not override extra body: %#v", body["custom"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":      "decision-model-preview",
			"request_id": "7b986c65-b223-9341-b5f0-b988e27ecaac",
			"answers": map[string]any{
				"department": map[string]any{
					"type":          "choice",
					"choice":        "billing",
					"confidence":    0.88,
					"probabilities": map[string]any{"billing": 0.94, "technical": 0.06},
				},
				"escalate": map[string]any{"type": "noul", "noul": 0.99},
				"severity": map[string]any{
					"type":          "score",
					"score":         2.25,
					"confidence":    0.91,
					"legend":        map[string]any{"0": "minor", "1": "partial", "2": "core down", "3": "severe"},
					"probabilities": map[string]any{"0": 0.0, "1": 0.01, "2": 0.73, "3": 0.26},
				},
			},
			"usage":      map[string]any{"input_tokens": 125},
			"latency_ms": 52.9,
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
		WithExtraBody(map[string]any{"custom": "config"}),
	).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

	resp, err := client.Decide(context.Background(), model.DecisionRequest{
		State: decisionTestState{TicketID: "T-1001", Content: "paid but not delivered"},
		Choice: map[string]model.ChoiceQuestion{
			"department": {Criteria: map[string]string{
				"billing":   "billing, refunds and invoices",
				"technical": "product defects and integrations",
			}},
		},
		Bool: map[string]model.BoolQuestion{
			"escalate": {Instructions: "notify the on-call engineer?", True: "escalate now"},
		},
		Score: map[string]model.ScoreQuestion{
			"severity": {Instructions: "how severe is it?", Criteria: []string{"minor", "partial", "core down", "severe"}},
		},
		Options: map[string]any{"custom": "request"},
	})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if resp.Choice["department"].Choice != "billing" || resp.Choice["department"].Confidence != 0.88 {
		t.Fatalf("unexpected choice answer: %#v", resp.Choice["department"])
	}
	if resp.Choice["department"].Probabilities["billing"] != 0.94 {
		t.Fatalf("unexpected choice probabilities: %#v", resp.Choice["department"].Probabilities)
	}
	if resp.Bool["escalate"].YesProbability != 0.99 {
		t.Fatalf("unexpected bool answer: %#v", resp.Bool["escalate"])
	}
	if resp.Score["severity"].Score != 2.25 || resp.Score["severity"].Confidence != 0.91 {
		t.Fatalf("unexpected score answer: %#v", resp.Score["severity"])
	}
	if resp.Score["severity"].Legend["2"] != "core down" {
		t.Fatalf("unexpected score legend: %#v", resp.Score["severity"].Legend)
	}
	if resp.Score["severity"].Probabilities["2"] != 0.73 {
		t.Fatalf("unexpected score probabilities: %#v", resp.Score["severity"].Probabilities)
	}
	if resp.Model != "decision-model-preview" || resp.RequestID != "7b986c65-b223-9341-b5f0-b988e27ecaac" {
		t.Fatalf("unexpected response envelope: %#v", resp)
	}
	if resp.Usage.InputTokens != 125 || resp.LatencyMS != 52.9 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
	if resp.Provider != "qwen" || resp.Raw == nil {
		t.Fatalf("unexpected provider metadata: %#v", resp)
	}
}

func TestDecisionClientOmitsEmptyQuestionFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		questions, ok := body["questions"].(map[string]any)
		if !ok || len(questions) != 2 {
			t.Fatalf("unexpected questions: %#v", body["questions"])
		}
		blank, ok := questions["blank"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected bool question: %#v", questions["blank"])
		}
		if _, ok := blank["instructions"]; ok {
			t.Fatalf("whitespace instructions must be omitted: %#v", blank)
		}
		if _, ok := blank["criteria"]; ok {
			t.Fatalf("empty bool criteria must be omitted: %#v", blank)
		}
		billing, ok := questions["billing"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected bool question: %#v", questions["billing"])
		}
		criteria, ok := billing["criteria"].(map[string]any)
		if !ok || len(criteria) != 1 || criteria["false"] != "no refund request" {
			t.Fatalf("unexpected bool criteria: %#v", billing["criteria"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{
				"blank":   map[string]any{"type": "noul", "noul": 0.1},
				"billing": map[string]any{"type": "noul", "noul": 0.2},
			},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

	resp, err := client.Decide(context.Background(), model.DecisionRequest{
		State: "is this comment asking for a refund?",
		Bool: map[string]model.BoolQuestion{
			"blank":   {Instructions: "   "},
			"billing": {False: "no refund request"},
		},
	})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if resp.Bool["blank"].YesProbability != 0.1 || resp.Bool["billing"].YesProbability != 0.2 {
		t.Fatalf("unexpected bool answers: %#v", resp.Bool)
	}
	if resp.Model != "decision-model-preview" {
		t.Fatalf("expected the client model to be echoed back, got %q", resp.Model)
	}
}

func TestDecisionClientValidatesRequestBeforeSending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	validChoice := func() map[string]model.ChoiceQuestion {
		return map[string]model.ChoiceQuestion{
			"department": {Criteria: map[string]string{"billing": "billing", "technical": "technical"}},
		}
	}
	tooMany := map[string]model.ChoiceQuestion{}
	for i := range 17 {
		tooMany[fmt.Sprintf("q%d", i)] = model.ChoiceQuestion{Criteria: map[string]string{"only": "only"}}
	}
	tooManyOptions := map[string]string{}
	for i := range 256 {
		tooManyOptions[fmt.Sprintf("option-%d", i)] = "an option"
	}
	tooManyLevels := make([]string, 256)
	for i := range tooManyLevels {
		tooManyLevels[i] = fmt.Sprintf("level-%d", i)
	}

	cases := []struct {
		name string
		req  model.DecisionRequest
		want string
	}{
		{
			name: "state is required",
			req:  model.DecisionRequest{Choice: validChoice()},
			want: "state is required",
		},
		{
			name: "blank string state",
			req:  model.DecisionRequest{State: "   ", Choice: validChoice()},
			want: "state is required",
		},
		{
			name: "empty map state",
			req:  model.DecisionRequest{State: map[string]any{}, Choice: validChoice()},
			want: "state is required",
		},
		{
			name: "no questions",
			req:  model.DecisionRequest{State: "ticket"},
			want: "at least one question is required",
		},
		{
			name: "too many questions",
			req:  model.DecisionRequest{State: "ticket", Choice: tooMany},
			want: "question count 17 exceeds the limit of 16",
		},
		{
			name: "too many choice options",
			req: model.DecisionRequest{State: "ticket", Choice: map[string]model.ChoiceQuestion{
				"department": {Criteria: tooManyOptions},
			}},
			want: `choice question "department": option count 256 exceeds the limit of 255`,
		},
		{
			name: "too many score levels",
			req: model.DecisionRequest{State: "ticket", Score: map[string]model.ScoreQuestion{
				"severity": {Criteria: tooManyLevels},
			}},
			want: `score question "severity": level count 256 exceeds the limit of 255`,
		},
		{
			name: "empty question id",
			req: model.DecisionRequest{State: "ticket", Choice: map[string]model.ChoiceQuestion{
				"": {Criteria: map[string]string{"only": "only"}},
			}},
			want: "question id is required",
		},
		{
			name: "duplicate question id",
			req: model.DecisionRequest{
				State:  "ticket",
				Choice: validChoice(),
				Score: map[string]model.ScoreQuestion{
					"department": {Criteria: []string{"low", "high"}},
				},
			},
			want: `duplicate question id "department"`,
		},
		{
			name: "choice criteria required",
			req: model.DecisionRequest{State: "ticket", Choice: map[string]model.ChoiceQuestion{
				"department": {},
			}},
			want: `choice question "department": criteria is required`,
		},
		{
			name: "choice option name required",
			req: model.DecisionRequest{State: "ticket", Choice: map[string]model.ChoiceQuestion{
				"department": {Criteria: map[string]string{" ": "blank option"}},
			}},
			want: `choice question "department": criteria option name is required`,
		},
		{
			name: "score levels required",
			req: model.DecisionRequest{State: "ticket", Score: map[string]model.ScoreQuestion{
				"severity": {Criteria: []string{"only"}},
			}},
			want: `score question "severity": criteria requires at least 2 levels`,
		},
		{
			name: "score blank level",
			req: model.DecisionRequest{State: "ticket", Score: map[string]model.ScoreQuestion{
				"severity": {Criteria: []string{"low", "  "}},
			}},
			want: `score question "severity": level 1 description is required`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewProvider(
				WithAPIKey("token"),
				WithBaseURL(server.URL),
			).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

			_, err := client.Decide(context.Background(), tc.req)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecisionClientAcceptsScalarState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["state"] != float64(42) {
			t.Fatalf("unexpected state: %#v", body["state"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{"refund": map[string]any{"type": "noul", "noul": 0.82}},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

	resp, err := client.Decide(context.Background(), model.DecisionRequest{
		State: 42,
		Bool:  map[string]model.BoolQuestion{"refund": {}},
	})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if resp.Bool["refund"].YesProbability != 0.82 {
		t.Fatalf("unexpected bool answer: %#v", resp.Bool["refund"])
	}
}

func TestDecisionClientRequestModelOverridesClientModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "decision-model-experimental" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{"refund": map[string]any{"type": "noul", "noul": 0.5}},
		})
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

	if _, err := client.Decide(context.Background(), model.DecisionRequest{
		Model: "decision-model-experimental",
		State: "is this comment asking for a refund?",
		Bool:  map[string]model.BoolQuestion{"refund": {}},
	}); err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
}

func TestDecisionClientRequiresModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewProvider(
		WithAPIKey("token"),
		WithBaseURL(server.URL),
	).DecisionClient(DecisionClientConfig{})

	_, err := client.Decide(context.Background(), model.DecisionRequest{
		State: "ticket",
		Bool:  map[string]model.BoolQuestion{"refund": {}},
	})
	if err == nil || err.Error() != "model is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecisionClientRejectsUnexpectedAnswers(t *testing.T) {
	req := model.DecisionRequest{
		State: "ticket",
		Choice: map[string]model.ChoiceQuestion{
			"department": {Criteria: map[string]string{"billing": "billing", "technical": "technical"}},
		},
		Score: map[string]model.ScoreQuestion{
			"severity": {Criteria: []string{"low", "high"}},
		},
	}

	cases := []struct {
		name    string
		answers map[string]any
		want    string
	}{
		{
			name: "missing answer",
			answers: map[string]any{
				"department": map[string]any{"type": "choice", "choice": "billing"},
			},
			want: `answer "severity" is missing from the response`,
		},
		{
			name: "unexpected answer",
			answers: map[string]any{
				"department": map[string]any{"type": "choice", "choice": "billing"},
				"severity":   map[string]any{"type": "score", "score": 1.0},
				"extra":      map[string]any{"type": "noul", "noul": 0.5},
			},
			want: `unexpected answer "extra" in the response`,
		},
		{
			name: "mismatched answer type",
			answers: map[string]any{
				"department": map[string]any{"type": "choice", "choice": "billing"},
				"severity":   map[string]any{"type": "noul", "noul": 0.5},
			},
			want: `answer "severity": expected type "score", got "noul"`,
		},
		{
			name: "choice outside criteria",
			answers: map[string]any{
				"department": map[string]any{"type": "choice", "choice": "logistics"},
				"severity":   map[string]any{"type": "score", "score": 1.0},
			},
			want: `answer "department": choice "logistics" is not one of the criteria options`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"model":      "decision-model-preview",
					"request_id": "req-1",
					"answers":    tc.answers,
				})
			}))
			defer server.Close()

			client := NewProvider(
				WithAPIKey("token"),
				WithBaseURL(server.URL),
			).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

			_, err := client.Decide(context.Background(), req)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecisionClientRejectsAnswersWithoutValueField(t *testing.T) {
	req := model.DecisionRequest{
		State: "ticket",
		Choice: map[string]model.ChoiceQuestion{
			"department": {Criteria: map[string]string{"billing": "billing"}},
		},
		Bool: map[string]model.BoolQuestion{"escalate": {}},
		Score: map[string]model.ScoreQuestion{
			"severity": {Criteria: []string{"low", "high"}},
		},
	}

	cases := []struct {
		name    string
		answers map[string]any
		want    string
	}{
		{
			name: "choice answer without value",
			answers: map[string]any{
				"department": map[string]any{"type": "choice"},
				"escalate":   map[string]any{"type": "noul", "noul": 0.5},
				"severity":   map[string]any{"type": "score", "score": 1},
			},
			want: `answer "department": choice is required`,
		},
		{
			name: "bool answer without value",
			answers: map[string]any{
				"department": map[string]any{"type": "choice", "choice": "billing"},
				"escalate":   map[string]any{"type": "noul"},
				"severity":   map[string]any{"type": "score", "score": 1},
			},
			want: `answer "escalate": noul is required`,
		},
		{
			name: "score answer without value",
			answers: map[string]any{
				"department": map[string]any{"type": "choice", "choice": "billing"},
				"escalate":   map[string]any{"type": "noul", "noul": 0.5},
				"severity":   map[string]any{"type": "score"},
			},
			want: `answer "severity": score is required`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"model":      "decision-model-preview",
					"request_id": "req-1",
					"answers":    tc.answers,
				})
			}))
			defer server.Close()

			client := NewProvider(
				WithAPIKey("token"),
				WithBaseURL(server.URL),
			).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

			_, err := client.Decide(context.Background(), req)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecisionClientSurfacesTransportAndDecodeErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantHas string
	}{
		{
			name:    "http error status",
			status:  http.StatusBadRequest,
			body:    `{"message":"invalid api key"}`,
			wantHas: `request failed: status=400 body={"message":"invalid api key"}`,
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    "{",
			wantHas: "decode decision response: ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewProvider(
				WithAPIKey("token"),
				WithBaseURL(server.URL),
			).DecisionClient(DecisionClientConfig{Model: "decision-model-preview"})

			_, err := client.Decide(context.Background(), model.DecisionRequest{
				State: "ticket",
				Bool:  map[string]model.BoolQuestion{"refund": {}},
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantHas != "" && !strings.Contains(err.Error(), tc.wantHas) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
