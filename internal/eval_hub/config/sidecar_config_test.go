package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDuration_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dur  Duration
		want string
	}{
		{"five minutes", Duration{5 * time.Minute}, `"5m0s"`},
		{"two hours", Duration{2 * time.Hour}, `"2h0m0s"`},
		{"thirty seconds", Duration{30 * time.Second}, `"30s"`},
		{"zero", Duration{0}, `"0s"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.dur)
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("MarshalJSON = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid durations", func(t *testing.T) {
		tests := []struct {
			name string
			json string
			want time.Duration
		}{
			{"minutes", `"5m"`, 5 * time.Minute},
			{"hours", `"2h"`, 2 * time.Hour},
			{"seconds", `"30s"`, 30 * time.Second},
			{"compound", `"1h30m"`, 90 * time.Minute},
			{"zero", `"0s"`, 0},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var d Duration
				if err := json.Unmarshal([]byte(tc.json), &d); err != nil {
					t.Fatalf("UnmarshalJSON(%s): %v", tc.json, err)
				}
				if d.Duration != tc.want {
					t.Fatalf("UnmarshalJSON(%s) = %v, want %v", tc.json, d.Duration, tc.want)
				}
			})
		}
	})

	t.Run("invalid duration string", func(t *testing.T) {
		var d Duration
		if err := json.Unmarshal([]byte(`"notaduration"`), &d); err == nil {
			t.Fatal("expected error for invalid duration string, got nil")
		}
	})

	t.Run("non-string JSON type", func(t *testing.T) {
		var d Duration
		if err := json.Unmarshal([]byte(`123`), &d); err == nil {
			t.Fatal("expected error for non-string JSON, got nil")
		}
	})
}

func TestDuration_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := LocalConfig{
		JobCacheSweepInterval: Duration{5 * time.Minute},
		JobCacheEntryTTL:      Duration{1 * time.Hour},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded LocalConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.JobCacheSweepInterval.Duration != original.JobCacheSweepInterval.Duration {
		t.Fatalf("sweep interval = %v, want %v", decoded.JobCacheSweepInterval.Duration, original.JobCacheSweepInterval.Duration)
	}
	if decoded.JobCacheEntryTTL.Duration != original.JobCacheEntryTTL.Duration {
		t.Fatalf("entry TTL = %v, want %v", decoded.JobCacheEntryTTL.Duration, original.JobCacheEntryTTL.Duration)
	}
}

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
