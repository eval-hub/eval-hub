package server

import (
	"net/http"
	"net/url"
	"testing"
)

func TestRemoteUserFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		rbacProxyAuth bool
		headers       map[string]string
		urlUser       *url.Userinfo
		want          string
	}{
		{
			name:          "rbac-proxy uses X-User only",
			rbacProxyAuth: true,
			headers:       map[string]string{USER_HEADER: "proxy-user", remoteUserHeader: "remote-user"},
			urlUser:       url.UserPassword("url-user", "secret"),
			want:          "proxy-user",
		},
		{
			name:          "rbac-proxy ignores fallbacks when X-User missing",
			rbacProxyAuth: true,
			headers:       map[string]string{remoteUserHeader: "remote-user"},
			urlUser:       url.UserPassword("url-user", "secret"),
			want:          "",
		},
		{
			name:          "default prefers X-User",
			rbacProxyAuth: false,
			headers:       map[string]string{USER_HEADER: "proxy-user", remoteUserHeader: "remote-user"},
			want:          "proxy-user",
		},
		{
			name:          "default falls back to URL user info",
			rbacProxyAuth: false,
			urlUser:       url.UserPassword("url-user", "secret"),
			want:          "url-user",
		},
		{
			name:          "default falls back to Remote-User header",
			rbacProxyAuth: false,
			headers:       map[string]string{remoteUserHeader: "remote-user"},
			want:          "remote-user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &http.Request{
				Header: make(http.Header),
				URL:    &url.URL{Path: "/"},
			}
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			if tt.urlUser != nil {
				req.URL.User = tt.urlUser
			}

			if got := remoteUserFromRequest(req, tt.rbacProxyAuth); got != tt.want {
				t.Fatalf("remoteUserFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}
