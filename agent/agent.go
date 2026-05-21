package agent

import (
	"context"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/runtime"
	"github.com/kiry163/easyllm/tool"
)

type Agent struct {
	Model        string
	Instructions string
	Tools        []tool.Tool
	Client       provider.Client
	Runner       *runtime.Runner
	Options      map[string]any
}

type RunRequest struct {
	Session  *runtime.Session
	Input    string
	Metadata map[string]any
}

type RunResult struct {
	Session    runtime.SessionSnapshot
	OutputText string
	StopReason string
}

func (a Agent) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	session := req.Session
	if session == nil {
		session = runtime.NewSession(a.Model)
	}
	if a.Instructions != "" && len(session.Items) == 0 {
		session.AppendItems(provider.SystemMessageItem{
			Content: []provider.TextPart{{Text: a.Instructions}},
		})
	}
	if req.Input != "" {
		session.AppendUserText(req.Input)
	}
	runner := a.Runner
	if runner == nil {
		runner = &runtime.Runner{Client: a.Client}
	}
	result, err := runner.Run(ctx, runtime.RunRequest{
		Session:       session,
		Tools:         a.Tools,
		Options:       a.Options,
		MaxModelCalls: 3,
		Metadata:      req.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &RunResult{
		Session:    result.Session,
		OutputText: result.OutputText,
		StopReason: result.StopReason,
	}, nil
}
