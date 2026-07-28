package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kiry163/easyllm/internal/httpjson"
	"github.com/kiry163/easyllm/internal/model"
)

type RerankClientConfig struct {
	Model string
}

type RerankClient struct {
	client    httpjson.Client
	extraBody map[string]any
	model     string
}

func (p *Provider) RerankClient(config RerankClientConfig) model.RerankClient {
	return &RerankClient{
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

func (c *RerankClient) Rerank(ctx context.Context, req model.RerankRequest) (*model.RerankResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if len(req.Documents) == 0 {
		return nil, fmt.Errorf("documents are required")
	}
	for i, document := range req.Documents {
		if strings.TrimSpace(document) == "" {
			return nil, fmt.Errorf("document at index %d is required", i)
		}
	}
	if req.TopN < 0 {
		return nil, fmt.Errorf("top_n cannot be negative")
	}
	if req.TopN > len(req.Documents) {
		return nil, fmt.Errorf("top_n cannot exceed document count")
	}

	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		modelName = c.model
	}
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	if modelName != "qwen3-rerank" {
		return nil, fmt.Errorf("model %q is not supported for qwen reranking", modelName)
	}

	body := cloneMap(c.extraBody)
	if body == nil {
		body = map[string]any{}
	}
	body["model"] = modelName
	body["query"] = req.Query
	body["documents"] = append([]string(nil), req.Documents...)
	if req.TopN > 0 {
		body["top_n"] = req.TopN
	}
	if req.Instruction != "" {
		body["instruct"] = req.Instruction
	}
	for key, value := range req.Options {
		body[key] = value
	}

	resp, err := c.client.Post(ctx, "/reranks", body)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}
	var rawPayload map[string]any
	if err := json.Unmarshal(resp.Body, &rawPayload); err != nil {
		return nil, fmt.Errorf("decode raw rerank response: %w", err)
	}

	responseModel := strings.TrimSpace(raw.Model)
	if responseModel == "" {
		responseModel = modelName
	}
	out := &model.RerankResponse{
		Results:    make([]model.RerankResult, 0, len(raw.Results)),
		Model:      responseModel,
		ResponseID: raw.ID,
		Usage:      model.Usage{TotalTokens: raw.Usage.TotalTokens},
		Provider:   "qwen",
		RetryCount: resp.RetryCount,
		Raw:        rawPayload,
	}
	for _, item := range raw.Results {
		if item.Index < 0 || item.Index >= len(req.Documents) {
			return nil, fmt.Errorf("rerank response index %d is out of range", item.Index)
		}
		out.Results = append(out.Results, model.RerankResult{
			Index:    item.Index,
			Score:    item.RelevanceScore,
			Document: req.Documents[item.Index],
		})
	}
	return out, nil
}
