package easyllm

import (
	"context"
	"fmt"

	"github.com/kiry163/easyllm/internal/model"
)

type Engine struct {
	client            Client
	instructions      string
	tools             []Tool
	hooks             Hooks
	maxModelCalls     int
	stopAfterToolCall bool
}

type RunRequest struct {
	Session    *Session
	Input      string
	InputParts []model.ContentPart
	Metadata   map[string]any
}

type EngineOption func(*Engine)

func WithInstructions(text string) EngineOption {
	return func(e *Engine) {
		e.instructions = text
	}
}

func WithTools(tools ...Tool) EngineOption {
	return func(e *Engine) {
		e.tools = append([]Tool(nil), tools...)
	}
}

func WithHooks(hooks Hooks) EngineOption {
	return func(e *Engine) {
		e.hooks = hooks
	}
}

func WithMaxModelCalls(n int) EngineOption {
	return func(e *Engine) {
		if n > 0 {
			e.maxModelCalls = n
		}
	}
}

func WithStopAfterToolCall(stop bool) EngineOption {
	return func(e *Engine) {
		e.stopAfterToolCall = stop
	}
}

func NewEngine(client Client, opts ...EngineOption) *Engine {
	engine := &Engine{
		client:        client,
		maxModelCalls: 3,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(engine)
		}
	}
	return engine
}

func (e *Engine) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	if err := validateRunRequest(req); err != nil {
		return nil, err
	}
	session := req.Session
	if session == nil {
		session = NewSession()
	}
	if e.instructions != "" && len(session.Items) == 0 {
		session.AppendItems(model.SystemMessageItem{
			Content: []model.TextPart{{Text: e.instructions}},
		})
	}
	if req.Input != "" {
		session.AppendUserText(req.Input)
	}
	if len(req.InputParts) > 0 {
		session.AppendUserContent(req.InputParts...)
	}
	return run(ctx, e.client, session, req.Metadata, runConfig{
		tools:             e.tools,
		hooks:             e.hooks,
		maxModelCalls:     e.maxModelCalls,
		stopAfterToolCall: e.stopAfterToolCall,
	})
}

func (e *Engine) RunStream(ctx context.Context, req RunRequest, handler StreamHandler) (*RunResult, error) {
	if err := validateRunRequest(req); err != nil {
		return nil, err
	}
	session := req.Session
	if session == nil {
		session = NewSession()
	}
	if e.instructions != "" && len(session.Items) == 0 {
		session.AppendItems(model.SystemMessageItem{
			Content: []model.TextPart{{Text: e.instructions}},
		})
	}
	if req.Input != "" {
		session.AppendUserText(req.Input)
	}
	if len(req.InputParts) > 0 {
		session.AppendUserContent(req.InputParts...)
	}
	return runStream(ctx, e.client, session, req.Metadata, handler, runConfig{
		tools:             e.tools,
		hooks:             e.hooks,
		maxModelCalls:     e.maxModelCalls,
		stopAfterToolCall: e.stopAfterToolCall,
	})
}

func validateRunRequest(req RunRequest) error {
	if req.Input != "" && len(req.InputParts) > 0 {
		return newEngineError(EngineErrorInvalidRequest, "run_request", fmt.Errorf("input and input_parts cannot both be set"))
	}
	return nil
}
