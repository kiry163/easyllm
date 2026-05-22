package engine

import (
	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

type Hooks struct {
	OnRunStart       func(RunStartEvent) error
	OnModelRequest   func(ModelRequestEvent) error
	OnModelResponse  func(ModelResponseEvent) error
	OnToolCallStart  func(ToolCallStartEvent) error
	OnToolCallFinish func(ToolCallFinishEvent) error
	OnRunFinish      func(RunFinishEvent) error
}

type RunStartEvent struct {
	Session  SessionSnapshot
	Metadata map[string]any
}

type ModelRequestEvent struct {
	Session  SessionSnapshot
	Request  provider.ModelRequest
	Metadata map[string]any
}

type ModelResponseEvent struct {
	Session  SessionSnapshot
	Response provider.ModelResponse
	Metadata map[string]any
}

type ToolCallStartEvent struct {
	Session  SessionSnapshot
	Call     provider.ToolCallOutput
	Metadata map[string]any
}

type ToolCallFinishEvent struct {
	Session  SessionSnapshot
	Call     provider.ToolCallOutput
	Result   *tool.Result
	Err      error
	Metadata map[string]any
}

type RunFinishEvent struct {
	Session  SessionSnapshot
	Result   *RunResult
	Err      error
	Metadata map[string]any
}
