package ociclient

import (
	"fmt"
	"io"
	"net/http"
)

// MaxErrorResponseBytes limits registry and token-service diagnostic response reads.
const MaxErrorResponseBytes = 64 * 1024

// RegistryHTTPError preserves an HTTP failure from a registry or its token service.
type RegistryHTTPError struct {
	Operation  string
	StatusCode int
	Message    string
	Err        error
}

func (e *RegistryHTTPError) Error() string {
	message := fmt.Sprintf("%s failed with HTTP status %d", e.Operation, e.StatusCode)
	if e.Message != "" {
		message += ": " + e.Message
	}
	if e.Err != nil {
		message += ": read response: " + e.Err.Error()
	}
	return message
}

func (e *RegistryHTTPError) Unwrap() error { return e.Err }

func registryHTTPError(operation string, resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxErrorResponseBytes))
	return &RegistryHTTPError{
		Operation: operation, StatusCode: resp.StatusCode, Message: string(body), Err: err,
	}
}
