package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
)

func TestMCPHTTPRejectsInvalidTLSBeforeListening(t *testing.T) {
	for _, transport := range []string{config.TransportHTTP, config.TransportHTTPSSE} {
		for _, tt := range []struct{ key, value, want string }{
			{"TLS_REQUIRED", "true", "certificate and key"},
			{"TLS_MIN_VERSION", "1.1", "TLS_MIN_VERSION"},
		} {
			t.Run(transport+"/"+tt.key, func(t *testing.T) {
				t.Setenv(tt.key, tt.value)
				cfg := config.DefaultConfig()
				cfg.Transport = transport
				if err := serveHTTP(context.Background(), http.NotFoundHandler(), cfg, slog.Default()); err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("MCP did not reject TLS contract before listening: %v", err)
				}
			})
		}
	}
}
