package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type Definition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type CallContext struct {
	CallID    string
	Name      string
	Iteration int
	Metadata  map[string]any
}

type Result struct {
	Message string
	Data    map[string]any
}

type Tool interface {
	Definition() Definition
	Invoke(context.Context, CallContext, map[string]any) (Result, error)
}

type StructHandler[T any] func(context.Context, CallContext, T) (Result, error)

type structTool[T any] struct {
	definition Definition
	handler    StructHandler[T]
}

func NewStruct[T any](name, description string, handler StructHandler[T]) (Tool, error) {
	parameters, err := SchemaFor[T]()
	if err != nil {
		return nil, err
	}
	return &structTool[T]{
		definition: Definition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
		handler: handler,
	}, nil
}

func (t *structTool[T]) Definition() Definition {
	return t.definition
}

func (t *structTool[T]) Invoke(ctx context.Context, call CallContext, raw map[string]any) (Result, error) {
	if raw == nil {
		raw = map[string]any{}
	}
	if rawText, ok := raw["raw"].(string); ok && len(raw) == 1 {
		decoded, _, err := DecodeJSONObjectString(rawText)
		if err == nil {
			raw = decoded
		}
	}
	args, err := BindArgs[T](raw)
	if err != nil {
		payload := map[string]any{
			"status":  "error",
			"code":    "invalid_arguments",
			"message": err.Error(),
		}
		return Result{}, &PayloadError{Err: err, Payload: payload}
	}
	return t.handler(ctx, call, args)
}

func DefinitionsFromTools(tools []Tool) []Definition {
	if len(tools) == 0 {
		return nil
	}
	definitions := make([]Definition, 0, len(tools))
	for _, current := range tools {
		if current == nil {
			continue
		}
		definitions = append(definitions, current.Definition())
	}
	return definitions
}

func EncodeToolSuccess(toolName string, result Result) string {
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

func EncodeToolError(toolName string, err error) string {
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
