package easyllm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kiry163/easyllm"
)

type submitArgs struct {
	Value string `json:"value" tool:"required"`
}

type submitRunner struct{}

func (submitRunner) Name() string {
	return "submit"
}

func (submitRunner) Description() string {
	return "submit payload"
}

func (submitRunner) Run(ctx context.Context, call easyllm.ToolCallContext, args submitArgs) (easyllm.ToolResult, error) {
	return easyllm.ToolResult{
		Message: "done",
		Data:    map[string]any{"value": args.Value},
	}, nil
}

type guidedArgs struct {
	Answer  string `json:"answer" tool:"required,minLength=2"`
	Success bool   `json:"success" tool:"required"`
}

type guidedRunner struct {
	call easyllm.ToolCallContext
}

func (*guidedRunner) Name() string        { return "guided" }
func (*guidedRunner) Description() string { return "guided repair" }

func (r *guidedRunner) Run(ctx context.Context, call easyllm.ToolCallContext, args guidedArgs) (easyllm.ToolResult, error) {
	r.call = call
	return easyllm.ToolResult{Data: map[string]any{
		"answer":  args.Answer,
		"success": args.Success,
	}}, nil
}

func TestNewRepairsRawJSONObjectArguments(t *testing.T) {
	submitTool, err := easyllm.NewTool[submitArgs](submitRunner{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	result, err := submitTool.Invoke(context.Background(), easyllm.ToolCallContext{
		Name:         "submit",
		RawArguments: `{"value":"ok"}`,
	})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result.Data["value"] != "ok" {
		t.Fatalf("unexpected value: %#v", result.Data["value"])
	}
}

func TestNewUsesRepairIntoWithRawArguments(t *testing.T) {
	runner := &guidedRunner{}
	tool, err := easyllm.NewTool[guidedArgs](runner)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}
	raw := `{"answer":"foo", "unknown": 30, bar","success":true}`
	result, err := tool.Invoke(
		context.Background(),
		easyllm.ToolCallContext{Name: "guided", RawArguments: raw},
	)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result.Data["answer"] != `foo", "unknown": 30, bar` || result.Data["success"] != true {
		t.Fatalf("unexpected repaired result: %#v", result.Data)
	}
	if !runner.call.Repaired {
		t.Fatal("expected repaired call context")
	}
	if runner.call.RepairStrategy == "" {
		t.Fatal("expected repair strategy in call context")
	}
}

func TestInvokeDoesNotFallbackAfterRepairFailure(t *testing.T) {
	runner := &guidedRunner{}
	tool, err := easyllm.NewTool[guidedArgs](runner)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}
	if _, err := tool.Invoke(context.Background(), easyllm.ToolCallContext{RawArguments: "not a JSON object"}); err == nil {
		t.Fatal("expected invalid arguments error")
	}
	if runner.call.Name != "" {
		t.Fatalf("runner executed after repair failure: %+v", runner.call)
	}
}

func TestUnknownArgumentPolicies(t *testing.T) {
	raw := `{"answer":"ok","success":true,"extra":1}`

	t.Run("reject by default", func(t *testing.T) {
		tool, err := easyllm.NewTool[guidedArgs](&guidedRunner{})
		if err != nil {
			t.Fatalf("NewTool returned error: %v", err)
		}
		_, err = tool.Invoke(context.Background(), easyllm.ToolCallContext{RawArguments: raw})
		if err == nil || !strings.Contains(err.Error(), "extra is not allowed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("warn and continue", func(t *testing.T) {
		runner := &guidedRunner{}
		tool, err := easyllm.NewTool[guidedArgs](runner, easyllm.WithUnknownArguments(easyllm.UnknownArgumentsWarn))
		if err != nil {
			t.Fatalf("NewTool returned error: %v", err)
		}
		if _, err := tool.Invoke(context.Background(), easyllm.ToolCallContext{RawArguments: raw}); err != nil {
			t.Fatalf("Invoke returned error: %v", err)
		}
		if len(runner.call.ArgumentIssues) != 1 || runner.call.ArgumentIssues[0].Path != "extra" {
			t.Fatalf("unexpected argument issues: %#v", runner.call.ArgumentIssues)
		}
	})

	t.Run("ignore and continue", func(t *testing.T) {
		runner := &guidedRunner{}
		tool, err := easyllm.NewTool[guidedArgs](runner, easyllm.WithUnknownArguments(easyllm.UnknownArgumentsIgnore))
		if err != nil {
			t.Fatalf("NewTool returned error: %v", err)
		}
		if _, err := tool.Invoke(context.Background(), easyllm.ToolCallContext{RawArguments: raw}); err != nil {
			t.Fatalf("Invoke returned error: %v", err)
		}
		if len(runner.call.ArgumentIssues) != 0 {
			t.Fatalf("unexpected argument issues: %#v", runner.call.ArgumentIssues)
		}
	})
}

func TestRepairIntoArgsUsesTypedValidation(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "required", raw: `{"success":true}`, wantErr: "answer is required"},
		{name: "constraint", raw: `{"answer":"x","success":true}`, wantErr: "answer length must be >= 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := easyllm.NewTool[guidedArgs](&guidedRunner{})
			if err != nil {
				t.Fatalf("NewTool returned error: %v", err)
			}
			_, err = tool.Invoke(context.Background(), easyllm.ToolCallContext{RawArguments: tt.raw})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSchemaForUsesJSONTagsAndRejectsLegacyNames(t *testing.T) {
	type jsonArgs struct {
		UserID string `json:"user_id" tool:"required"`
	}
	schema, err := easyllm.SchemaFor[jsonArgs]()
	if err != nil {
		t.Fatalf("SchemaFor returned error: %v", err)
	}
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["user_id"]; !ok {
		t.Fatalf("JSON field missing from schema: %#v", properties)
	}

	type legacyArgs struct {
		UserID string `tool:"name=user_id,required"`
	}
	if _, err := easyllm.SchemaFor[legacyArgs](); err == nil || !strings.Contains(err.Error(), "use a json tag") {
		t.Fatalf("unexpected legacy tag error: %v", err)
	}

	type stringOptionArgs struct {
		Count int `json:"count,string"`
	}
	if _, err := easyllm.SchemaFor[stringOptionArgs](); err == nil || !strings.Contains(err.Error(), "json option string") {
		t.Fatalf("unexpected json string option error: %v", err)
	}

	type embedded struct {
		Value string `json:"value"`
	}
	type anonymousArgs struct {
		embedded
	}
	if _, err := easyllm.SchemaFor[anonymousArgs](); err == nil || !strings.Contains(err.Error(), "anonymous tool field") {
		t.Fatalf("unexpected anonymous field error: %v", err)
	}
}

func TestNewRejectsInvalidUnknownArgumentPolicy(t *testing.T) {
	_, err := easyllm.NewTool[submitArgs](submitRunner{}, easyllm.WithUnknownArguments("invalid"))
	if err == nil || !strings.Contains(err.Error(), "invalid unknown argument policy") {
		t.Fatalf("unexpected policy error: %v", err)
	}
}

func TestNewUsesToolMetadataMethods(t *testing.T) {
	submitTool, err := easyllm.NewTool[submitArgs](submitRunner{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	def := submitTool.Definition()
	if def.Name != "submit" || def.Description != "submit payload" {
		t.Fatalf("unexpected definition: %+v", def)
	}
}

func TestNewSupportsStrictOption(t *testing.T) {
	submitTool, err := easyllm.NewTool[submitArgs](submitRunner{}, easyllm.WithStrict(true))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	def := submitTool.Definition()
	if def.Strict == nil || *def.Strict != true {
		t.Fatalf("expected strict=true, got %#v", def.Strict)
	}
}

func TestSchemaForSupportsEnumRangeAndClosedObjects(t *testing.T) {
	type forecastArgs struct {
		Unit string `json:"unit" tool:"required,desc=Temperature unit,enum=celsius|fahrenheit"`
		Days int    `json:"days" tool:"desc=Forecast days,minimum=1,maximum=10"`
	}

	schema, err := easyllm.SchemaFor[forecastArgs]()
	if err != nil {
		t.Fatalf("SchemaFor returned error: %v", err)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("expected additionalProperties=false, got %#v", schema["additionalProperties"])
	}
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "unit" {
		t.Fatalf("unexpected required fields: %#v", schema["required"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected properties: %#v", schema["properties"])
	}
	unit, ok := properties["unit"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected unit schema: %#v", properties["unit"])
	}
	if unit["description"] != "Temperature unit" {
		t.Fatalf("unexpected unit description: %#v", unit["description"])
	}
	enum, ok := unit["enum"].([]any)
	if !ok || len(enum) != 2 || enum[0] != "celsius" || enum[1] != "fahrenheit" {
		t.Fatalf("unexpected enum: %#v", unit["enum"])
	}
	days, ok := properties["days"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected days schema: %#v", properties["days"])
	}
	if days["minimum"] != float64(1) || days["maximum"] != float64(10) {
		t.Fatalf("unexpected range: minimum=%#v maximum=%#v", days["minimum"], days["maximum"])
	}
}

func TestSchemaForSupportsStringAndArrayLengthConstraints(t *testing.T) {
	type profileArgs struct {
		Nickname string   `json:"nickname" tool:"required,minLength=2,maxLength=12"`
		Hobbies  []string `json:"hobbies" tool:"required,minItems=1,maxItems=5"`
	}

	schema, err := easyllm.SchemaFor[profileArgs]()
	if err != nil {
		t.Fatalf("SchemaFor returned error: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected properties: %#v", schema["properties"])
	}
	nickname, ok := properties["nickname"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected nickname schema: %#v", properties["nickname"])
	}
	if nickname["minLength"] != 2 || nickname["maxLength"] != 12 {
		t.Fatalf("unexpected string length constraints: minLength=%#v maxLength=%#v", nickname["minLength"], nickname["maxLength"])
	}
	hobbies, ok := properties["hobbies"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected hobbies schema: %#v", properties["hobbies"])
	}
	if hobbies["minItems"] != 1 || hobbies["maxItems"] != 5 {
		t.Fatalf("unexpected array length constraints: minItems=%#v maxItems=%#v", hobbies["minItems"], hobbies["maxItems"])
	}
}

func TestBindArgsValidatesEnumRangeAndUnknownFields(t *testing.T) {
	type forecastArgs struct {
		Unit string `json:"unit" tool:"required,enum=celsius|fahrenheit"`
		Days int    `json:"days" tool:"minimum=1,maximum=10"`
	}

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr string
	}{
		{
			name: "enum",
			raw: map[string]any{
				"unit": "kelvin",
				"days": float64(3),
			},
			wantErr: "unit must be one of",
		},
		{
			name: "minimum",
			raw: map[string]any{
				"unit": "celsius",
				"days": float64(0),
			},
			wantErr: "days must be >= 1",
		},
		{
			name: "maximum",
			raw: map[string]any{
				"unit": "celsius",
				"days": float64(11),
			},
			wantErr: "days must be <= 10",
		},
		{
			name: "unknown field",
			raw: map[string]any{
				"unit":  "celsius",
				"extra": "ignored",
			},
			wantErr: "extra is not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := easyllm.BindArgs[forecastArgs](tt.raw)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestBindArgsValidatesStringAndArrayLengthConstraints(t *testing.T) {
	type profileArgs struct {
		Nickname string   `json:"nickname" tool:"required,minLength=2,maxLength=12"`
		Hobbies  []string `json:"hobbies" tool:"required,minItems=1,maxItems=2"`
	}

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr string
	}{
		{
			name: "string minLength",
			raw: map[string]any{
				"nickname": "A",
				"hobbies":  []any{"football"},
			},
			wantErr: "nickname length must be >= 2",
		},
		{
			name: "string maxLength",
			raw: map[string]any{
				"nickname": "abcdefghijklmn",
				"hobbies":  []any{"football"},
			},
			wantErr: "nickname length must be <= 12",
		},
		{
			name: "array minItems",
			raw: map[string]any{
				"nickname": "Alice",
				"hobbies":  []any{},
			},
			wantErr: "hobbies item count must be >= 1",
		},
		{
			name: "array maxItems",
			raw: map[string]any{
				"nickname": "Alice",
				"hobbies":  []any{"football", "painting", "robotics"},
			},
			wantErr: "hobbies item count must be <= 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := easyllm.BindArgs[profileArgs](tt.raw)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
