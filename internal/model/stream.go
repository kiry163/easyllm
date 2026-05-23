package model

type StreamEventType string

const (
	StreamEventMessageDelta StreamEventType = "message_delta"
	StreamEventToolStart    StreamEventType = "tool_start"
	StreamEventToolFinish   StreamEventType = "tool_finish"
	StreamEventDone         StreamEventType = "done"
)

type StreamEvent struct {
	Type     StreamEventType
	Text     string
	ToolName string
	Raw      any
}

type StreamHandler func(StreamEvent) error
