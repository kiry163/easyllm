package engine

import (
	"context"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

type Engine struct {
	client            provider.ModelClient
	instructions      string
	tools             []tool.Tool
	hooks             Hooks
	maxModelCalls     int
	stopAfterToolCall bool
}

type RunRequest struct {
	Session  *Session
	Input    string
	Metadata map[string]any
}

type Option func(*Engine)

func WithInstructions(text string) Option {
	return func(e *Engine) {
		e.instructions = text
	}
}

func WithTools(tools ...tool.Tool) Option {
	return func(e *Engine) {
		e.tools = append([]tool.Tool(nil), tools...)
	}
}

func WithHooks(hooks Hooks) Option {
	return func(e *Engine) {
		e.hooks = hooks
	}
}

func WithMaxModelCalls(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxModelCalls = n
		}
	}
}

func WithStopAfterToolCall(stop bool) Option {
	return func(e *Engine) {
		e.stopAfterToolCall = stop
	}
}

func New(client provider.ModelClient, opts ...Option) *Engine {
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
		session.AppendItems(provider.SystemMessageItem{
			Content: []provider.TextPart{{Text: e.instructions}},
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
