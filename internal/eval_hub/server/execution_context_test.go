package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
)

func TestRequestWrapper(t *testing.T) {
	httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs?tags=test-tag-2&tags=test-tag-3", nil)
	rec := httptest.NewRecorder()
	requestWrapper := server.NewRequestWrapper(rec, httpRequest, -1)

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

func TestRequestWrapper_BodyExceedsMaxBytes(t *testing.T) {
	body := strings.NewReader(`{"x":"` + strings.Repeat("a", 64) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", body)
	rec := httptest.NewRecorder()
	const maxBytes int64 = 32
	rw := server.NewRequestWrapper(rec, req, maxBytes)

	_, err := rw.BodyAsBytes()
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	var se *serviceerrors.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected ServiceError, got %v", err)
	}
	if se.MessageCode().GetStatusCode() != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", se.MessageCode().GetStatusCode())
	}
}
