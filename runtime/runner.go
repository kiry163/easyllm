package runtime

import (
	"context"
	"fmt"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

type RunRequest struct {
	Session          *Session
	Tools            []tool.Tool
	Options          map[string]any
	MaxModelCalls    int
	StopOnToolResult func(tool.Result) bool
	Metadata         map[string]any
}

type RunResult struct {
	Session        SessionSnapshot
	OutputText     string
	ModelCallCount int
	ToolCallCount  int
	StopReason     string
}

type Runner struct {
	Client provider.Client
	Hooks  Hooks
}

func (r Runner) Run(ctx context.Context, req RunRequest) (retResult *RunResult, retErr error) {
	if req.Session == nil {
		return nil, fmt.Errorf("session is required")
	}
	if r.Client == nil {
		return nil, fmt.Errorf("client is required")
	}
	if req.MaxModelCalls <= 0 {
		req.MaxModelCalls = 3
	}
	if r.Hooks.OnRunStart != nil {
		if err := r.Hooks.OnRunStart(RunStartEvent{Session: req.Session.Snapshot(), Metadata: cloneMap(req.Metadata)}); err != nil {
			return nil, err
		}
	}

	result := &RunResult{}
	defer func() {
		if r.Hooks.OnRunFinish != nil {
			finishErr := r.Hooks.OnRunFinish(RunFinishEvent{
				Session:  req.Session.Snapshot(),
				Result:   result,
				Err:      retErr,
				Metadata: cloneMap(req.Metadata),
			})
			if finishErr != nil && retErr == nil {
				retErr = finishErr
			}
		}
	}()

	toolMap := make(map[string]tool.Tool, len(req.Tools))
	for _, current := range req.Tools {
		if current == nil {
			continue
		}
		toolMap[current.Definition().Name] = current
	}

	for iteration := 1; iteration <= req.MaxModelCalls; iteration++ {
		modelReq := provider.ModelRequest{
			Model:    req.Session.Model,
			Input:    req.Session.ItemsView(),
			Tools:    tool.DefinitionsFromTools(req.Tools),
			Options:  cloneMap(req.Options),
			Metadata: cloneMap(req.Metadata),
		}
		if r.Hooks.OnModelRequest != nil {
			if err := r.Hooks.OnModelRequest(ModelRequestEvent{
				Session:  req.Session.Snapshot(),
				Request:  modelReq,
				Metadata: cloneMap(req.Metadata),
			}); err != nil {
				retErr = err
				return nil, retErr
			}
		}
		resp, err := r.Client.Generate(ctx, modelReq)
		if err != nil {
			retErr = err
			return nil, retErr
		}
		result.ModelCallCount++
		req.Session.Iteration = iteration
		req.Session.LastResponse = resp
		req.Session.Usage.InputTokens += resp.Usage.InputTokens
		req.Session.Usage.OutputTokens += resp.Usage.OutputTokens
		req.Session.Usage.TotalTokens += resp.Usage.TotalTokens

		req.Session.AppendItems(outputItemsToInputItems(resp.Output)...)
		if r.Hooks.OnModelResponse != nil {
			if err := r.Hooks.OnModelResponse(ModelResponseEvent{
				Session:  req.Session.Snapshot(),
				Response: *resp,
				Metadata: cloneMap(req.Metadata),
			}); err != nil {
				retErr = err
				return nil, retErr
			}
		}

		hasToolCalls := false
		for _, item := range resp.Output {
			switch out := item.(type) {
			case provider.MessageOutput:
				if len(out.Content) > 0 {
					result.OutputText = out.Content[len(out.Content)-1].Text
				}
			case provider.ToolCallOutput:
				hasToolCalls = true
				result.ToolCallCount++
				if r.Hooks.OnToolCallStart != nil {
					if err := r.Hooks.OnToolCallStart(ToolCallStartEvent{
						Session:  req.Session.Snapshot(),
						Call:     out,
						Metadata: cloneMap(req.Metadata),
					}); err != nil {
						retErr = err
						return nil, retErr
					}
				}
				runTool := toolMap[out.Name]
				if runTool == nil {
					toolErr := tool.NewPayloadError("tool not found", map[string]any{
						"status":  "error",
						"code":    "tool_not_found",
						"tool":    out.Name,
						"message": fmt.Sprintf("tool %q not found", out.Name),
					})
					req.Session.AppendItems(provider.ToolResultItem{
						CallID:  out.CallID,
						Name:    out.Name,
						Content: tool.EncodeToolError(out.Name, toolErr),
					})
					if r.Hooks.OnToolCallFinish != nil {
						if err := r.Hooks.OnToolCallFinish(ToolCallFinishEvent{
							Session:  req.Session.Snapshot(),
							Call:     out,
							Err:      toolErr,
							Metadata: cloneMap(req.Metadata),
						}); err != nil {
							retErr = err
							return nil, retErr
						}
					}
					continue
				}
				outcome, invokeErr := runTool.Invoke(ctx, tool.CallContext{
					CallID:    out.CallID,
					Name:      out.Name,
					Iteration: iteration,
					Metadata:  cloneMap(req.Metadata),
				}, out.Arguments)
				message := ""
				if invokeErr != nil {
					message = tool.EncodeToolError(out.Name, invokeErr)
				} else {
					message = tool.EncodeToolSuccess(out.Name, outcome)
				}
				req.Session.AppendItems(provider.ToolResultItem{
					CallID:  out.CallID,
					Name:    out.Name,
					Content: message,
				})
				if r.Hooks.OnToolCallFinish != nil {
					event := ToolCallFinishEvent{
						Session:  req.Session.Snapshot(),
						Call:     out,
						Metadata: cloneMap(req.Metadata),
					}
					if invokeErr != nil {
						event.Err = invokeErr
					} else {
						copyOutcome := outcome
						event.Result = &copyOutcome
					}
					if err := r.Hooks.OnToolCallFinish(event); err != nil {
						retErr = err
						return nil, retErr
					}
				}
				if invokeErr == nil && req.StopOnToolResult != nil && req.StopOnToolResult(outcome) {
					result.StopReason = "stop_on_tool_result"
					result.Session = req.Session.Snapshot()
					retResult = result
					return retResult, nil
				}
			}
		}

		if !hasToolCalls {
			result.StopReason = "stop"
			result.Session = req.Session.Snapshot()
			retResult = result
			return retResult, nil
		}
	}

	result.StopReason = "model_call_limit_exceeded"
	result.Session = req.Session.Snapshot()
	retResult = result
	return retResult, nil
}

func outputItemsToInputItems(items []provider.OutputItem) []provider.InputItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]provider.InputItem, 0, len(items))
	for _, item := range items {
		switch current := item.(type) {
		case provider.MessageOutput:
			out = append(out, provider.AssistantMessageItem{
				Content: current.Content,
			})
		case provider.ToolCallOutput:
			out = append(out, provider.ToolCallItem{
				CallID:       current.CallID,
				Name:         current.Name,
				Arguments:    current.Arguments,
				RawArguments: current.RawArguments,
				Repaired:     current.Repaired,
			})
		}
	}
	return out
}
