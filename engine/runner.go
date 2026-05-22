package engine

import (
	"context"
	"fmt"

	"github.com/kiry163/easyllm"
	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/tool"
)

type runConfig struct {
	tools             []tool.Tool
	hooks             Hooks
	maxModelCalls     int
	stopAfterToolCall bool
}

type StopReason string

const (
	StopReasonStop                   StopReason = "stop"
	StopReasonStopOnToolResult       StopReason = "stop_on_tool_result"
	StopReasonModelCallLimitExceeded StopReason = "model_call_limit_exceeded"
)

type RunResult struct {
	Session        SessionSnapshot
	OutputText     string
	ModelCallCount int
	ToolCallCount  int
	ToolResults    []ToolCallResult
	StopReason     StopReason
	Usage          provider.Usage
}

type ToolCallResult struct {
	Call   provider.ToolCallOutput
	Result tool.Result
	Err    error
}

func run(ctx context.Context, client easyllm.Client, session *Session, metadata map[string]any, cfg runConfig) (retResult *RunResult, retErr error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}
	if cfg.maxModelCalls <= 0 {
		cfg.maxModelCalls = 3
	}
	if cfg.hooks.OnRunStart != nil {
		if err := cfg.hooks.OnRunStart(RunStartEvent{Session: session.Snapshot(), Metadata: cloneMap(metadata)}); err != nil {
			return nil, err
		}
	}

	result := &RunResult{}
	defer func() {
		if cfg.hooks.OnRunFinish != nil {
			finishErr := cfg.hooks.OnRunFinish(RunFinishEvent{
				Session:  session.Snapshot(),
				Result:   result,
				Err:      retErr,
				Metadata: cloneMap(metadata),
			})
			if finishErr != nil && retErr == nil {
				retErr = finishErr
			}
		}
	}()

	toolMap := make(map[string]tool.Tool, len(cfg.tools))
	for _, current := range cfg.tools {
		if current == nil {
			continue
		}
		toolMap[current.Definition().Name] = current
	}

	for iteration := 1; iteration <= cfg.maxModelCalls; iteration++ {
		modelReq := easyllm.ModelRequest{
			Input:    session.ItemsView(),
			Tools:    tool.DefinitionsFromTools(cfg.tools),
			Metadata: cloneMap(metadata),
		}
		if cfg.hooks.OnModelRequest != nil {
			if err := cfg.hooks.OnModelRequest(ModelRequestEvent{
				Session:  session.Snapshot(),
				Request:  modelReq,
				Metadata: cloneMap(metadata),
			}); err != nil {
				retErr = err
				return nil, retErr
			}
		}
		resp, err := client.Generate(ctx, modelReq)
		if err != nil {
			retErr = err
			return nil, retErr
		}
		result.ModelCallCount++
		session.Iteration = iteration
		session.LastResponse = resp
		addUsage(&session.Usage, resp.Usage)
		addUsage(&result.Usage, resp.Usage)

		session.AppendItems(outputItemsToInputItems(resp.Output)...)
		if cfg.hooks.OnModelResponse != nil {
			if err := cfg.hooks.OnModelResponse(ModelResponseEvent{
				Session:  session.Snapshot(),
				Response: *resp,
				Metadata: cloneMap(metadata),
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
				if cfg.hooks.OnToolCallStart != nil {
					if err := cfg.hooks.OnToolCallStart(ToolCallStartEvent{
						Session:  session.Snapshot(),
						Call:     out,
						Metadata: cloneMap(metadata),
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
					session.AppendItems(provider.ToolResultItem{
						CallID:  out.CallID,
						Name:    out.Name,
						Content: tool.EncodeToolError(out.Name, toolErr),
					})
					if cfg.hooks.OnToolCallFinish != nil {
						if err := cfg.hooks.OnToolCallFinish(ToolCallFinishEvent{
							Session:  session.Snapshot(),
							Call:     out,
							Err:      toolErr,
							Metadata: cloneMap(metadata),
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
					Metadata:  cloneMap(metadata),
				}, out.Arguments)
				result.ToolResults = append(result.ToolResults, ToolCallResult{
					Call:   out,
					Result: outcome,
					Err:    invokeErr,
				})
				message := ""
				if invokeErr != nil {
					message = tool.EncodeToolError(out.Name, invokeErr)
				} else {
					message = tool.EncodeToolSuccess(out.Name, outcome)
				}
				session.AppendItems(provider.ToolResultItem{
					CallID:  out.CallID,
					Name:    out.Name,
					Content: message,
				})
				if cfg.hooks.OnToolCallFinish != nil {
					event := ToolCallFinishEvent{
						Session:  session.Snapshot(),
						Call:     out,
						Metadata: cloneMap(metadata),
					}
					if invokeErr != nil {
						event.Err = invokeErr
					} else {
						copyOutcome := outcome
						event.Result = &copyOutcome
					}
					if err := cfg.hooks.OnToolCallFinish(event); err != nil {
						retErr = err
						return nil, retErr
					}
				}
				if invokeErr == nil && cfg.stopAfterToolCall {
					result.StopReason = StopReasonStopOnToolResult
					result.Session = session.Snapshot()
					retResult = result
					return retResult, nil
				}
			}
		}

		if !hasToolCalls {
			result.StopReason = StopReasonStop
			result.Session = session.Snapshot()
			retResult = result
			return retResult, nil
		}
	}

	result.StopReason = StopReasonModelCallLimitExceeded
	result.Session = session.Snapshot()
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

func addUsage(dst *provider.Usage, src provider.Usage) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.TotalTokens += src.TotalTokens
	dst.CachedInputTokens += src.CachedInputTokens
	if len(src.Details) == 0 {
		return
	}
	if dst.Details == nil {
		dst.Details = make(map[string]any, len(src.Details))
	}
	for key, value := range src.Details {
		dst.Details[key] = value
	}
}
