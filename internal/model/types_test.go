package model

import "testing"

func TestToolCallOutputPreservesRawArguments(t *testing.T) {
	out := ToolCallOutput{
		CallID:       "call_1",
		Name:         "submit",
		RawArguments: "{\"value\":\"ok\"}",
	}

	if out.CallID != "call_1" {
		t.Fatalf("unexpected call id: %q", out.CallID)
	}
	if out.RawArguments != "{\"value\":\"ok\"}" {
		t.Fatalf("unexpected raw arguments: %q", out.RawArguments)
	}
}
