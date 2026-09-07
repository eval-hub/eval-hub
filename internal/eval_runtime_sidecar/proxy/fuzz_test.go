package proxy

import (
	"strings"
	"testing"
)

func FuzzParseLocalModelPath(f *testing.F) {
	f.Add("/model/job-123/v1/completions")
	f.Add("/model/job-123")
	f.Add("/model/")
	f.Add("/model")
	f.Add("/model//extra")
	f.Add("")
	f.Add("/other/path")
	f.Add("/model/job-id/")
	f.Add("/model/abc/def/ghi")

	f.Fuzz(func(t *testing.T, path string) {
		jobID, remaining, ok := ParseLocalModelPath(path)
		if !ok {
			if jobID != "" || remaining != "" {
				t.Fatalf("expected empty values on !ok, got jobID=%q remaining=%q", jobID, remaining)
			}
			return
		}

		if jobID == "" {
			t.Fatalf("ok=true but jobID is empty for path %q", path)
		}

		if strings.Contains(jobID, "/") {
			t.Fatalf("jobID %q contains '/' for path %q", jobID, path)
		}

		if remaining != "" && !strings.HasPrefix(remaining, "/") {
			t.Fatalf("remaining %q does not start with '/' for path %q", remaining, path)
		}
	})
}

func FuzzIsModelRefToken(f *testing.F) {
	f.Add("Bearer api-key:ref")
	f.Add("Bearer kfp_sa_token:ref")
	f.Add("Bearer some-prefix_api-key:ref")
	f.Add("Bearer token:secret")
	f.Add("Bearer normal-token")
	f.Add("")
	f.Add("Basic dXNlcjpwYXNz")
	f.Add("Bearer :ref")
	f.Add("bearer api-key:ref")

	f.Fuzz(func(t *testing.T, authHeader string) {
		got := isModelRefToken(authHeader)
		if got {
			if !strings.HasPrefix(authHeader, "Bearer ") {
				t.Fatalf("isModelRefToken(%q) = true but no Bearer prefix", authHeader)
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if !strings.HasSuffix(token, ":ref") {
				t.Fatalf("isModelRefToken(%q) = true but token %q doesn't end with :ref", authHeader, token)
			}
		}
	})
}

func FuzzIsExplicitHardcodedToken(f *testing.F) {
	f.Add("Bearer token:my-secret")
	f.Add("Bearer token:")
	f.Add("Bearer api-key:ref")
	f.Add("Bearer normal")
	f.Add("")
	f.Add("bearer token:secret")

	f.Fuzz(func(t *testing.T, authHeader string) {
		got := isExplicitHardcodedToken(authHeader)
		if got {
			if !strings.HasPrefix(authHeader, "Bearer ") {
				t.Fatalf("isExplicitHardcodedToken(%q) = true without Bearer prefix", authHeader)
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if !strings.HasPrefix(token, "token:") {
				t.Fatalf("isExplicitHardcodedToken(%q) = true but token %q lacks token: prefix", authHeader, token)
			}
		}
	})
}

func FuzzExtractExplicitHardcodedToken(f *testing.F) {
	f.Add("Bearer token:my-secret")
	f.Add("Bearer token:")
	f.Add("Bearer normal")
	f.Add("")

	f.Fuzz(func(t *testing.T, authHeader string) {
		got := extractExplicitHardcodedToken(authHeader)
		if !isExplicitHardcodedToken(authHeader) {
			if got != "" {
				t.Fatalf("non-explicit token header returned non-empty extraction: %q", got)
			}
			return
		}
		expected := strings.TrimPrefix(strings.TrimPrefix(authHeader, "Bearer "), "token:")
		if got != expected {
			t.Fatalf("extractExplicitHardcodedToken(%q) = %q, want %q", authHeader, got, expected)
		}
	})
}

func FuzzIsCredentialKey(f *testing.F) {
	f.Add("api-key")
	f.Add("kfp_api-key")
	f.Add("kfp_sa_token")
	f.Add("model_url")
	f.Add("random-key")
	f.Add("")
	f.Add("_api-key")
	f.Add("_sa_token")

	f.Fuzz(func(t *testing.T, key string) {
		got := isCredentialKey(key)
		want := key == "api-key" ||
			strings.HasSuffix(key, "_api-key") ||
			strings.HasSuffix(key, "_sa_token")
		if got != want {
			t.Fatalf("isCredentialKey(%q) = %v, oracle = %v", key, got, want)
		}
	})
}
