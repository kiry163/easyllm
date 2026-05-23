package easyllm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kiry163/easyllm/internal/jsonrepair"
	"github.com/kiry163/easyllm/internal/model"
)

type ToolDefinition = model.ToolDefinition

type ToolCallContext struct {
	CallID    string
	Name      string
	Iteration int
	Metadata  map[string]any
}

type ToolResult struct {
	Message string
	Data    map[string]any
}

type Tool interface {
	Definition() ToolDefinition
	Invoke(context.Context, ToolCallContext, map[string]any) (ToolResult, error)
}

type runnerMetadata interface {
	Name() string
	Description() string
}

type toolRunner[Args any] interface {
	runnerMetadata
	Run(context.Context, ToolCallContext, Args) (ToolResult, error)
}

type runnableTool[Args any, Runner toolRunner[Args]] struct {
	definition ToolDefinition
	runner     Runner
}

type ToolOption func(*ToolDefinition)

func WithStrict(strict bool) ToolOption {
	return func(def *ToolDefinition) {
		def.Strict = &strict
	}
}

func NewTool[Args any, Runner toolRunner[Args]](runner Runner, opts ...ToolOption) (Tool, error) {
	parameters, err := SchemaFor[Args]()
	if err != nil {
		return nil, err
	}
	definition := ToolDefinition{
		Name:        runner.Name(),
		Description: runner.Description(),
		Parameters:  parameters,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&definition)
		}
	}
	return &runnableTool[Args, Runner]{
		definition: definition,
		runner:     runner,
	}, nil
}

func (t *runnableTool[Args, Runner]) Definition() ToolDefinition {
	return t.definition
}

func (t *runnableTool[Args, Runner]) Invoke(ctx context.Context, call ToolCallContext, raw map[string]any) (ToolResult, error) {
	if raw == nil {
		raw = map[string]any{}
	}
	if rawText, ok := raw["raw"].(string); ok && len(raw) == 1 {
		decoded, _, err := jsonrepair.DecodeJSONObjectString(rawText)
		if err == nil {
			raw = decoded
		}
	}
	args, err := BindArgs[Args](raw)
	if err != nil {
		payload := map[string]any{
			"status":  "error",
			"code":    "invalid_arguments",
			"message": err.Error(),
		}
		return ToolResult{}, &PayloadError{Err: err, Payload: payload}
	}
	return t.runner.Run(ctx, call, args)
}

func definitionsFromTools(tools []Tool) []ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	definitions := make([]ToolDefinition, 0, len(tools))
	for _, current := range tools {
		if current == nil {
			continue
		}
		definitions = append(definitions, current.Definition())
	}
	return definitions
}

func encodeToolSuccess(toolName string, result ToolResult) string {
	payload := map[string]any{
		"status": "ok",
		"tool":   toolName,
	}
	if result.Message != "" {
		payload["message"] = result.Message
	}
	for key, value := range result.Data {
		payload[key] = value
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func encodeToolError(toolName string, err error) string {
	payload := map[string]any{
		"status":  "error",
		"tool":    toolName,
		"message": err.Error(),
	}
	var structured *PayloadError
	if AsPayloadError(err, &structured) && structured != nil {
		for key, value := range structured.Payload {
			payload[key] = value
		}
		if _, ok := payload["tool"]; !ok {
			payload["tool"] = toolName
		}
		if _, ok := payload["message"]; !ok && structured.Err != nil {
			payload["message"] = structured.Err.Error()
		}
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Sprintf(`{"status":"error","tool":%q,"message":%q}`, toolName, err.Error())
	}
	return string(data)
}
