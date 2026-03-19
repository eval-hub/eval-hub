package server_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
)

func TestRequestWrapper(t *testing.T) {
	httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs?tags=test-tag-2&tags=test-tag-3", nil)
	requestWrapper := server.NewRequestWrapper(httpRequest)

	if requestWrapper.Method() != http.MethodGet {
		t.Errorf("Expected method %s, got %s", http.MethodGet, requestWrapper.Method())
	}
	if requestWrapper.URI() != "/api/v1/evaluations/jobs?tags=test-tag-2&tags=test-tag-3" {
		t.Errorf("Expected URI %s, got %s", "/api/v1/evaluations/jobs?tags=test-tag-2&tags=test-tag-3", requestWrapper.URI())
	}
	if requestWrapper.Path() != "/api/v1/evaluations/jobs" {
		t.Errorf("Expected path %s, got %s", "/api/v1/evaluations/jobs", requestWrapper.Path())
	}
	tags := requestWrapper.Query("tags")
	if len(tags) != 2 {
		t.Errorf("Expected query %s, got %s", []string{"test-tag-2", "test-tag-3"}, tags)
	}
	if !slices.Contains(tags, "test-tag-2") {
		t.Errorf("Expected query %s, got %s", []string{"test-tag-2", "test-tag-3"}, tags)
	}
	if !slices.Contains(tags, "test-tag-3") {
		t.Errorf("Expected query %s, got %s", []string{"test-tag-2", "test-tag-3"}, tags)
	}
}
