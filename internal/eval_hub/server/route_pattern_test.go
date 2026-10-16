package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMethodRoutePreservesSpanName(t *testing.T) {
	path := "/api/v1/evaluations/post-processing/{post_processing_job_id}"
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/evaluations/post-processing/job", nil)
	for _, pattern := range []string{path, http.MethodDelete + " " + path} {
		if got := spanNameFormatter(pattern, req); got != http.MethodDelete+" "+path {
			t.Fatalf("span for %q = %q", pattern, got)
		}
	}
}
