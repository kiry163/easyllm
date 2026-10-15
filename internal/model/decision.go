package model

import "context"

// DecisionClient answers typed questions about a business state in one forward
// pass. It never generates text.
type DecisionClient interface {
	Decide(context.Context, DecisionRequest) (*DecisionResponse, error)
}

// DecisionRequest carries the business state and up to 16 typed questions,
// grouped by question type. Question ids must be unique across all groups.
type DecisionRequest struct {
	Model   string                    `json:"model,omitempty"`
	State   any                       `json:"state"`
	Choice  map[string]ChoiceQuestion `json:"choice,omitempty"`
	Bool    map[string]BoolQuestion   `json:"bool,omitempty"`
	Score   map[string]ScoreQuestion  `json:"score,omitempty"`
	Options map[string]any            `json:"options,omitempty"`
}

// ChoiceQuestion asks the model to pick exactly one option.
type ChoiceQuestion struct {
	Instructions string            `json:"instructions,omitempty"`
	Criteria     map[string]string `json:"criteria"`
}

// BoolQuestion asks a yes/no question. True and False optionally describe both
// sides of the judgment.
type BoolQuestion struct {
	Instructions string `json:"instructions,omitempty"`
	True         string `json:"true,omitempty"`
	False        string `json:"false,omitempty"`
}

// ScoreQuestion asks for a rating on an ordered scale, from lowest to highest.
type ScoreQuestion struct {
	Instructions string   `json:"instructions,omitempty"`
	Criteria     []string `json:"criteria"`
}

// ChoiceAnswer is the answer to a ChoiceQuestion.
type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// BoolAnswer is the answer to a BoolQuestion. YesProbability is P(yes), where
// 0 means no and 1 means yes.
type BoolAnswer struct {
	YesProbability float64 `json:"noul"`
}

// ScoreAnswer is the answer to a ScoreQuestion. Score is the probability
// weighted expectation of the level index and can fall between two levels.
type ScoreAnswer struct {
	Score         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Legend        map[string]string  `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// DecisionResponse holds one answer per requested question id, grouped by
// question type.
type DecisionResponse struct {
	Choice     map[string]ChoiceAnswer `json:"choice"`
	Bool       map[string]BoolAnswer   `json:"bool"`
	Score      map[string]ScoreAnswer  `json:"score"`
	Model      string                  `json:"model"`
	RequestID  string                  `json:"request_id"`
	Usage      Usage                   `json:"usage"`
	LatencyMS  float64                 `json:"latency_ms"`
	Provider   string                  `json:"provider"`
	RetryCount int                     `json:"retry_count"`
	Raw        any                     `json:"raw"`
}
