package model

import "context"

type EmbeddingClient interface {
	Embed(context.Context, EmbeddingRequest) (*EmbeddingResponse, error)
}

type EmbeddingRequest struct {
	Model      string         `json:"model,omitempty"`
	Input      []string       `json:"input"`
	Dimensions int            `json:"dimensions,omitempty"`
	Options    map[string]any `json:"options,omitempty"`
}

type Embedding struct {
	Index  int       `json:"index"`
	Vector []float32 `json:"vector"`
}

type EmbeddingResponse struct {
	Embeddings []Embedding `json:"embeddings"`
	Model      string      `json:"model"`
	Usage      Usage       `json:"usage"`
	Provider   string      `json:"provider"`
	RetryCount int         `json:"retry_count"`
	Raw        any         `json:"raw"`
}
