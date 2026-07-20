package mlflowclient

import (
	"net/url"
	"strings"
	"testing"
)

func FuzzBuildArtifactUploadEndpoint(f *testing.F) {
	f.Add("1/run 1/artifacts/evaluation-card.json")
	f.Add("1/abc123/artifacts/evaluation-card.json")
	f.Add("")
	f.Add("/")
	f.Add("//")
	f.Add("../etc/passwd")
	f.Add("a/b/c")
	f.Add("segment with spaces/and%2Fencoded")

	f.Fuzz(func(t *testing.T, artifactPath string) {
		got, err := buildArtifactUploadEndpoint(artifactPath)
		segments := strings.Split(artifactPath, "/")
		nonEmpty := 0
		for _, segment := range segments {
			if segment != "" {
				nonEmpty++
			}
		}
		if nonEmpty == 0 {
			if err == nil {
				t.Fatal("expected error for empty artifact path")
			}
			if got != "" {
				t.Fatalf("expected empty endpoint on error, got %q", got)
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error for path %q: %v", artifactPath, err)
		}
		if !strings.HasPrefix(got, artifactsAPIBasePath+"/") {
			t.Fatalf("endpoint %q missing base path prefix", got)
		}
		if strings.Contains(got, " ") {
			t.Fatalf("endpoint %q contains unescaped space", got)
		}
		if _, err := url.Parse(got); err != nil {
			t.Fatalf("endpoint %q is not a valid URL path: %v", got, err)
		}
		suffix := strings.TrimPrefix(got, artifactsAPIBasePath+"/")
		escaped := strings.Split(suffix, "/")
		if len(escaped) != nonEmpty {
			t.Fatalf("endpoint segment count = %d, want %d for path %q", len(escaped), nonEmpty, artifactPath)
		}
		i := 0
		for _, segment := range segments {
			if segment == "" {
				continue
			}
			if escaped[i] != url.PathEscape(segment) {
				t.Fatalf("segment %d = %q, want %q", i, escaped[i], url.PathEscape(segment))
			}
			i++
		}
	})
}
