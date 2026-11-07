package oci

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

func TestOperationErrorPreservesCause(t *testing.T) {
	for _, cause := range []error{
		errors.New("docker configuration is missing"),
		errors.New("registry credentials are missing"),
		errors.New("registry credentials are invalid"),
	} {
		err := &OperationError{Operation: "resolve credentials", Err: fmt.Errorf("status 403: %w", cause)}
		if !errors.Is(err, cause) {
			t.Fatalf("credential error identity lost: %v", err)
		}
		var httpErr *ociclient.RegistryHTTPError
		if errors.As(err, &httpErr) {
			t.Fatal("error text was mistaken for an HTTP error")
		}
	}
}

func TestOperationErrorDiagnostics(t *testing.T) {
	cause := &ociclient.RegistryHTTPError{Operation: "put manifest", StatusCode: 403, Message: "denied"}
	inner := &OperationError{Operation: "publish evaluation bundle", Err: cause}
	outer := &OperationError{Operation: "Evaluation bundle OCI publication failed", Err: inner}
	var registryErr *ociclient.RegistryHTTPError
	if !errors.Is(outer, cause) || !errors.As(outer, &registryErr) || registryErr != cause {
		t.Fatalf("registry cause lost: %v", outer)
	}
	want := "Evaluation bundle OCI publication failed: publish evaluation bundle: " + cause.Error()
	if outer.Error() != want {
		t.Fatalf("error = %q, want %q", outer.Error(), want)
	}
	err := &OperationError{Operation: "initialize OCI publisher", Reason: "publisher is unavailable"}
	if err.Error() != "initialize OCI publisher: publisher is unavailable" || errors.Unwrap(err) != nil {
		t.Fatalf("unexpected reason-only error: %v", err)
	}
}
