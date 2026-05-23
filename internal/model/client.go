package model

import "context"

type Client interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
}
