package agent

import (
	"context"
	"testing"

	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/runtime"
)

type fakeClient struct{}

func (fakeClient) Generate(ctx context.Context, req provider.ModelRequest) (*provider.ModelResponse, error) {
	return &provider.ModelResponse{
		Output: []provider.OutputItem{
			provider.MessageOutput{
				Role: "assistant",
				Content: []provider.TextPart{
					{Text: "done"},
				},
			},
		},
		FinishReason: "stop",
	}, nil
}

func TestAgentRunAppendsInputAndReturnsOutput(t *testing.T) {
	a := Agent{
		Model:        "gpt-test",
		Instructions: "be helpful",
		Client:       fakeClient{},
		Runner:       &runtime.Runner{Client: fakeClient{}},
	}

	result, err := a.Run(context.Background(), RunRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OutputText != "done" {
		t.Fatalf("unexpected output: %q", result.OutputText)
	}
}
