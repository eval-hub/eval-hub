package proxy

import "testing"

func TestIsModelProxyPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/model", true},
		{"/model/", true},
		{"/model//v1", true},
		{"/model/v1/completions", true},
		{"/api/2.0/mlflow", false},
		{"/modelv1", false},
		{"/v1/completions", false},
	}
	for _, tt := range tests {
		if got := IsModelProxyPath(tt.path); got != tt.want {
			t.Errorf("IsModelProxyPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestStripModelProxyPathPrefix(t *testing.T) {
	if got := stripModelProxyPathPrefix("/model"); got != "/" {
		t.Errorf("got %q, want /", got)
	}
	if got := stripModelProxyPathPrefix("/model/v1/chat/completions"); got != "/v1/chat/completions" {
		t.Errorf("got %q", got)
	}
	if got := stripModelProxyPathPrefix("/other"); got != "/other" {
		t.Errorf("got %q", got)
	}
}
