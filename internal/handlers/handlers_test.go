package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
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

type MockResponseWrapper struct {
	recorder *httptest.ResponseRecorder
}

func (w MockResponseWrapper) SetStatusCode(code int) {
	w.recorder.WriteHeader(code)
}

func (w MockResponseWrapper) SetHeader(key string, value string) {
	w.recorder.Header().Set(key, value)
}

func (w MockResponseWrapper) DeleteHeader(key string) {
	w.recorder.Header().Del(key)
}

func (w MockResponseWrapper) Write(buf []byte) (int, error) {
	return w.recorder.Write(buf)
}

func (w MockResponseWrapper) Error(err string, code int, requestId string) {
	w.WriteJSON(api.Error{Message: err, Code: code, Trace: requestId}, code)
}

func (w MockResponseWrapper) WriteJSON(v any, code int) {
	w.recorder.Code = code
	w.recorder.Header().Set("Content-Type", "application/json")
	w.recorder.WriteHeader(code)
	err := json.NewEncoder(w.recorder).Encode(v)
	if err != nil {
		fmt.Printf("Failed to encode JSON: %v\n", err)
	}
}
