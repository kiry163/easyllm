package model

type Usage struct {
	InputTokens       int            `json:"input_tokens"`
	OutputTokens      int            `json:"output_tokens"`
	TotalTokens       int            `json:"total_tokens"`
	CachedInputTokens int            `json:"cached_input_tokens"`
	Details           map[string]any `json:"details"`
}

type ContentPartType string

const (
	ContentPartTypeText  ContentPartType = "text"
	ContentPartTypeImage ContentPartType = "image"
)

type ImageDetail string

const (
	ImageDetailAuto ImageDetail = "auto"
	ImageDetailLow  ImageDetail = "low"
	ImageDetailHigh ImageDetail = "high"
)

type ContentPart struct {
	Type     ContentPartType `json:"type,omitempty"`
	Text     string          `json:"text,omitempty"`
	ImageURL string          `json:"image_url,omitempty"`
	Detail   ImageDetail     `json:"detail,omitempty"`
}

type TextPart = ContentPart

func NewTextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartTypeText, Text: text}
}

func NewImagePart(imageURL string, detail ImageDetail) ContentPart {
	return ContentPart{Type: ContentPartTypeImage, ImageURL: imageURL, Detail: detail}
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
	ToolCalls     []ToolCallItem `json:"tool_calls"`
	ProviderState map[string]any `json:"provider_state"`
}

func (AssistantMessageItem) inputItem() {}

type ToolCallItem struct {
	ID            string         `json:"id"`
	CallID        string         `json:"call_id"`
	Name          string         `json:"name"`
	RawArguments  string         `json:"raw_arguments"`
	ProviderState map[string]any `json:"provider_state"`
	Metadata      map[string]any `json:"metadata"`
}

type ToolResultItem struct {
	ID       string         `json:"id"`
	CallID   string         `json:"call_id"`
	Name     string         `json:"name"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

func (ToolResultItem) inputItem() {}

type AssistantOutput struct {
	Role          string           `json:"role"`
	Content       []TextPart       `json:"content"`
	ToolCalls     []ToolCallOutput `json:"tool_calls"`
	ProviderState map[string]any   `json:"provider_state"`
}

func (AssistantOutput) outputItem() {}

type ToolCallOutput struct {
	CallID        string         `json:"call_id"`
	Name          string         `json:"name"`
	RawArguments  string         `json:"raw_arguments"`
	ProviderState map[string]any `json:"provider_state"`
}

type ToolDefinition struct {
	Name                  string                `json:"name"`
	Description           string                `json:"description"`
	Parameters            map[string]any        `json:"parameters"`
	Strict                *bool                 `json:"strict"`
	UnknownArgumentPolicy UnknownArgumentPolicy `json:"-"`
}

type UnknownArgumentPolicy string

const (
	UnknownArgumentsReject UnknownArgumentPolicy = "reject"
	UnknownArgumentsWarn   UnknownArgumentPolicy = "warn"
	UnknownArgumentsIgnore UnknownArgumentPolicy = "ignore"
)

type ToolChoiceMode string

const (
	ToolChoiceModeAuto     ToolChoiceMode = "auto"
	ToolChoiceModeRequired ToolChoiceMode = "required"
	ToolChoiceModeNone     ToolChoiceMode = "none"
	ToolChoiceModeNamed    ToolChoiceMode = "named"
)

// ToolChoice controls how a model may select tools. Its fields are private so
// callers construct only supported choices through the public helpers.
type ToolChoice struct {
	mode     ToolChoiceMode
	toolName string
}

func NewToolChoice(mode ToolChoiceMode, toolName string) ToolChoice {
	return ToolChoice{mode: mode, toolName: toolName}
}

func (c ToolChoice) Mode() ToolChoiceMode {
	return c.mode
}

func (c ToolChoice) ToolName() string {
	return c.toolName
}

type ModelRequest struct {
	Model      string           `json:"model"`
	Input      []InputItem      `json:"input"`
	Tools      []ToolDefinition `json:"tools"`
	ToolChoice *ToolChoice      `json:"tool_choice,omitempty"`
	Options    map[string]any   `json:"options"`
	Metadata   map[string]any   `json:"metadata"`
}

type ModelResponse struct {
	Output       []OutputItem `json:"output"`
	FinishReason string       `json:"finish_reason"`
	Usage        Usage        `json:"usage"`
	ResponseID   string       `json:"response_id"`
	Provider     string       `json:"provider"`
	RetryCount   int          `json:"retry_count"`
	Raw          any          `json:"raw"`
}
