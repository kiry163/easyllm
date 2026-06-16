package model

type ModelInfo struct {
	ID       string         `json:"id"`
	Object   string         `json:"object,omitempty"`
	Created  int64          `json:"created,omitempty"`
	OwnedBy  string         `json:"owned_by,omitempty"`
	Provider string         `json:"provider,omitempty"`
	Raw      map[string]any `json:"raw,omitempty"`
}

type ListModelsResponse struct {
	Models   []ModelInfo `json:"models"`
	Provider string      `json:"provider"`
	Raw      any         `json:"raw,omitempty"`
}
