package serviceerrors

import (
	"errors"
	"testing"

	"github.com/eval-hub/eval-hub/internal/messages"
)

func TestNewServiceError(t *testing.T) {
	err := NewServiceError(messages.BadRequest, "Error", "test error")
	if err == nil {
		t.Fatal("NewServiceError returned nil")
	}
	if err.Error() == "" {
		t.Error("Error() returned empty string")
	}
	if err.ShouldRollback() {
		t.Error("new ServiceError should not have rollback by default")
	}
	if err.MessageCode() == nil {
		t.Error("MessageCode() returned nil")
	}
}

func TestWithRollback_Method(t *testing.T) {
	err := NewServiceError(messages.BadRequest, "Error", "test")
	wrapped := err.WithRollback()
	if !wrapped.ShouldRollback() {
		t.Error("WithRollback() should set rollback to true")
	}
	if wrapped.MessageCode() != err.MessageCode() {
		t.Error("WithRollback() should preserve message code")
	}
}

func TestWithRollback_Func_ServiceError(t *testing.T) {
	err := NewServiceError(messages.BadRequest, "Error", "test")
	wrapped := WithRollback(err)
	if wrapped == nil {
		t.Fatal("WithRollback returned nil")
	}
	if !wrapped.ShouldRollback() {
		t.Error("WithRollback should set rollback to true for ServiceError")
	}
	// Original should be unchanged
	if err.ShouldRollback() {
		t.Error("original ServiceError should not be modified")
	}
}

func TestWithRollback_Func_NonServiceError(t *testing.T) {
	origErr := errors.New("plain error")
	wrapped := WithRollback(origErr)
	if wrapped == nil {
		t.Fatal("WithRollback returned nil for plain error")
	}
	if !wrapped.ShouldRollback() {
		t.Error("WithRollback should set rollback to true")
	}
	if wrapped.Error() == "" {
		t.Error("wrapped error should contain message")
	}
	// Should wrap as InternalServerError
	if wrapped.MessageCode() == nil {
		t.Error("MessageCode should not be nil")
	}
}
