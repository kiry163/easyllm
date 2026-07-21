package easyllm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kiry163/easyllm/internal/model"
	repairjson "github.com/kiry163/jsonrepair"
)

type ToolDefinition = model.ToolDefinition

// UnknownArgumentPolicy controls how tool invocation handles fields that are
// not present in the argument struct.
type UnknownArgumentPolicy = model.UnknownArgumentPolicy

const (
	UnknownArgumentsReject = model.UnknownArgumentsReject
	UnknownArgumentsWarn   = model.UnknownArgumentsWarn
	UnknownArgumentsIgnore = model.UnknownArgumentsIgnore
)

// ToolArgumentIssue records a non-blocking tool argument validation issue.
type ToolArgumentIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ToolCallContext struct {
	CallID         string              `json:"call_id"`
	Name           string              `json:"name"`
	Iteration      int                 `json:"iteration"`
	Metadata       map[string]any      `json:"metadata"`
	RawArguments   string              `json:"raw_arguments"`
	Repaired       bool                `json:"repaired"`
	RepairStrategy string              `json:"repair_strategy"`
	ArgumentIssues []ToolArgumentIssue `json:"argument_issues"`
}

type ToolResult struct {
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

type Tool interface {
	Definition() ToolDefinition
	Invoke(context.Context, ToolCallContext) (ToolResult, error)
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

// WithUnknownArguments configures unknown-field handling for a tool.
func WithUnknownArguments(policy UnknownArgumentPolicy) ToolOption {
	return func(def *ToolDefinition) {
		def.UnknownArgumentPolicy = policy
	}
}

func NewTool[Args any, Runner toolRunner[Args]](runner Runner, opts ...ToolOption) (Tool, error) {
	parameters, err := SchemaFor[Args]()
	if err != nil {
		return nil, err
	}
	definition := ToolDefinition{
		Name:                  runner.Name(),
		Description:           runner.Description(),
		Parameters:            parameters,
		UnknownArgumentPolicy: UnknownArgumentsReject,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&definition)
		}
	}
	if !validUnknownArgumentPolicy(definition.UnknownArgumentPolicy) {
		return nil, fmt.Errorf("invalid unknown argument policy %q", definition.UnknownArgumentPolicy)
	}
	return &runnableTool[Args, Runner]{
		definition: definition,
		runner:     runner,
	}, nil
}

func (t *runnableTool[Args, Runner]) Definition() ToolDefinition {
	return t.definition
}

func (t *runnableTool[Args, Runner]) Invoke(ctx context.Context, call ToolCallContext) (ToolResult, error) {
	raw := call.RawArguments
	if raw == "" {
		raw = "{}"
	}
	var args Args
	report, err := repairjson.RepairInto(raw, &args)
	if err != nil {
		return invalidToolArguments(err)
	}
	var repairedRaw map[string]any
	if report == nil || json.Unmarshal([]byte(report.RepairedJSON), &repairedRaw) != nil {
		return invalidToolArguments(fmt.Errorf("repaired tool arguments are not a JSON object"))
	}
	issues, validateErr := validateTypedArgs(args, repairedRaw, t.definition.UnknownArgumentPolicy)
	call.Repaired = report.Strategy != repairjson.StrategyDirect
	call.RepairStrategy = string(report.Strategy)
	call.ArgumentIssues = append(call.ArgumentIssues, issues...)
	if validateErr != nil {
		return invalidToolArguments(validateErr)
	}
	return t.runner.Run(ctx, call, args)
}

func validUnknownArgumentPolicy(policy UnknownArgumentPolicy) bool {
	switch policy {
	case UnknownArgumentsReject, UnknownArgumentsWarn, UnknownArgumentsIgnore:
		return true
	default:
		return false
	}
}

func invalidToolArguments(err error) (ToolResult, error) {
	payload := map[string]any{
		"status":  "error",
		"code":    "invalid_arguments",
		"message": err.Error(),
	}
	return ToolResult{}, &PayloadError{Err: err, Payload: payload}
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
