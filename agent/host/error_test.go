package host

import (
	"errors"
	"testing"
)

func TestWrapPreservesCauseButKeepsMessageSafe(t *testing.T) {
	secret := errors.New("token=secret")
	wrapped := Wrap(StageRegistration, "register runtime", secret)
	if wrapped == nil || wrapped.Error() != "agent host registration: register runtime" {
		t.Fatalf("wrapped error=%v", wrapped)
	}
	if !errors.Is(wrapped, secret) {
		t.Fatal("wrapped cause was not preserved")
	}
	if stage, ok := StageOf(wrapped); !ok || stage != StageRegistration {
		t.Fatalf("StageOf() = %q, %v", stage, ok)
	}
	if errors.As(wrapped, new(*Error)) == false {
		t.Fatal("wrapped host error was not inspectable")
	}
	if contains := wrapped.Error(); contains == "" || contains == secret.Error() {
		t.Fatalf("unsafe public error=%q", contains)
	}
}

func TestWrapNilReturnsNil(t *testing.T) {
	if Wrap(StageServe, "serve runtime", nil) != nil {
		t.Fatal("Wrap(nil) returned an error")
	}
}

func TestWrapRejectsUnknownStageWithoutExposingCause(t *testing.T) {
	wrapped := Wrap(Stage("provider-secret"), "invalid stage", errors.New("token=secret"))
	if wrapped.Error() != "agent host config: invalid host error" {
		t.Fatalf("wrapped error=%v", wrapped)
	}
	unsafe := Wrap(StageServe, "serve runtime\ntoken=secret", errors.New("cause"))
	if unsafe.Error() != "agent host config: invalid host error" {
		t.Fatalf("unsafe wrapped error=%v", unsafe)
	}
}
