package model

import (
	"encoding/json"
	"slices"
	"sort"
	"testing"
)

func TestDecisionResponseMarshalsSnakeCaseKeys(t *testing.T) {
	resp := DecisionResponse{
		Choice: map[string]ChoiceAnswer{
			"department": {Choice: "billing", Confidence: 0.88, Probabilities: map[string]float64{"billing": 0.94}},
		},
		Bool: map[string]BoolAnswer{"escalate": {YesProbability: 0.99}},
		Score: map[string]ScoreAnswer{
			"severity": {
				Score:         2.25,
				Confidence:    0.91,
				Legend:        map[string]string{"2": "core down"},
				Probabilities: map[string]float64{"2": 0.73},
			},
		},
		Model:      "decision-model-preview",
		RequestID:  "req-1",
		Usage:      Usage{InputTokens: 125},
		LatencyMS:  52.9,
		Provider:   "qwen",
		RetryCount: 1,
		Raw:        map[string]any{"answers": map[string]any{}},
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal decision response: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode decision response: %v", err)
	}

	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"bool", "choice", "latency_ms", "model", "provider", "raw", "request_id", "retry_count", "score", "usage"}
	if !slices.Equal(keys, want) {
		t.Fatalf("unexpected marshaled keys: got %v, want %v", keys, want)
	}

	var usage struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(decoded["usage"], &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if usage.InputTokens != 125 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	var choices map[string]struct {
		Choice     string  `json:"choice"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal(decoded["choice"], &choices); err != nil {
		t.Fatalf("decode choice answers: %v", err)
	}
	if choices["department"].Choice != "billing" || choices["department"].Confidence != 0.88 {
		t.Fatalf("unexpected choice answers: %#v", choices)
	}

	var bools map[string]struct {
		Noul float64 `json:"noul"`
	}
	if err := json.Unmarshal(decoded["bool"], &bools); err != nil {
		t.Fatalf("decode bool answers: %v", err)
	}
	if bools["escalate"].Noul != 0.99 {
		t.Fatalf("unexpected bool answers: %#v", bools)
	}
}
