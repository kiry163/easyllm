package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kiry163/easyllm/internal/httpjson"
	"github.com/kiry163/easyllm/internal/model"
)

type EmbeddingClientConfig struct {
	Model string
}

type EmbeddingClient struct {
	client       httpjson.Client
	extraBody    map[string]any
	model        string
	providerName string
}

func (p *Provider) EmbeddingClient(config EmbeddingClientConfig) model.EmbeddingClient {
	return &EmbeddingClient{
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
		extraBody:    cloneMap(p.extraBody),
		model:        strings.TrimSpace(config.Model),
		providerName: p.providerName,
	}
}

func (c *EmbeddingClient) Embed(ctx context.Context, req model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	if len(req.Input) == 0 {
		return nil, fmt.Errorf("input is required")
	}
	if req.Dimensions < 0 {
		return nil, fmt.Errorf("dimensions cannot be negative")
	}

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
	body["input"] = append([]string(nil), req.Input...)
	if req.Dimensions > 0 {
		body["dimensions"] = req.Dimensions
	}
	for key, value := range req.Options {
		body[key] = value
	}

	resp, err := c.client.Post(ctx, "/embeddings", body)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	var rawPayload map[string]any
	if err := json.Unmarshal(resp.Body, &rawPayload); err != nil {
		return nil, fmt.Errorf("decode raw embedding response: %w", err)
	}

	responseModel := strings.TrimSpace(raw.Model)
	if responseModel == "" {
		responseModel = modelName
	}
	out := &model.EmbeddingResponse{
		Embeddings: make([]model.Embedding, 0, len(raw.Data)),
		Model:      responseModel,
		Usage: model.Usage{
			InputTokens: raw.Usage.PromptTokens,
			TotalTokens: raw.Usage.TotalTokens,
		},
		Provider:   c.providerName,
		RetryCount: resp.RetryCount,
		Raw:        rawPayload,
	}
	for _, item := range raw.Data {
		out.Embeddings = append(out.Embeddings, model.Embedding{
			Index:  item.Index,
			Vector: item.Embedding,
		})
	}
	return out, nil
}
