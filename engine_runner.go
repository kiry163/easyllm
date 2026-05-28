package easyllm

import (
	"context"
	"fmt"

	"github.com/kiry163/easyllm/internal/model"
)

type runConfig struct {
	tools             []Tool
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
	Session        SessionSnapshot  `json:"session"`
	OutputText     string           `json:"output_text"`
	ModelCallCount int              `json:"model_call_count"`
	ToolCallCount  int              `json:"tool_call_count"`
	ToolResults    []ToolCallResult `json:"tool_results"`
	StopReason     StopReason       `json:"stop_reason"`
	Usage          model.Usage      `json:"usage"`
}

type ToolCallResult struct {
	Call   model.ToolCallOutput `json:"call"`
	Result ToolResult           `json:"result"`
	Err    error                `json:"err"`
}

func run(ctx context.Context, client Client, session *Session, metadata map[string]any, cfg runConfig) (retResult *RunResult, retErr error) {
	if session == nil {
		return nil, newEngineError(EngineErrorInvalidRequest, "run", fmt.Errorf("session is required"))
	}
	if client == nil {
		return nil, newEngineError(EngineErrorInvalidRequest, "run", fmt.Errorf("client is required"))
	}
	if cfg.maxModelCalls <= 0 {
		cfg.maxModelCalls = 3
	}
	if cfg.hooks.OnRunStart != nil {
		if err := cfg.hooks.OnRunStart(RunStartEvent{Session: session.Snapshot(), Metadata: cloneMap(metadata)}); err != nil {
			return nil, newEngineError(EngineErrorHook, "on_run_start", err)
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
				retErr = newEngineError(EngineErrorHook, "on_run_finish", finishErr)
			}
		}
	}()

	toolMap := make(map[string]Tool, len(cfg.tools))
	for _, current := range cfg.tools {
		if current == nil {
			continue
		}
		toolMap[current.Definition().Name] = current
	}

	for iteration := 1; iteration <= cfg.maxModelCalls; iteration++ {
		modelReq := ModelRequest{
			Input:    session.ItemsView(),
			Tools:    definitionsFromTools(cfg.tools),
			Metadata: cloneMap(metadata),
		}
		if cfg.hooks.OnModelRequest != nil {
			if err := cfg.hooks.OnModelRequest(ModelRequestEvent{
				Session:  session.Snapshot(),
				Request:  modelReq,
				Metadata: cloneMap(metadata),
			}); err != nil {
				retErr = newEngineError(EngineErrorHook, "on_model_request", err)
				return nil, retErr
			}
		}
		resp, err := client.Generate(ctx, modelReq)
		if err != nil {
			retErr = modelEngineError("model_generate", err)
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
				retErr = newEngineError(EngineErrorHook, "on_model_response", err)
				return nil, retErr
			}
		}

		hasToolCalls := false
		for _, item := range resp.Output {
			switch out := item.(type) {
			case model.MessageOutput:
				if len(out.Content) > 0 {
					result.OutputText = out.Content[len(out.Content)-1].Text
				}
			case model.ToolCallOutput:
				hasToolCalls = true
				result.ToolCallCount++
				if cfg.hooks.OnToolCallStart != nil {
					if err := cfg.hooks.OnToolCallStart(ToolCallStartEvent{
						Session:  session.Snapshot(),
						Call:     out,
						Metadata: cloneMap(metadata),
					}); err != nil {
						retErr = newEngineError(EngineErrorHook, "on_tool_call_start", err)
						return nil, retErr
					}
				}
				runTool := toolMap[out.Name]
				if runTool == nil {
					toolErr := NewPayloadError("tool not found", map[string]any{
						"status":  "error",
						"code":    "tool_not_found",
						"tool":    out.Name,
						"message": fmt.Sprintf("tool %q not found", out.Name),
					})
					session.AppendItems(model.ToolResultItem{
						CallID:  out.CallID,
						Name:    out.Name,
						Content: encodeToolError(out.Name, toolErr),
					})
					if cfg.hooks.OnToolCallFinish != nil {
						if err := cfg.hooks.OnToolCallFinish(ToolCallFinishEvent{
							Session:  session.Snapshot(),
							Call:     out,
							Err:      toolErr,
							Metadata: cloneMap(metadata),
						}); err != nil {
							retErr = newEngineError(EngineErrorHook, "on_tool_call_finish", err)
							return nil, retErr
						}
					}
					continue
				}
				outcome, invokeErr := runTool.Invoke(ctx, ToolCallContext{
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
					message = encodeToolError(out.Name, invokeErr)
				} else {
					message = encodeToolSuccess(out.Name, outcome)
				}
				session.AppendItems(model.ToolResultItem{
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
						retErr = newEngineError(EngineErrorHook, "on_tool_call_finish", err)
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

func runStream(ctx context.Context, client Client, session *Session, metadata map[string]any, handler StreamHandler, cfg runConfig) (retResult *RunResult, retErr error) {
	if session == nil {
		return nil, newEngineError(EngineErrorInvalidRequest, "run_stream", fmt.Errorf("session is required"))
	}
	if client == nil {
		return nil, newEngineError(EngineErrorInvalidRequest, "run_stream", fmt.Errorf("client is required"))
	}
	if cfg.maxModelCalls <= 0 {
		cfg.maxModelCalls = 3
	}
	if cfg.hooks.OnRunStart != nil {
		if err := cfg.hooks.OnRunStart(RunStartEvent{Session: session.Snapshot(), Metadata: cloneMap(metadata)}); err != nil {
			return nil, newEngineError(EngineErrorHook, "on_run_start", err)
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
				retErr = newEngineError(EngineErrorHook, "on_run_finish", finishErr)
			}
		}
	}()

	toolMap := make(map[string]Tool, len(cfg.tools))
	for _, current := range cfg.tools {
		if current == nil {
			continue
		}
		toolMap[current.Definition().Name] = current
	}

	for iteration := 1; iteration <= cfg.maxModelCalls; iteration++ {
		modelReq := ModelRequest{
			Input:    session.ItemsView(),
			Tools:    definitionsFromTools(cfg.tools),
			Metadata: cloneMap(metadata),
		}
		if cfg.hooks.OnModelRequest != nil {
			if err := cfg.hooks.OnModelRequest(ModelRequestEvent{
				Session:  session.Snapshot(),
				Request:  modelReq,
				Metadata: cloneMap(metadata),
			}); err != nil {
				retErr = newEngineError(EngineErrorHook, "on_model_request", err)
				return nil, retErr
			}
		}

		var resp *ModelResponse
		err := client.GenerateStream(ctx, modelReq, func(event StreamEvent) error {
			switch event.Type {
			case StreamEventDone:
				if event.Raw == nil {
					return nil
				}
				captured, ok := event.Raw.(*ModelResponse)
				if !ok {
					return newEngineError(EngineErrorInternal, "run_stream", fmt.Errorf("stream done event missing model response"))
				}
				resp = captured
				return nil
			case StreamEventMessageDelta:
				if handler != nil {
					if err := handler(event); err != nil {
						return newEngineError(EngineErrorHandler, "stream_handler", err)
					}
				}
			}
			return nil
		})
		if err != nil {
			retErr = modelEngineError("model_generate_stream", err)
			return nil, retErr
		}
		if resp == nil {
			retErr = newEngineError(EngineErrorInternal, "run_stream", fmt.Errorf("stream completed without model response"))
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
				retErr = newEngineError(EngineErrorHook, "on_model_response", err)
				return nil, retErr
			}
		}

		hasToolCalls := false
		for _, item := range resp.Output {
			switch out := item.(type) {
			case model.MessageOutput:
				if len(out.Content) > 0 {
					result.OutputText = out.Content[len(out.Content)-1].Text
				}
			case model.ToolCallOutput:
				hasToolCalls = true
				result.ToolCallCount++
				if cfg.hooks.OnToolCallStart != nil {
					if err := cfg.hooks.OnToolCallStart(ToolCallStartEvent{
						Session:  session.Snapshot(),
						Call:     out,
						Metadata: cloneMap(metadata),
					}); err != nil {
						retErr = newEngineError(EngineErrorHook, "on_tool_call_start", err)
						return nil, retErr
					}
				}
				if handler != nil {
					if err := handler(StreamEvent{Type: StreamEventToolStart, ToolName: out.Name}); err != nil {
						retErr = newEngineError(EngineErrorHandler, "stream_handler", err)
						return nil, retErr
					}
				}
				runTool := toolMap[out.Name]
				if runTool == nil {
					toolErr := NewPayloadError("tool not found", map[string]any{
						"status":  "error",
						"code":    "tool_not_found",
						"tool":    out.Name,
						"message": fmt.Sprintf("tool %q not found", out.Name),
					})
					session.AppendItems(model.ToolResultItem{
						CallID:  out.CallID,
						Name:    out.Name,
						Content: encodeToolError(out.Name, toolErr),
					})
					if cfg.hooks.OnToolCallFinish != nil {
						if err := cfg.hooks.OnToolCallFinish(ToolCallFinishEvent{
							Session:  session.Snapshot(),
							Call:     out,
							Err:      toolErr,
							Metadata: cloneMap(metadata),
						}); err != nil {
							retErr = newEngineError(EngineErrorHook, "on_tool_call_finish", err)
							return nil, retErr
						}
					}
					if handler != nil {
						if err := handler(StreamEvent{Type: StreamEventToolFinish, ToolName: out.Name}); err != nil {
							retErr = newEngineError(EngineErrorHandler, "stream_handler", err)
							return nil, retErr
						}
					}
					continue
				}
				outcome, invokeErr := runTool.Invoke(ctx, ToolCallContext{
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
					message = encodeToolError(out.Name, invokeErr)
				} else {
					message = encodeToolSuccess(out.Name, outcome)
				}
				session.AppendItems(model.ToolResultItem{
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
						retErr = newEngineError(EngineErrorHook, "on_tool_call_finish", err)
						return nil, retErr
					}
				}
				if handler != nil {
					if err := handler(StreamEvent{Type: StreamEventToolFinish, ToolName: out.Name}); err != nil {
						retErr = newEngineError(EngineErrorHandler, "stream_handler", err)
						return nil, retErr
					}
				}
				if invokeErr == nil && cfg.stopAfterToolCall {
					result.StopReason = StopReasonStopOnToolResult
					result.Session = session.Snapshot()
					if handler != nil {
						if err := handler(StreamEvent{Type: StreamEventDone}); err != nil {
							retErr = newEngineError(EngineErrorHandler, "stream_handler", err)
							return nil, retErr
						}
					}
					retResult = result
					return retResult, nil
				}
			}
		}

		if !hasToolCalls {
			result.StopReason = StopReasonStop
			result.Session = session.Snapshot()
			if handler != nil {
				if err := handler(StreamEvent{Type: StreamEventDone}); err != nil {
					retErr = newEngineError(EngineErrorHandler, "stream_handler", err)
					return nil, retErr
				}
			}
			retResult = result
			return retResult, nil
		}
	}

	result.StopReason = StopReasonModelCallLimitExceeded
	result.Session = session.Snapshot()
	if handler != nil {
		if err := handler(StreamEvent{Type: StreamEventDone}); err != nil {
			retErr = newEngineError(EngineErrorHandler, "stream_handler", err)
			return nil, retErr
		}
	}
	retResult = result
	return retResult, nil
}

func outputItemsToInputItems(items []model.OutputItem) []model.InputItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.InputItem, 0, len(items))
	for _, item := range items {
		switch current := item.(type) {
		case model.MessageOutput:
			out = append(out, model.AssistantMessageItem{
				Content:       current.Content,
				ProviderState: cloneMap(current.ProviderState),
			})
		case model.ToolCallOutput:
			out = append(out, model.ToolCallItem{
				CallID:        current.CallID,
				Name:          current.Name,
				Arguments:     current.Arguments,
				RawArguments:  current.RawArguments,
				Repaired:      current.Repaired,
				ProviderState: cloneMap(current.ProviderState),
			})
		}
	}
	return out
}

func addUsage(dst *model.Usage, src model.Usage) {
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
