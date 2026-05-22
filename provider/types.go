package provider

import "github.com/kiry163/easyllm/tool"

type Usage struct {
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	Details           map[string]any
}

type TextPart struct {
	Text string
}

type InputItem interface {
	inputItem()
}

type OutputItem interface {
	outputItem()
}

type SystemMessageItem struct {
	ID       string
	Metadata map[string]any
	Content  []TextPart
}

func (SystemMessageItem) inputItem() {}

type UserMessageItem struct {
	ID       string
	Metadata map[string]any
	Content  []TextPart
}

func (UserMessageItem) inputItem() {}

type AssistantMessageItem struct {
	ID       string
	Metadata map[string]any
	Content  []TextPart
}

func (AssistantMessageItem) inputItem() {}

type ToolCallItem struct {
	ID           string
	CallID       string
	Name         string
	Arguments    map[string]any
	RawArguments string
	Repaired     bool
	Metadata     map[string]any
}

func (ToolCallItem) inputItem() {}

type ToolResultItem struct {
	ID       string
	CallID   string
	Name     string
	Content  string
	Metadata map[string]any
}

func (ToolResultItem) inputItem() {}

type MessageOutput struct {
	Role    string
	Content []TextPart
}

func (MessageOutput) outputItem() {}

type ToolCallOutput struct {
	CallID       string
	Name         string
	Arguments    map[string]any
	RawArguments string
	Repaired     bool
}

func (ToolCallOutput) outputItem() {}

type ModelRequest struct {
	Model    string
	Input    []InputItem
	Tools    []tool.Definition
	Options  map[string]any
	Metadata map[string]any
}

type ModelResponse struct {
	Output       []OutputItem
	FinishReason string
	Usage        Usage
	ResponseID   string
	Provider     string
	Raw          any
}
