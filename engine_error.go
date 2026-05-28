package easyllm

import (
	"context"
	"errors"
	"fmt"
	"net"
)

type EngineErrorKind string

const (
	EngineErrorInvalidRequest EngineErrorKind = "invalid_request"
	EngineErrorModelRequest   EngineErrorKind = "model_request"
	EngineErrorTimeout        EngineErrorKind = "timeout"
	EngineErrorHook           EngineErrorKind = "hook"
	EngineErrorHandler        EngineErrorKind = "handler"
	EngineErrorInternal       EngineErrorKind = "internal"
)

type EngineError struct {
	Kind      EngineErrorKind
	Operation string
	Err       error
}

func (e *EngineError) Error() string {
	if e == nil {
		return ""
	}
	if e.Operation != "" && e.Err != nil {
		return fmt.Sprintf("engine %s %s: %v", e.Operation, e.Kind, e.Err)
	}
	if e.Err != nil {
		return fmt.Sprintf("engine %s: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("engine %s", e.Kind)
}

func (e *EngineError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newEngineError(kind EngineErrorKind, operation string, err error) error {
	if err == nil {
		return nil
	}
	var existing *EngineError
	if errors.As(err, &existing) {
		return err
	}
	return &EngineError{Kind: kind, Operation: operation, Err: err}
}

func modelEngineError(operation string, err error) error {
	if isTimeoutError(err) {
		return newEngineError(EngineErrorTimeout, operation, err)
	}
	return newEngineError(EngineErrorModelRequest, operation, err)
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrStreamFirstEventTimeout) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
