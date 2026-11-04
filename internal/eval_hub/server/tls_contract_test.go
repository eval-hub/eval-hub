package server_test

import (
	"strings"
	"testing"
)

func TestAPIRejectsInvalidTLSBeforeListening(t *testing.T) {
	for _, tt := range []struct{ key, value, want string }{
		{"TLS_REQUIRED", "true", "certificate and key"},
		{"TLS_MIN_VERSION", "1.1", "TLS_MIN_VERSION"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			srv, err := createServer(t, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := srv.Start(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("API did not reject TLS contract before listening: %v", err)
			}
		})
	}
}
