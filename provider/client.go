package provider

import "context"

type Client interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
}

type ModelClient = Client
