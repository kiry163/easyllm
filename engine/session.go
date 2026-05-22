package engine

import "github.com/kiry163/easyllm/provider"

type Message struct {
	Role       string
	Content    string
	ToolCallID string
}

type Session struct {
	ID           string
	Items        []provider.InputItem
	Usage        provider.Usage
	LastResponse *provider.ModelResponse
	Iteration    int
	Metadata     map[string]any
}

type SessionOption func(*Session)

type SessionSnapshot struct {
	ID           string
	Items        []provider.InputItem
	Usage        provider.Usage
	LastResponse *provider.ModelResponse
	Iteration    int
	Metadata     map[string]any
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
	s.Items = append(s.Items, provider.UserMessageItem{
		Content: []provider.TextPart{{Text: text}},
	})
}

func (s *Session) AppendItems(items ...provider.InputItem) {
	s.Items = append(s.Items, items...)
}

func (s *Session) ItemsView() []provider.InputItem {
	if len(s.Items) == 0 {
		return nil
	}
	out := make([]provider.InputItem, 0, len(s.Items))
	out = append(out, s.Items...)
	return out
}

func (s *Session) MessagesView() []Message {
	out := make([]Message, 0, len(s.Items))
	for _, item := range s.Items {
		switch current := item.(type) {
		case provider.SystemMessageItem:
			out = append(out, Message{Role: "system", Content: firstText(current.Content)})
		case provider.UserMessageItem:
			out = append(out, Message{Role: "user", Content: firstText(current.Content)})
		case provider.AssistantMessageItem:
			out = append(out, Message{Role: "assistant", Content: firstText(current.Content)})
		case provider.ToolResultItem:
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

func firstText(parts []provider.TextPart) string {
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
