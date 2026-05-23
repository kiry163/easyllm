package model

import "context"

type ImageClient interface {
	GenerateImage(context.Context, ImageRequest) (*ImageResponse, error)
}

type ImageRequest struct {
	Prompt       string         `json:"prompt"`
	Model        string         `json:"model"`
	Size         string         `json:"size"`
	Quality      string         `json:"quality"`
	Background   string         `json:"background"`
	OutputFormat string         `json:"output_format"`
	Count        int            `json:"count"`
	Options      map[string]any `json:"options"`
}

type GeneratedImage struct {
	B64JSON       string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt"`
}

type ImageResponse struct {
	Created  int64            `json:"created"`
	Images   []GeneratedImage `json:"images"`
	Usage    Usage            `json:"usage"`
	Provider string           `json:"provider"`
	Raw      any              `json:"raw"`
}
