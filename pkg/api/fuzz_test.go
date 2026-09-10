package api

import (
	"net"
	"strings"
	"testing"
)

func FuzzValidateGitCloneURL(f *testing.F) {
	f.Add("https://github.com/org/repo.git")
	f.Add("http://github.com/org/repo.git")
	f.Add("https://localhost/repo.git")
	f.Add("https://127.0.0.1/repo.git")
	f.Add("https://10.0.0.1/repo.git")
	f.Add("https://metadata.google.internal/path")
	f.Add("https://evil.svc/repo")
	f.Add("https://evil.cluster.local/repo")
	f.Add("ftp://github.com/repo")
	f.Add("git@github.com:org/repo.git")
	f.Add("")
	f.Add("   ")
	f.Add("https://github.com/org/repo.git?ref=main")
	f.Add("https://[::1]/repo.git")
	f.Add("https://169.254.169.254/latest/meta-data/")
	f.Add("://bad")

	f.Fuzz(func(t *testing.T, raw string) {
		err := ValidateGitCloneURL(raw)
		trimmed := strings.TrimSpace(raw)

		if trimmed == "" && err == nil {
			t.Fatal("expected error for empty URL")
		}

		if err == nil {
			// Successful validation must mean http or https.
			lower := strings.ToLower(trimmed)
			if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
				t.Fatalf("accepted non-http(s) URL: %q", raw)
			}
		}
	})
}

func FuzzValidateGitCloneURLAuth(f *testing.F) {
	f.Add("https://github.com/org/repo.git", true)
	f.Add("http://github.com/org/repo.git", true)
	f.Add("http://github.com/org/repo.git", false)
	f.Add("https://github.com/org/repo.git", false)
	f.Add("", true)
	f.Add("", false)

	f.Fuzz(func(t *testing.T, raw string, withCredentials bool) {
		err := ValidateGitCloneURLAuth(raw, withCredentials)
		if !withCredentials && err != nil {
			t.Fatal("expected nil error when withCredentials=false")
		}
	})
}

func FuzzValidateGitCloneURLResolved(f *testing.F) {
	f.Add("https://github.com/org/repo.git")
	f.Add("https://localhost/repo.git")
	f.Add("https://127.0.0.1/repo.git")
	f.Add("https://evil.local/repo")
	f.Add("")

	stubLookup := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	}

	f.Fuzz(func(t *testing.T, raw string) {
		_ = ValidateGitCloneURLResolved(raw, stubLookup)
	})
}

func FuzzLooksLikeHexSHA(f *testing.F) {
	f.Add("abc1234")
	f.Add("abcdef1234567890abcdef1234567890abcdef12")
	f.Add("abcdef")
	f.Add("ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890")
	f.Add("")
	f.Add("xyz1234")
	f.Add("abc123!")

	f.Fuzz(func(t *testing.T, s string) {
		got := LooksLikeHexSHA(s)
		if got {
			if len(s) < 7 || len(s) > 40 {
				t.Fatalf("LooksLikeHexSHA(%q) = true but length %d not in [7,40]", s, len(s))
			}
			for _, c := range s {
				if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
					t.Fatalf("LooksLikeHexSHA(%q) = true but contains non-hex char %q", s, c)
				}
			}
		}
	})
}

func FuzzValidateResolvedSHA(f *testing.F) {
	f.Add("")
	f.Add("abc1234")
	f.Add("not-a-sha")
	f.Add("abcdef1234567890abcdef1234567890abcdef12")
	f.Add("   ")

	f.Fuzz(func(t *testing.T, sha string) {
		err := ValidateResolvedSHA(sha)
		if sha == "" && err != nil {
			t.Fatal("expected nil error for empty SHA")
		}
		if err == nil && sha != "" && !LooksLikeHexSHA(sha) {
			t.Fatalf("accepted invalid SHA %q", sha)
		}
	})
}

func FuzzGetOverallState(f *testing.F) {
	f.Add("pending")
	f.Add("running")
	f.Add("completed")
	f.Add("failed")
	f.Add("cancelled")
	f.Add("partially_failed")
	f.Add("unknown")
	f.Add("")
	f.Add("PENDING")

	validStates := map[string]bool{
		"pending": true, "running": true, "completed": true,
		"failed": true, "cancelled": true, "partially_failed": true,
	}

	f.Fuzz(func(t *testing.T, s string) {
		state, err := GetOverallState(s)
		if validStates[s] {
			if err != nil {
				t.Fatalf("unexpected error for valid state %q: %v", s, err)
			}
			if string(state) != s {
				t.Fatalf("GetOverallState(%q) = %q, want %q", s, state, s)
			}
		} else {
			if err == nil {
				t.Fatalf("expected error for invalid state %q", s)
			}
		}
	})
}

func FuzzDateTimeFromString(f *testing.F) {
	f.Add("2024-01-15T10:30:00Z")
	f.Add("2024-01-15T10:30:00+05:30")
	f.Add("")
	f.Add("not-a-date")
	f.Add("2024-13-01T00:00:00Z")

	f.Fuzz(func(t *testing.T, s string) {
		dt := DateTime(s)
		tm, err := DateTimeFromString(dt)
		if err == nil && s != "" {
			roundTripped := DateTimeToString(tm)
			tm2, err2 := DateTimeFromString(roundTripped)
			if err2 != nil {
				t.Fatalf("round-trip failed for %q: %v", s, err2)
			}
			if !tm.Equal(tm2) {
				t.Fatalf("round-trip time mismatch: %v vs %v", tm, tm2)
			}
		}
	})
}

func FuzzRewriteSidecarURLsInMessage(f *testing.F) {
	targets := SidecarURLTargets{
		EvalHub: "https://evalhub.example.com",
		MLFlow:  "https://mlflow.example.com",
		OCI:     "https://quay.io",
		Model:   "https://model.example.com",
	}
	base := "http://localhost:8080"

	f.Add("GET http://localhost:8080/api/v1/evaluations/123 failed", base)
	f.Add("POST http://localhost:8080/api/2.0/mlflow/runs timed out", base)
	f.Add("no urls in this message", base)
	f.Add("", base)
	f.Add("http://localhost:8080", "")
	f.Add("http://localhost:8080/unknown/path", base)
	f.Add("multiple http://localhost:8080/a and http://localhost:8080/b urls", base)

	f.Fuzz(func(t *testing.T, message, sidecarBaseURL string) {
		result := RewriteSidecarURLsInMessage(message, sidecarBaseURL, targets)

		// Rewriting must never panic and must always return a string.
		_ = result

		// When sidecar base URL is empty or not in the message, original is returned.
		base := strings.TrimRight(strings.TrimSpace(sidecarBaseURL), "/")
		if base == "" || !strings.Contains(message, base) {
			if result != message {
				t.Fatalf("expected original message when base URL absent, got %q", result)
			}
		}
	})
}

func FuzzIsMLflowProxyPath_API(f *testing.F) {
	f.Add("/api/2.0/mlflow")
	f.Add("/api/2.0/mlflow/")
	f.Add("/api/2.0/mlflow/experiments/list")
	f.Add("/api/2.0/mlflowx")
	f.Add("/api/2.0/mlflow-artifacts")
	f.Add("/api/2.0/mlflow-artifacts/path")
	f.Add("/api/3.0/mlflow")
	f.Add("")
	f.Add("/other/path")

	f.Fuzz(func(t *testing.T, path string) {
		got := isMLflowProxyPath(path)
		want := mlflowPathMatchesPrefix(path, "/api/2.0/mlflow") ||
			mlflowPathMatchesPrefix(path, "/api/3.0/mlflow") ||
			mlflowPathMatchesPrefix(path, "/api/2.0/mlflow-artifacts")
		if got != want {
			t.Fatalf("isMLflowProxyPath(%q) = %v, oracle = %v", path, got, want)
		}
	})
}

func FuzzOciPathMatchesRepository(f *testing.F) {
	f.Add("/v2/org/repo/manifests/latest", "org/repo")
	f.Add("/v2/org/repo/blobs/sha256:abc", "org/repo")
	f.Add("/v2/other/repo/manifests/v1", "org/repo")
	f.Add("/org/repo/tags/list", "org/repo")
	f.Add("", "org/repo")
	f.Add("/v2/org/repo", "")
	f.Add("/v2///org/repo", "org/repo")

	f.Fuzz(func(t *testing.T, path, repository string) {
		got := ociPathMatchesRepository(path, repository)
		repoParts := splitPathSegments(repository)
		if len(repoParts) == 0 && got {
			t.Fatalf("ociPathMatchesRepository(%q, %q) = true with empty repo segments", path, repository)
		}
	})
}
