package tool

import (
	"context"
	"strings"
	"testing"
)

type submitArgs struct {
	Value string `tool:"name=value,required"`
}

type submitRunner struct{}

func (submitRunner) Name() string {
	return "submit"
}

func (submitRunner) Description() string {
	return "submit payload"
}

func (submitRunner) Run(ctx context.Context, call CallContext, args submitArgs) (Result, error) {
	return Result{
		Message: "done",
		Data:    map[string]any{"value": args.Value},
	}, nil
}

func TestNewRepairsQuotedJSONObjectArguments(t *testing.T) {
	submitTool, err := New[submitArgs](submitRunner{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	result, err := submitTool.Invoke(context.Background(), CallContext{Name: "submit"}, map[string]any{
		"raw": "\"{\\\"value\\\":\\\"ok\\\"}\"",
	})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if result.Data["value"] != "ok" {
		t.Fatalf("unexpected value: %#v", result.Data["value"])
	}
}

func TestNewUsesToolMetadataMethods(t *testing.T) {
	submitTool, err := New[submitArgs](submitRunner{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	def := submitTool.Definition()
	if def.Name != "submit" || def.Description != "submit payload" {
		t.Fatalf("unexpected definition: %+v", def)
	}
}

func TestSchemaForSupportsEnumRangeAndClosedObjects(t *testing.T) {
	type forecastArgs struct {
		Unit string `tool:"name=unit,required,desc=Temperature unit,enum=celsius|fahrenheit"`
		Days int    `tool:"name=days,desc=Forecast days,minimum=1,maximum=10"`
	}

	schema, err := SchemaFor[forecastArgs]()
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

func TestBindArgsValidatesEnumRangeAndUnknownFields(t *testing.T) {
	type forecastArgs struct {
		Unit string `tool:"name=unit,required,enum=celsius|fahrenheit"`
		Days int    `tool:"name=days,minimum=1,maximum=10"`
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
			_, err := BindArgs[forecastArgs](tt.raw)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
