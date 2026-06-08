package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
)

func TestWrapRequestRBACProxyRequiresHeaders(t *testing.T) {
	t.Parallel()

	handler := wrapRequest(&config.Config{AuthType: config.AuthTypeRBACProxy}, discardLogger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
