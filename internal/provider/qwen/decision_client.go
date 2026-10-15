package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/kiry163/easyllm/internal/httpjson"
	"github.com/kiry163/easyllm/internal/model"
)

const (
	decisionQuestionChoice = "choice"
	decisionQuestionBool   = "noul"
	decisionQuestionScore  = "score"

	decisionMaxQuestions     = 16
	decisionMaxChoiceOptions = 255
	decisionMinScoreLevels   = 2
	decisionMaxScoreLevels   = 255
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

	if err := validateDecisionRequest(req); err != nil {
		return nil, err
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
		if err := checkDecisionAnswerType(id, decisionQuestionChoice, message); err != nil {
			return nil, err
		}
		var answer struct {
			Choice        string             `json:"choice"`
			Confidence    float64            `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
		}
		if err := json.Unmarshal(message, &answer); err != nil {
			return nil, fmt.Errorf("decode answer %q: %w", id, err)
		}
		if _, ok := req.Choice[id].Criteria[answer.Choice]; !ok {
			return nil, fmt.Errorf("answer %q: choice %q is not one of the criteria options", id, answer.Choice)
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
		if err := checkDecisionAnswerType(id, decisionQuestionBool, message); err != nil {
			return nil, err
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
		if err := checkDecisionAnswerType(id, decisionQuestionScore, message); err != nil {
			return nil, err
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

func validateDecisionRequest(req model.DecisionRequest) error {
	if err := validateDecisionState(req.State); err != nil {
		return err
	}

	total := len(req.Choice) + len(req.Bool) + len(req.Score)
	if total == 0 {
		return fmt.Errorf("at least one question is required")
	}
	if total > decisionMaxQuestions {
		return fmt.Errorf("question count %d exceeds the limit of %d", total, decisionMaxQuestions)
	}

	seen := make(map[string]struct{}, total)
	for id, question := range req.Choice {
		if err := registerDecisionQuestionID(seen, id); err != nil {
			return err
		}
		if len(question.Criteria) == 0 {
			return fmt.Errorf("choice question %q: criteria is required", id)
		}
		if len(question.Criteria) > decisionMaxChoiceOptions {
			return fmt.Errorf("choice question %q: option count %d exceeds the limit of %d", id, len(question.Criteria), decisionMaxChoiceOptions)
		}
		for option := range question.Criteria {
			if strings.TrimSpace(option) == "" {
				return fmt.Errorf("choice question %q: criteria option name is required", id)
			}
		}
	}
	for id := range req.Bool {
		if err := registerDecisionQuestionID(seen, id); err != nil {
			return err
		}
	}
	for id, question := range req.Score {
		if err := registerDecisionQuestionID(seen, id); err != nil {
			return err
		}
		if len(question.Criteria) < decisionMinScoreLevels {
			return fmt.Errorf("score question %q: criteria requires at least %d levels", id, decisionMinScoreLevels)
		}
		if len(question.Criteria) > decisionMaxScoreLevels {
			return fmt.Errorf("score question %q: level count %d exceeds the limit of %d", id, len(question.Criteria), decisionMaxScoreLevels)
		}
		for index, level := range question.Criteria {
			if strings.TrimSpace(level) == "" {
				return fmt.Errorf("score question %q: level %d description is required", id, index)
			}
		}
	}
	return nil
}

func registerDecisionQuestionID(seen map[string]struct{}, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("question id is required")
	}
	if _, ok := seen[id]; ok {
		return fmt.Errorf("duplicate question id %q", id)
	}
	seen[id] = struct{}{}
	return nil
}

func validateDecisionState(state any) error {
	if state == nil {
		return fmt.Errorf("state is required")
	}
	if text, ok := state.(string); ok {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("state is required")
		}
		return nil
	}
	switch value := reflect.ValueOf(state); value.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		if value.Len() == 0 {
			return fmt.Errorf("state is required")
		}
	}
	return nil
}

func checkDecisionAnswerType(id string, expected string, message json.RawMessage) error {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(message, &envelope); err != nil {
		return fmt.Errorf("decode answer %q: %w", id, err)
	}
	if envelope.Type != expected {
		return fmt.Errorf("answer %q: expected type %q, got %q", id, expected, envelope.Type)
	}
	return nil
}
