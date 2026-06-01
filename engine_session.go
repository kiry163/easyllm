package easyllm

import "github.com/kiry163/easyllm/internal/model"

type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}

type Session struct {
	ID           string
	Items        []model.InputItem
	Usage        model.Usage
	LastResponse *ModelResponse
	Iteration    int
	Metadata     map[string]any
}

type SessionOption func(*Session)

type SessionSnapshot struct {
	ID           string            `json:"id"`
	Items        []model.InputItem `json:"items"`
	Usage        model.Usage       `json:"usage"`
	LastResponse *ModelResponse    `json:"last_response"`
	Iteration    int               `json:"iteration"`
	Metadata     map[string]any    `json:"metadata"`
}

func NewSession(opts ...SessionOption) *Session {
	session := &Session{
		Metadata: map[string]any{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(session)
		}
	}
	return session
}

func (s *Session) AppendUserText(text string) {
	s.Items = append(s.Items, model.UserMessageItem{
		Content: []model.TextPart{{Text: text}},
	})
}

func (s *Session) AppendItems(items ...model.InputItem) {
	s.Items = append(s.Items, items...)
}

func (s *Session) ItemsView() []model.InputItem {
	if len(s.Items) == 0 {
		return nil
	}
	out := make([]model.InputItem, 0, len(s.Items))
	out = append(out, s.Items...)
	return out
}

func (s *Session) MessagesView() []Message {
	out := make([]Message, 0, len(s.Items))
	for _, item := range s.Items {
		switch current := item.(type) {
		case model.SystemMessageItem:
			out = append(out, Message{Role: "system", Content: firstText(current.Content)})
		case model.UserMessageItem:
			out = append(out, Message{Role: "user", Content: firstText(current.Content)})
		case model.AssistantMessageItem:
			out = append(out, Message{Role: "assistant", Content: firstText(current.Content)})
			for _, call := range current.ToolCalls {
				out = append(out, Message{Role: "assistant", Content: "", ToolCallID: call.CallID})
			}
		case model.ToolResultItem:
			out = append(out, Message{Role: "tool", Content: current.Content, ToolCallID: current.CallID})
		}
	}
	return out
}

func (s *Session) Snapshot() SessionSnapshot {
	return SessionSnapshot{
		ID:           s.ID,
		Items:        s.ItemsView(),
		Usage:        s.Usage,
		LastResponse: s.LastResponse,
		Iteration:    s.Iteration,
		Metadata:     cloneMap(s.Metadata),
	}
}

func firstText(parts []model.TextPart) string {
	if len(parts) == 0 {
		return ""
	}
	return parts[0].Text
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
