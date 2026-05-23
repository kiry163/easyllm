package model

type Usage struct {
	InputTokens       int            `json:"input_tokens"`
	OutputTokens      int            `json:"output_tokens"`
	TotalTokens       int            `json:"total_tokens"`
	CachedInputTokens int            `json:"cached_input_tokens"`
	Details           map[string]any `json:"details"`
}

type TextPart struct {
	Text string `json:"text"`
}

type InputItem interface {
	inputItem()
}

type OutputItem interface {
	outputItem()
}

type SystemMessageItem struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata"`
	Content  []TextPart     `json:"content"`
}

func (SystemMessageItem) inputItem() {}

type UserMessageItem struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata"`
	Content  []TextPart     `json:"content"`
}

func (UserMessageItem) inputItem() {}

type AssistantMessageItem struct {
	ID            string         `json:"id"`
	Metadata      map[string]any `json:"metadata"`
	Content       []TextPart     `json:"content"`
	ProviderState map[string]any `json:"provider_state"`
}

func (AssistantMessageItem) inputItem() {}

type ToolCallItem struct {
	ID            string         `json:"id"`
	CallID        string         `json:"call_id"`
	Name          string         `json:"name"`
	Arguments     map[string]any `json:"arguments"`
	RawArguments  string         `json:"raw_arguments"`
	Repaired      bool           `json:"repaired"`
	ProviderState map[string]any `json:"provider_state"`
	Metadata      map[string]any `json:"metadata"`
}

func (ToolCallItem) inputItem() {}

type ToolResultItem struct {
	ID       string         `json:"id"`
	CallID   string         `json:"call_id"`
	Name     string         `json:"name"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

func (ToolResultItem) inputItem() {}

type MessageOutput struct {
	Role          string         `json:"role"`
	Content       []TextPart     `json:"content"`
	ProviderState map[string]any `json:"provider_state"`
}

func (MessageOutput) outputItem() {}

type ToolCallOutput struct {
	CallID        string         `json:"call_id"`
	Name          string         `json:"name"`
	Arguments     map[string]any `json:"arguments"`
	RawArguments  string         `json:"raw_arguments"`
	Repaired      bool           `json:"repaired"`
	ProviderState map[string]any `json:"provider_state"`
}

func (ToolCallOutput) outputItem() {}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      *bool          `json:"strict"`
}

type ModelRequest struct {
	Model    string           `json:"model"`
	Input    []InputItem      `json:"input"`
	Tools    []ToolDefinition `json:"tools"`
	Options  map[string]any   `json:"options"`
	Metadata map[string]any   `json:"metadata"`
}

type ModelResponse struct {
	Output       []OutputItem `json:"output"`
	FinishReason string       `json:"finish_reason"`
	Usage        Usage        `json:"usage"`
	ResponseID   string       `json:"response_id"`
	Provider     string       `json:"provider"`
	Raw          any          `json:"raw"`
}
