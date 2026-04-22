package proxy

import (
	"net/http"
	"testing"
)

func TestRewriteHuggingFaceRedirectLocation(t *testing.T) {
	hub := "huggingface.co"

	tests := []struct {
		name     string
		location string
		want     string
	}{
		{
			name:     "relative resolve-cache",
			location: "/api/resolve-cache/models/google/flan-t5-small/0fc9ddf78a1e988dac52e2dac162b0ede4fd74ab/config.json?%2Fgoogle%2Fflan-t5-small%2Fresolve%2Fmain%2Fconfig.json=&etag=%221%22",
			want:     "/huggingface/api/resolve-cache/models/google/flan-t5-small/0fc9ddf78a1e988dac52e2dac162b0ede4fd74ab/config.json?%2Fgoogle%2Fflan-t5-small%2Fresolve%2Fmain%2Fconfig.json=&etag=%221%22",
		},
		{
			name:     "relative parquet path",
			location: "/datasets/allenai/ai2_arc/resolve/210d026faf9955653af8916fad021475a3f00453/ARC-Easy/train-00000-of-00001.parquet",
			want:     "/huggingface/datasets/allenai/ai2_arc/resolve/210d026faf9955653af8916fad021475a3f00453/ARC-Easy/train-00000-of-00001.parquet",
		},
		{
			name:     "absolute huggingface.co",
			location: "https://huggingface.co/api/models/foo",
			want:     "/huggingface/api/models/foo",
		},
		{
			name:     "subdomain huggingface.co",
			location: "https://cdn-lfs.huggingface.co/foo/bar",
			want:     "/huggingface/foo/bar",
		},
		{
			name:     "already under huggingface prefix unchanged",
			location: "/huggingface/api/resolve-cache/foo",
			want:     "/huggingface/api/resolve-cache/foo",
		},
		{
			name:     "cas xet unchanged",
			location: "https://cas-bridge.xethub.hf.co/xet-bridge-us/abc?X-Amz-Algorithm=AWS4-HMAC-SHA256",
			want:     "https://cas-bridge.xethub.hf.co/xet-bridge-us/abc?X-Amz-Algorithm=AWS4-HMAC-SHA256",
		},
		{
			name:     "empty",
			location: "",
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteHuggingFaceRedirectLocation(tt.location, hub)
			if got != tt.want {
				t.Fatalf("rewriteHuggingFaceRedirectLocation(%q) = %q, want %q", tt.location, got, tt.want)
			}
		})
	}
}

func TestClientOriginFromRequest(t *testing.T) {
	t.Run("X-Forwarded-Proto https", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "http://10.0.0.1:8080/huggingface/foo", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-Proto", "https")
		if got := ClientOriginFromRequest(req); got != "https://10.0.0.1:8080" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("plain http", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "http://localhost:8080/huggingface/foo", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := ClientOriginFromRequest(req); got != "http://localhost:8080" {
			t.Fatalf("got %q", got)
		}
	})
}
