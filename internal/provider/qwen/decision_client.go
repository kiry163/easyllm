package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kiry163/easyllm/internal/httpjson"
	"github.com/kiry163/easyllm/internal/model"
)

const (
	decisionQuestionChoice = "choice"
	decisionQuestionBool   = "noul"
	decisionQuestionScore  = "score"
)

type DecisionClientConfig struct {
	Model string
}

// DecisionClient calls the TypeSafe System One endpoint. It answers typed
// questions in one forward pass and never generates text.
type DecisionClient struct {
	client    httpjson.Client
	extraBody map[string]any
	model     string
}

func (p *Provider) DecisionClient(config DecisionClientConfig) model.DecisionClient {
	return &DecisionClient{
		client: httpjson.Client{
			APIKey:   p.apiKey,
			BaseURL:  p.baseURL,
			HTTPDoer: p.resolvedHTTPDoer(),
			Retry: httpjson.RetryConfig{
				MaxRetries:     p.retry.MaxRetries,
				InitialBackoff: p.retry.InitialBackoff,
				MaxBackoff:     p.retry.MaxBackoff,
			},
		},
		extraBody: cloneMap(p.extraBody),
		model:     strings.TrimSpace(config.Model),
	}
}

func (c *DecisionClient) Decide(ctx context.Context, req model.DecisionRequest) (*model.DecisionResponse, error) {
	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		modelName = c.model
	}
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}

	body := cloneMap(c.extraBody)
	if body == nil {
		body = map[string]any{}
	}
	body["model"] = modelName
	body["state"] = req.State
	body["questions"] = decisionQuestionsBody(req)
	for key, value := range req.Options {
		body[key] = value
	}

	resp, err := c.client.Post(ctx, "/systemone", body)
	if err != nil {
		return nil, err
	}

	out, err := decodeDecisionResponse(resp.Body, req)
	if err != nil {
		return nil, err
	}
	if out.Model == "" {
		out.Model = modelName
	}
	out.Provider = "qwen"
	out.RetryCount = resp.RetryCount
	return out, nil
}

func decisionQuestionsBody(req model.DecisionRequest) map[string]any {
	questions := make(map[string]any, len(req.Choice)+len(req.Bool)+len(req.Score))
	for id, question := range req.Choice {
		entry := map[string]any{
			"type":     decisionQuestionChoice,
			"criteria": cloneDecisionStrings(question.Criteria),
		}
		if instructions := strings.TrimSpace(question.Instructions); instructions != "" {
			entry["instructions"] = question.Instructions
		}
		questions[id] = entry
	}
	for id, question := range req.Bool {
		entry := map[string]any{"type": decisionQuestionBool}
		if instructions := strings.TrimSpace(question.Instructions); instructions != "" {
			entry["instructions"] = question.Instructions
		}
		criteria := map[string]any{}
		if strings.TrimSpace(question.True) != "" {
			criteria["true"] = question.True
		}
		if strings.TrimSpace(question.False) != "" {
			criteria["false"] = question.False
		}
		if len(criteria) > 0 {
			entry["criteria"] = criteria
		}
		questions[id] = entry
	}
	for id, question := range req.Score {
		entry := map[string]any{
			"type":     decisionQuestionScore,
			"criteria": append([]string(nil), question.Criteria...),
		}
		if instructions := strings.TrimSpace(question.Instructions); instructions != "" {
			entry["instructions"] = question.Instructions
		}
		questions[id] = entry
	}
	return questions
}

func decodeDecisionResponse(body []byte, req model.DecisionRequest) (*model.DecisionResponse, error) {
	var raw struct {
		Model     string                     `json:"model"`
		RequestID string                     `json:"request_id"`
		Answers   map[string]json.RawMessage `json:"answers"`
		Usage     struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
		LatencyMS float64 `json:"latency_ms"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode decision response: %w", err)
	}
	var rawPayload map[string]any
	if err := json.Unmarshal(body, &rawPayload); err != nil {
		return nil, fmt.Errorf("decode decision response: %w", err)
	}

	out := &model.DecisionResponse{
		Choice:    make(map[string]model.ChoiceAnswer, len(req.Choice)),
		Bool:      make(map[string]model.BoolAnswer, len(req.Bool)),
		Score:     make(map[string]model.ScoreAnswer, len(req.Score)),
		Model:     raw.Model,
		RequestID: raw.RequestID,
		Usage:     model.Usage{InputTokens: raw.Usage.InputTokens},
		LatencyMS: raw.LatencyMS,
		Raw:       rawPayload,
	}

	for id := range raw.Answers {
		if _, ok := req.Choice[id]; ok {
			continue
		}
		if _, ok := req.Bool[id]; ok {
			continue
		}
		if _, ok := req.Score[id]; ok {
			continue
		}
		return nil, fmt.Errorf("unexpected answer %q in the response", id)
	}

	for id := range req.Choice {
		message, ok := raw.Answers[id]
		if !ok {
			return nil, fmt.Errorf("answer %q is missing from the response", id)
		}
		var answer struct {
			Choice        string             `json:"choice"`
			Confidence    float64            `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
		}
		if err := json.Unmarshal(message, &answer); err != nil {
			return nil, fmt.Errorf("decode answer %q: %w", id, err)
		}
		out.Choice[id] = model.ChoiceAnswer{
			Choice:        answer.Choice,
			Confidence:    answer.Confidence,
			Probabilities: answer.Probabilities,
		}
	}

	for id := range req.Bool {
		message, ok := raw.Answers[id]
		if !ok {
			return nil, fmt.Errorf("answer %q is missing from the response", id)
		}
		var answer struct {
			Noul float64 `json:"noul"`
		}
		if err := json.Unmarshal(message, &answer); err != nil {
			return nil, fmt.Errorf("decode answer %q: %w", id, err)
		}
		out.Bool[id] = model.BoolAnswer{YesProbability: answer.Noul}
	}

	for id := range req.Score {
		message, ok := raw.Answers[id]
		if !ok {
			return nil, fmt.Errorf("answer %q is missing from the response", id)
		}
		var answer struct {
			Score         float64            `json:"score"`
			Confidence    float64            `json:"confidence"`
			Legend        map[string]string  `json:"legend"`
			Probabilities map[string]float64 `json:"probabilities"`
		}
		if err := json.Unmarshal(message, &answer); err != nil {
			return nil, fmt.Errorf("decode answer %q: %w", id, err)
		}
		out.Score[id] = model.ScoreAnswer{
			Score:         answer.Score,
			Confidence:    answer.Confidence,
			Legend:        answer.Legend,
			Probabilities: answer.Probabilities,
		}
	}

	return out, nil
}

func cloneDecisionStrings(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
