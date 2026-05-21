package tool

import (
	"context"
	"testing"
)

type submitArgs struct {
	Value string `tool:"name=value,required"`
}

func TestNewStructRepairsQuotedJSONObjectArguments(t *testing.T) {
	submitTool, err := NewStruct("submit", "submit payload", func(ctx context.Context, call CallContext, args submitArgs) (Result, error) {
		if args.Value != "ok" {
			t.Fatalf("unexpected value: %q", args.Value)
		}
		return Result{Message: "done"}, nil
	})
	if err != nil {
		t.Fatalf("NewStruct returned error: %v", err)
	}

	_, err = submitTool.Invoke(context.Background(), CallContext{Name: "submit"}, map[string]any{
		"raw": "\"{\\\"value\\\":\\\"ok\\\"}\"",
	})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
}
