package model

import "testing"

func TestToolCallOutputPreservesRawArgumentsMetadata(t *testing.T) {
	out := ToolCallOutput{
		CallID:       "call_1",
		Name:         "submit",
		Arguments:    map[string]any{"value": "ok"},
		RawArguments: "{\"value\":\"ok\"}",
		Repaired:     true,
	}

	if out.CallID != "call_1" {
		t.Fatalf("unexpected call id: %q", out.CallID)
	}
	if out.Arguments["value"] != "ok" {
		t.Fatalf("unexpected arguments: %#v", out.Arguments)
	}
	if !out.Repaired {
		t.Fatalf("expected repaired flag to be preserved")
	}
}
