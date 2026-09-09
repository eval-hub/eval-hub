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

		// Derive expected outcome from the /model/<jobID>[/<remaining>] grammar.
		rest, hasPrefix := strings.CutPrefix(path, "/model/")
		wantOK := hasPrefix && rest != "" && strings.IndexByte(rest, '/') != 0
		if ok != wantOK {
			t.Fatalf("ParseLocalModelPath(%q): ok=%v, want %v", path, ok, wantOK)
		}

		if !ok {
			if jobID != "" || remaining != "" {
				t.Fatalf("expected empty on !ok, got jobID=%q remaining=%q", jobID, remaining)
			}
			return
		}

		var wantJobID, wantRemaining string
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			wantJobID = rest[:i]
			wantRemaining = rest[i:]
		} else {
			wantJobID = rest
		}
		if jobID != wantJobID {
			t.Fatalf("ParseLocalModelPath(%q): jobID=%q, want %q", path, jobID, wantJobID)
		}
		if remaining != wantRemaining {
			t.Fatalf("ParseLocalModelPath(%q): remaining=%q, want %q", path, remaining, wantRemaining)
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

		want := false
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			want = strings.HasSuffix(token, ":ref")
		}
		if got != want {
			t.Fatalf("isModelRefToken(%q) = %v, want %v", authHeader, got, want)
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

		want := false
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			want = strings.HasPrefix(token, "token:")
		}
		if got != want {
			t.Fatalf("isExplicitHardcodedToken(%q) = %v, want %v", authHeader, got, want)
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

		var want string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if strings.HasPrefix(token, "token:") {
				want = strings.TrimPrefix(token, "token:")
			}
		}
		if got != want {
			t.Fatalf("extractExplicitHardcodedToken(%q) = %q, want %q", authHeader, got, want)
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
