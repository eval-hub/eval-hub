package proxy

import "testing"

func TestIsHuggingFaceProxyPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/huggingface", true},
		{"/huggingface/", true},
		{"/huggingface//api", true},
		{"/huggingface/api/models/foo", true},
		{"/huggingfacev1", false},
		{"/api/models", false},
		{"/", false},
	}
	for _, tt := range tests {
		if got := IsHuggingFaceProxyPath(tt.path); got != tt.want {
			t.Errorf("IsHuggingFaceProxyPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestStripHuggingFaceProxyPathPrefix(t *testing.T) {
	if got := stripHuggingFaceProxyPathPrefix("/huggingface"); got != "/" {
		t.Errorf("strip = %q, want /", got)
	}
	if got := stripHuggingFaceProxyPathPrefix("/huggingface/api/models"); got != "/api/models" {
		t.Errorf("strip = %q, want /api/models", got)
	}
}
