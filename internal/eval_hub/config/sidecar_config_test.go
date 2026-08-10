package config

import "testing"

func TestEffectiveBaseURL(t *testing.T) {
	t.Parallel()

	t.Run("returns BaseURL when set", func(t *testing.T) {
		sc := &SidecarConfig{BaseURL: "https://sidecar.example:9443"}
		if got := sc.EffectiveBaseURL(); got != "https://sidecar.example:9443" {
			t.Fatalf("EffectiveBaseURL() = %q, want %q", got, "https://sidecar.example:9443")
		}
	})

	t.Run("returns default when BaseURL empty", func(t *testing.T) {
		sc := &SidecarConfig{}
		if got := sc.EffectiveBaseURL(); got != DefaultSidecarBaseURL {
			t.Fatalf("EffectiveBaseURL() = %q, want %q", got, DefaultSidecarBaseURL)
		}
	})
}

func TestResolvePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		baseURL     string
		wantPort    int32
		wantBaseURL string
		wantErr     bool
	}{
		{"standard URL", "http://localhost:8080", 8080, "http://localhost:8080", false},
		{"custom port", "http://localhost:9090", 9090, "http://localhost:9090", false},
		{"https with port", "https://sidecar.example:8443", 8443, "https://sidecar.example:8443", false},
		{"trailing slash", "http://localhost:8080/", 8080, "http://localhost:8080/", false},
		{"empty URL is no-op", "", 0, "", false},
		{"no port is rejected", "http://localhost", 0, "", true},
		{"ftp scheme rejected", "ftp://localhost:2121", 0, "", true},
		{"opaque URL rejected", "localhost:9090", 0, "", true},
		{"hostless URL rejected", "http://:8080", 0, "", true},
		{"port zero rejected", "http://localhost:0", 0, "", true},
		{"port out of range", "http://localhost:70000", 0, "", true},
		{"negative port rejected", "http://localhost:-1", 0, "", true},
		{"invalid port string", "http://localhost:abc", 0, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := &SidecarConfig{BaseURL: tc.baseURL}
			err := sc.ResolvePort()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolvePort() with BaseURL %q: want error, got port %d", tc.baseURL, sc.Port)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePort() with BaseURL %q: unexpected error: %v", tc.baseURL, err)
			}
			if sc.Port != tc.wantPort {
				t.Fatalf("ResolvePort() with BaseURL %q: got port %d, want %d", tc.baseURL, sc.Port, tc.wantPort)
			}
			if sc.BaseURL != tc.wantBaseURL {
				t.Fatalf("ResolvePort() with BaseURL %q: got BaseURL %q, want %q", tc.baseURL, sc.BaseURL, tc.wantBaseURL)
			}
		})
	}
}
