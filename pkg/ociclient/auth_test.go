package ociclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAuthParam(t *testing.T) {
	t.Parallel()

	key, value, ok := parseAuthParam(`realm="https://auth.example/token"`)
	if !ok || key != "realm" || value != "https://auth.example/token" {
		t.Fatalf("parseAuthParam() = (%q, %q, %v)", key, value, ok)
	}
	if _, _, ok := parseAuthParam("invalid"); ok {
		t.Fatal("expected invalid param to fail")
	}
}

func TestParseBearerRealm(t *testing.T) {
	t.Parallel()

	header := `Bearer realm="https://auth.example/token",service="registry",scope="repository:org/repo:pull"`
	got, err := parseBearerRealm(header)
	if err != nil {
		t.Fatalf("parseBearerRealm() err = %v", err)
	}
	want := "https://auth.example/token?service=registry&scope=repository:org/repo:pull"
	if got != want {
		t.Fatalf("parseBearerRealm() = %q, want %q", got, want)
	}
}

func TestParseBearerRealmErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
	}{
		{name: "not bearer", header: `Basic realm="x"`},
		{name: "missing realm", header: `Bearer service="registry"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseBearerRealm(tc.header); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAuthenticatorRefreshToken(t *testing.T) {
	t.Parallel()

	const tokenPath = "/token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			if user, pass, ok := r.BasicAuth(); !ok || user != "user" || pass != "pass" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{Token: "registry-token"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	auth := newAuthenticator(srv.URL, "org/repo", Credentials{Username: "user", Password: "pass"}, srv.Client())
	if err := auth.refreshToken(); err != nil {
		t.Fatalf("refreshToken() err = %v", err)
	}
	if auth.token != "registry-token" {
		t.Fatalf("token = %q, want registry-token", auth.token)
	}
}

func TestAuthenticatorRefreshTokenAccessTokenField(t *testing.T) {
	t.Parallel()

	const tokenPath = "/token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access-token"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	auth := newAuthenticator(srv.URL, "org/repo", Credentials{Username: "user", Password: "pass"}, srv.Client())
	if err := auth.refreshToken(); err != nil {
		t.Fatalf("refreshToken() err = %v", err)
	}
	if auth.token != "access-token" {
		t.Fatalf("token = %q, want access-token", auth.token)
	}
}

func TestAuthenticatorRefreshTokenNoAuthRequired(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v2" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	auth := newAuthenticator(srv.URL, "org/repo", Credentials{}, srv.Client())
	auth.token = "stale"
	if err := auth.refreshToken(); err != nil {
		t.Fatalf("refreshToken() err = %v", err)
	}
	if auth.token != "" {
		t.Fatalf("token = %q, want empty when registry does not challenge", auth.token)
	}
}

func TestAuthenticatorCreateNewTokenErrors(t *testing.T) {
	t.Parallel()

	const tokenPath = "/token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	auth := newAuthenticator(srv.URL, "org/repo", Credentials{Username: "user", Password: "bad"}, srv.Client())
	if err := auth.refreshToken(); err == nil {
		t.Fatal("expected token request failure")
	}
}

func TestAuthenticatorAuthorizeSetsBearerHeader(t *testing.T) {
	t.Parallel()

	auth := newAuthenticator("https://registry.example", "org/repo", Credentials{}, http.DefaultClient)
	auth.token = "cached-token"
	req, err := http.NewRequest(http.MethodGet, "https://registry.example/v2/", nil)
	if err != nil {
		t.Fatalf("NewRequest() err = %v", err)
	}
	auth.authorize(req)
	if got := req.Header.Get("Authorization"); got != "Bearer cached-token" {
		t.Fatalf("Authorization = %q", got)
	}
}
