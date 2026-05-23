package easyllm

import "fmt"

type PayloadError struct {
	Err     error
	Payload map[string]any
}

func (e *PayloadError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func NewPayloadError(message string, payload map[string]any) error {
	return &PayloadError{
		Err:     fmt.Errorf("%s", message),
		Payload: payload,
	}
}

func AsPayloadError(err error, target **PayloadError) bool {
	if err == nil || target == nil {
		return false
	}
	typed, ok := err.(*PayloadError)
	if !ok {
		return false
	}
	*target = typed
	return true
}
