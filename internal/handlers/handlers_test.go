package handlers_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/handlers"
)

func TestNew(t *testing.T) {
	h := handlers.New(nil, nil)
	if h == nil {
		t.Error("New() returned nil")
	}
}

func createExecutionContext(method string, uri string) *executioncontext.ExecutionContext {
	return &executioncontext.ExecutionContext{
		Request: &MockRequest{
			TestMethod: method,
			TestURI:    uri,
			headers:    make(map[string]string),
		},
	}
}

type MockRequest struct {
	TestMethod string
	TestURI    string
	headers    map[string]string
}

func (r *MockRequest) Method() string {
	return r.TestMethod
}

func (r *MockRequest) URI() string {
	return r.TestURI
}

func (r *MockRequest) Path() string {
	return ""
}

func (r *MockRequest) Query(key string) map[string][]string {
	return make(map[string][]string)
}

func (r *MockRequest) Header(key string) string {
	return r.headers[key]
}

func (r *MockRequest) BodyAsBytes() ([]byte, error) {
	return []byte{}, nil
}

func (r *MockRequest) SetHeader(key string, value string) {
	r.headers[key] = value
}
