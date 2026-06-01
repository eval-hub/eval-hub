package mlflowclient

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	err := &APIError{
		StatusCode:   404,
		ResponseBody: `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`,
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "RESOURCE_DOES_NOT_EXIST") {
		t.Fatalf("Error() = %q", msg)
	}
}

func TestIsResourceDoesNotExistError(t *testing.T) {
	t.Parallel()

	t.Run("structured error", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode: 404,
			MLFlowError: &MLFlowError{
				ErrorCode: "RESOURCE_DOES_NOT_EXIST",
			},
		}
		if !IsResourceDoesNotExistError(err) {
			t.Fatal("expected true")
		}
	})

	t.Run("response body fallback", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   404,
			ResponseBody: `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`,
		}
		if !IsResourceDoesNotExistError(err) {
			t.Fatal("expected true")
		}
	})

	t.Run("wrong status", func(t *testing.T) {
		t.Parallel()
		err := &APIError{StatusCode: 500, MLFlowError: &MLFlowError{ErrorCode: "RESOURCE_DOES_NOT_EXIST"}}
		if IsResourceDoesNotExistError(err) {
			t.Fatal("expected false for non-404")
		}
	})

	t.Run("wrapped", func(t *testing.T) {
		t.Parallel()
		inner := &APIError{StatusCode: 404, MLFlowError: &MLFlowError{ErrorCode: "RESOURCE_DOES_NOT_EXIST"}}
		if !IsResourceDoesNotExistError(errors.Join(inner)) {
			t.Fatal("expected errors.As to match wrapped APIError")
		}
	})
}

func TestIsResourceAlreadyExistsError(t *testing.T) {
	t.Parallel()

	t.Run("structured error", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode: 400,
			MLFlowError: &MLFlowError{
				ErrorCode: "RESOURCE_ALREADY_EXISTS",
			},
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true")
		}
	})

	t.Run("response body fallback", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   400,
			ResponseBody: `{"error_code":"RESOURCE_ALREADY_EXISTS"}`,
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true")
		}
	})
}
