package model

import "context"

type RerankClient interface {
	Rerank(context.Context, RerankRequest) (*RerankResponse, error)
}

type RerankRequest struct {
	Model       string         `json:"model,omitempty"`
	Query       string         `json:"query"`
	Documents   []string       `json:"documents"`
	TopN        int            `json:"top_n,omitempty"`
	Instruction string         `json:"instruction,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
}

type RerankResult struct {
	Index    int     `json:"index"`
	Score    float64 `json:"score"`
	Document string  `json:"document"`
}

type RerankResponse struct {
	Results    []RerankResult `json:"results"`
	Model      string         `json:"model"`
	ResponseID string         `json:"response_id"`
	Usage      Usage          `json:"usage"`
	Provider   string         `json:"provider"`
	RetryCount int            `json:"retry_count"`
	Raw        any            `json:"raw"`
}
