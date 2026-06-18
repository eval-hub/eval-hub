package features

import (
	"net/url"
	"testing"

	messages "github.com/cucumber/messages/go/v21"
)

func TestScenarioHasTag(t *testing.T) {
	t.Parallel()
	sc := &messages.Pickle{
		Tags: []*messages.PickleTag{
			{Name: "@metrics"},
			{Name: "hardware_profile"},
		},
	}
	if !scenarioHasTag(sc, "metrics") {
		t.Fatal("expected @metrics to match tag metrics")
	}
	if !scenarioHasTag(sc, "hardware_profile") {
		t.Fatal("expected hardware_profile to match")
	}
	if scenarioHasTag(sc, "ignore") {
		t.Fatal("unexpected tag match")
	}
}

func TestIsMetricsScrapePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{path: "/metrics", want: true},
		{path: "http://localhost:8081/metrics", want: true},
		{path: "/api/v1/health", want: false},
		{path: "/api/v1/evaluations/jobs", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := isMetricsScrapePath(tt.path); got != tt.want {
				t.Fatalf("isMetricsScrapePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestJoinBaseURL(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("http://localhost:8081")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	if got := joinBaseURL(base, "/metrics"); got != "http://localhost:8081/metrics" {
		t.Fatalf("joinBaseURL = %q, want http://localhost:8081/metrics", got)
	}
}

func TestResolveMetricsBaseURL(t *testing.T) {
	apiBase, err := url.Parse("http://localhost:8080")
	if err != nil {
		t.Fatalf("parse api base: %v", err)
	}

	t.Run("defaults to API base in local mode", func(t *testing.T) {
		t.Setenv("SERVER_URL", "")
		t.Setenv(envMetricsURL, "")
		got, err := resolveMetricsBaseURL(apiBase)
		if err != nil {
			t.Fatalf("resolveMetricsBaseURL: %v", err)
		}
		if got.String() != apiBase.String() {
			t.Fatalf("got %q, want %q", got, apiBase)
		}
	})

	t.Run("uses METRICS_URL when set", func(t *testing.T) {
		t.Setenv("SERVER_URL", "")
		t.Setenv(envMetricsURL, "http://evalhub-metrics.evalhub.svc:8081")
		got, err := resolveMetricsBaseURL(apiBase)
		if err != nil {
			t.Fatalf("resolveMetricsBaseURL: %v", err)
		}
		if got.String() != "http://evalhub-metrics.evalhub.svc:8081" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("returns nil when remote without METRICS_URL", func(t *testing.T) {
		t.Setenv("SERVER_URL", "https://evalhub.example.com")
		t.Setenv(envMetricsURL, "")
		got, err := resolveMetricsBaseURL(apiBase)
		if err != nil {
			t.Fatalf("resolveMetricsBaseURL: %v", err)
		}
		if got != nil {
			t.Fatalf("got %q, want nil", got)
		}
	})

	t.Run("invalid METRICS_URL", func(t *testing.T) {
		t.Setenv("SERVER_URL", "")
		t.Setenv(envMetricsURL, "://bad")
		if _, err := resolveMetricsBaseURL(apiBase); err == nil {
			t.Fatal("expected error for invalid METRICS_URL")
		}
	})
}
