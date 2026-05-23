package easyllm

import (
	"context"

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
	Session  *Session
	Input    string
	Metadata map[string]any
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
	return run(ctx, e.client, session, req.Metadata, runConfig{
		tools:             e.tools,
		hooks:             e.hooks,
		maxModelCalls:     e.maxModelCalls,
		stopAfterToolCall: e.stopAfterToolCall,
	})
}
