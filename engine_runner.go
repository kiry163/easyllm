package easyllm

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/kiry163/easyllm/internal/model"
)

type runConfig struct {
	tools                 []Tool
	hooks                 Hooks
	maxModelCalls         int
	parallelToolExecution bool
	maxToolConcurrency    int
	stopOnToolSuccess     bool
	stopToolName          string
	toolChoice            *ToolChoice
	requestItems          []model.InputItem
}

type StopReason string

const (
	StopReasonStop                   StopReason = "stop"
	StopReasonToolSucceeded          StopReason = "tool_succeeded"
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
	RetryCount     int              `json:"retry_count"`
}

type ToolCallResult struct {
	Call   model.ToolCallOutput `json:"call"`
	Result ToolResult           `json:"result"`
	Err    error                `json:"err"`
}

type toolExecution struct {
	call    model.ToolCallOutput
	result  ToolResult
	err     error
	content string
}

func prepareToolMap(cfg *runConfig) (map[string]Tool, error) {
	if cfg.maxToolConcurrency < 0 {
		return nil, fmt.Errorf("max tool concurrency cannot be negative")
	}
	toolMap := make(map[string]Tool, len(cfg.tools))
	for _, current := range cfg.tools {
		if current == nil {
			continue
		}
		name := current.Definition().Name
		if _, exists := toolMap[name]; exists {
			return nil, fmt.Errorf("duplicate tool name %q", name)
		}
		toolMap[name] = current
	}
	if err := validateToolChoice(cfg.toolChoice, toolMap); err != nil {
		return nil, err
	}
	if !cfg.stopOnToolSuccess {
		return toolMap, nil
	}
	cfg.stopToolName = strings.TrimSpace(cfg.stopToolName)
	if cfg.stopToolName == "" {
		return nil, fmt.Errorf("stop tool name is required")
	}
	if _, exists := toolMap[cfg.stopToolName]; !exists {
		return nil, fmt.Errorf("stop tool %q is not registered", cfg.stopToolName)
	}
	return toolMap, nil
}

func validateToolChoice(choice *ToolChoice, tools map[string]Tool) error {
	if choice == nil {
		return nil
	}
	switch choice.Mode() {
	case ToolChoiceModeAuto, ToolChoiceModeNone:
		return nil
	case ToolChoiceModeRequired:
		if len(tools) == 0 {
			return fmt.Errorf("required tool choice needs at least one registered tool")
		}
		return nil
	case ToolChoiceModeNamed:
		name := strings.TrimSpace(choice.ToolName())
		if name == "" {
			return fmt.Errorf("named tool choice requires a tool name")
		}
		if _, exists := tools[name]; !exists {
			return fmt.Errorf("tool choice %q is not registered", name)
		}
		return nil
	default:
		return fmt.Errorf("invalid tool choice mode %q", choice.Mode())
	}
}

func collectToolCalls(resp *ModelResponse, result *RunResult) []model.ToolCallOutput {
	var calls []model.ToolCallOutput
	for _, item := range resp.Output {
		out, ok := item.(model.AssistantOutput)
		if !ok {
			continue
		}
		if len(out.Content) > 0 {
			result.OutputText = out.Content[len(out.Content)-1].Text
		}
		calls = append(calls, out.ToolCalls...)
	}
	return calls
}

func processToolBatch(
	ctx context.Context,
	session *Session,
	result *RunResult,
	calls []model.ToolCallOutput,
	toolMap map[string]Tool,
	iteration int,
	metadata map[string]any,
	handler StreamHandler,
	cfg runConfig,
) (bool, error) {
	result.ToolCallCount += len(calls)
	for _, call := range calls {
		if cfg.hooks.OnToolCallStart != nil {
			if err := cfg.hooks.OnToolCallStart(ToolCallStartEvent{
				Session:  session.Snapshot(),
				Call:     call,
				Metadata: cloneMap(metadata),
			}); err != nil {
				return false, newEngineError(EngineErrorHook, "on_tool_call_start", err)
			}
		}
		if handler != nil {
			if err := handler(StreamEvent{
				Type:       StreamEventToolStart,
				ToolName:   call.Name,
				ToolCallID: call.CallID,
			}); err != nil {
				return false, newEngineError(EngineErrorHandler, "stream_handler", err)
			}
		}
	}

	executions := invokeToolBatch(ctx, calls, toolMap, iteration, metadata, cfg)
	stopTriggered := false
	for _, execution := range executions {
		result.ToolResults = append(result.ToolResults, ToolCallResult{
			Call:   execution.call,
			Result: execution.result,
			Err:    execution.err,
		})
		session.AppendItems(model.ToolResultItem{
			CallID:  execution.call.CallID,
			Name:    execution.call.Name,
			Content: execution.content,
		})
		if cfg.hooks.OnToolCallFinish != nil {
			event := ToolCallFinishEvent{
				Session:  session.Snapshot(),
				Call:     execution.call,
				Metadata: cloneMap(metadata),
			}
			if execution.err != nil {
				event.Err = execution.err
			} else {
				outcome := execution.result
				event.Result = &outcome
			}
			if err := cfg.hooks.OnToolCallFinish(event); err != nil {
				return false, newEngineError(EngineErrorHook, "on_tool_call_finish", err)
			}
		}
		if handler != nil {
			if err := handler(StreamEvent{
				Type:       StreamEventToolFinish,
				ToolName:   execution.call.Name,
				ToolCallID: execution.call.CallID,
			}); err != nil {
				return false, newEngineError(EngineErrorHandler, "stream_handler", err)
			}
		}
		if cfg.stopOnToolSuccess && execution.call.Name == cfg.stopToolName && execution.err == nil {
			stopTriggered = true
		}
	}
	return stopTriggered, nil
}

func invokeToolBatch(
	ctx context.Context,
	calls []model.ToolCallOutput,
	toolMap map[string]Tool,
	iteration int,
	metadata map[string]any,
	cfg runConfig,
) []toolExecution {
	executions := make([]toolExecution, len(calls))
	limit := 1
	if cfg.parallelToolExecution {
		limit = len(calls)
		if cfg.maxToolConcurrency > 0 && cfg.maxToolConcurrency < limit {
			limit = cfg.maxToolConcurrency
		}
	}
	if limit <= 1 {
		for index, call := range calls {
			executions[index] = invokeTool(ctx, call, toolMap, iteration, metadata)
		}
		return executions
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(limit)
	for range limit {
		go func() {
			defer workers.Done()
			for index := range jobs {
				executions[index] = invokeTool(ctx, calls[index], toolMap, iteration, metadata)
			}
		}()
	}
	for index := range calls {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return executions
}

func invokeTool(
	ctx context.Context,
	call model.ToolCallOutput,
	toolMap map[string]Tool,
	iteration int,
	metadata map[string]any,
) toolExecution {
	execution := toolExecution{call: call}
	runTool := toolMap[call.Name]
	if runTool == nil {
		execution.err = NewPayloadError("tool not found", map[string]any{
			"status":  "error",
			"code":    "tool_not_found",
			"tool":    call.Name,
			"message": fmt.Sprintf("tool %q not found", call.Name),
		})
		execution.content = encodeToolError(call.Name, execution.err)
		return execution
	}
	execution.result, execution.err = runTool.Invoke(ctx, ToolCallContext{
		CallID:       call.CallID,
		Name:         call.Name,
		Iteration:    iteration,
		Metadata:     cloneMap(metadata),
		RawArguments: call.RawArguments,
	})
	if execution.err != nil {
		execution.content = encodeToolError(call.Name, execution.err)
	} else {
		execution.content = encodeToolSuccess(call.Name, execution.result)
	}
	return execution
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
	toolMap, err := prepareToolMap(&cfg)
	if err != nil {
		return nil, newEngineError(EngineErrorInvalidRequest, "run_config", err)
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

	for iteration := 1; iteration <= cfg.maxModelCalls; iteration++ {
		modelReq := buildModelRequest(session, metadata, cfg, iteration)
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
		result.RetryCount += resp.RetryCount

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

		calls := collectToolCalls(resp, result)
		if len(calls) == 0 {
			result.StopReason = StopReasonStop
			result.Session = session.Snapshot()
			retResult = result
			return retResult, nil
		}
		stopTriggered, err := processToolBatch(ctx, session, result, calls, toolMap, iteration, metadata, nil, cfg)
		if err != nil {
			retErr = err
			return nil, retErr
		}
		if stopTriggered {
			result.StopReason = StopReasonToolSucceeded
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
	toolMap, err := prepareToolMap(&cfg)
	if err != nil {
		return nil, newEngineError(EngineErrorInvalidRequest, "run_config", err)
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

	for iteration := 1; iteration <= cfg.maxModelCalls; iteration++ {
		modelReq := buildModelRequest(session, metadata, cfg, iteration)
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
		result.RetryCount += resp.RetryCount
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

		calls := collectToolCalls(resp, result)
		if len(calls) == 0 {
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
		stopTriggered, err := processToolBatch(ctx, session, result, calls, toolMap, iteration, metadata, handler, cfg)
		if err != nil {
			retErr = err
			return nil, retErr
		}
		if stopTriggered {
			result.StopReason = StopReasonToolSucceeded
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

func buildModelRequest(session *Session, metadata map[string]any, cfg runConfig, iteration int) ModelRequest {
	var toolChoice *ToolChoice
	if cfg.toolChoice != nil {
		choice := AutoToolChoice()
		if iteration == 1 {
			choice = *cfg.toolChoice
		}
		toolChoice = &choice
	}
	return ModelRequest{
		Input:      inputWithRequestItems(session.ItemsView(), cfg.requestItems),
		Tools:      definitionsFromTools(cfg.tools),
		ToolChoice: toolChoice,
		Metadata:   cloneMap(metadata),
	}
}

func inputWithRequestItems(items, requestItems []model.InputItem) []model.InputItem {
	if len(requestItems) == 0 {
		return items
	}
	systemCount := 0
	for systemCount < len(items) {
		if _, ok := items[systemCount].(model.SystemMessageItem); !ok {
			break
		}
		systemCount++
	}
	out := make([]model.InputItem, 0, len(items)+len(requestItems))
	out = append(out, items[:systemCount]...)
	out = append(out, requestItems...)
	out = append(out, items[systemCount:]...)
	return out
}

func outputItemsToInputItems(items []model.OutputItem) []model.InputItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.InputItem, 0, len(items))
	for _, item := range items {
		switch current := item.(type) {
		case model.AssistantOutput:
			toolCalls := make([]model.ToolCallItem, 0, len(current.ToolCalls))
			for _, call := range current.ToolCalls {
				toolCalls = append(toolCalls, model.ToolCallItem{
					CallID:        call.CallID,
					Name:          call.Name,
					RawArguments:  call.RawArguments,
					ProviderState: cloneMap(call.ProviderState),
				})
			}
			out = append(out, model.AssistantMessageItem{
				Content:       current.Content,
				ToolCalls:     toolCalls,
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
