package host

import (
	"errors"
	"fmt"
	"strings"
)

// Stage identifies a lifecycle boundary owned by the Runtime host.
type Stage string

const (
	StageConfig       Stage = "config"
	StageRegistration Stage = "registration"
	StageHandler      Stage = "handler"
	StageServe        Stage = "serve"
	StageShutdown     Stage = "shutdown"
)

// Error adds a stable lifecycle stage without exposing the wrapped error's
// message by default. The wrapped error remains available to errors.Is/As so
// callers can make an explicit local diagnostic decision.
type Error struct {
	stage   Stage
	message string
	wrapped error
}

// Wrap creates a staged host error. message must be a static, provider-safe
// description chosen by the host integration; it must not contain secrets,
// request payloads, or raw dependency responses.
func Wrap(stage Stage, message string, err error) error {
	if err == nil {
		return nil
	}
	if !validStage(stage) || !validMessage(message) {
		return &Error{stage: StageConfig, message: "invalid host error", wrapped: err}
	}
	return &Error{stage: stage, message: message, wrapped: err}
}

func validMessage(message string) bool {
	if message == "" || len(message) > 128 || strings.TrimSpace(message) != message {
		return false
	}
	for _, character := range message {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("agent host %s: %s", e.stage, e.message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.wrapped
}

// Stage returns the host lifecycle stage associated with err.
func (e *Error) Stage() Stage {
	if e == nil {
		return ""
	}
	return e.stage
}

// StageOf extracts the first staged host error from an error chain.
func StageOf(err error) (Stage, bool) {
	var staged *Error
	if !errors.As(err, &staged) || staged == nil {
		return "", false
	}
	return staged.Stage(), true
}

func validStage(stage Stage) bool {
	switch stage {
	case StageConfig, StageRegistration, StageHandler, StageServe, StageShutdown:
		return true
	default:
		return false
	}
}
