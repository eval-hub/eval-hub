package ociclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type tokenResponse struct {
	Token       string `json:"token,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
}

type authenticator struct {
	username   string
	password   string
	registry   string
	repository string
	httpClient *http.Client
	token      string
}

// newAuthenticator builds registry auth state for a single repository using dockerconfigjson
// credentials. It is required because OCI registries authenticate via Bearer tokens obtained
// from a separate token endpoint, not by sending username/password on every blob/manifest call.
func newAuthenticator(registry, repository string, creds Credentials, httpClient *http.Client) *authenticator {
	return &authenticator{
		username:   creds.Username,
		password:   creds.Password,
		registry:   NormalizeRegistryHost(registry),
		repository: strings.TrimSpace(repository),
		httpClient: httpClient,
	}
}

// authorize attaches the current Bearer token to an outbound registry request when one is cached.
func (a *authenticator) authorize(req *http.Request) {
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
}

// refreshToken performs the OCI Distribution token flow after a 401 response. Registries issue
// short-lived Bearer tokens scoped to repository push/pull; this exchanges dockerconfigjson
// credentials for that token.
func (a *authenticator) refreshToken(ctx context.Context) error {
	nextURL, err := a.initiateChallenge(ctx)
	if err != nil {
		return err
	}
	if nextURL == "" {
		a.token = ""
		return nil
	}
	return a.createNewToken(ctx, nextURL)
}

// initiateChallenge probes GET /v2/ and parses the WWW-Authenticate Bearer challenge to find
// the token endpoint URL. An empty return means the registry did not require authentication.
func (a *authenticator) initiateChallenge(ctx context.Context) (string, error) {
	scheme := "https"
	if strings.HasPrefix(a.registry, "http://") {
		scheme = "http"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(a.registry, "https://"), "http://")
	authURL := scheme + "://" + host + "/v2"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		return "", nil
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	if challenge == "" {
		return "", fmt.Errorf("no WWW-Authenticate header")
	}
	nextURL, err := parseBearerRealm(challenge)
	if err != nil {
		return "", err
	}
	repository := a.repository
	if repository == "" {
		repository = "default/repo"
	}
	if !strings.Contains(nextURL, "scope=") {
		sep := "?"
		if strings.Contains(nextURL, "?") {
			sep = "&"
		}
		nextURL += sep + "scope=repository:" + repository + ":pull,push"
	}
	return nextURL, nil
}

// createNewToken requests a Bearer token from the registry token service using HTTP basic auth
// with the dockerconfigjson username and password.
func (a *authenticator) createNewToken(ctx context.Context, nextURL string) error {
	if err := validateTokenRealmURL(a.registry, nextURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(a.username, a.password)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var tokenData tokenResponse
	if err := json.Unmarshal(body, &tokenData); err != nil {
		return err
	}
	switch {
	case tokenData.Token != "":
		a.token = tokenData.Token
	case tokenData.AccessToken != "":
		a.token = tokenData.AccessToken
	default:
		return fmt.Errorf("auth response missing token")
	}
	return nil
}

// validateTokenRealmURL rejects insecure HTTP token endpoints when the registry is configured for HTTPS.
func validateTokenRealmURL(registry, nextURL string) error {
	parsed, err := url.Parse(nextURL)
	if err != nil {
		return fmt.Errorf("parse token realm url: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if strings.HasPrefix(registry, "https://") {
			return fmt.Errorf("insecure token realm %q for https registry", nextURL)
		}
		return nil
	default:
		return fmt.Errorf("unsupported token realm scheme %q", parsed.Scheme)
	}
}

// parseBearerRealm extracts the token service URL from a WWW-Authenticate: Bearer header and
// re-attaches service/scope query parameters required by the registry token endpoint
func parseBearerRealm(header string) (string, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return "", fmt.Errorf("not a Bearer challenge")
	}
	header = header[7:]
	var realm, service, scope string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if key, value, ok := parseAuthParam(part); ok {
			switch key {
			case "realm":
				realm = value
			case "service":
				service = value
			case "scope":
				scope = value
			}
		}
	}
	if realm == "" {
		return "", fmt.Errorf("no realm in challenge")
	}
	sep := "?"
	if strings.Contains(realm, "?") {
		sep = "&"
	}
	if service != "" {
		realm += sep + "service=" + url.QueryEscape(service)
		sep = "&"
	}
	if scope != "" {
		realm += sep + "scope=" + url.QueryEscape(scope)
	}
	return realm, nil
}

// parseAuthParam splits a single key="value" pair from a WWW-Authenticate challenge fragment.
func parseAuthParam(s string) (key, value string, ok bool) {
	eq := strings.Index(s, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:eq])
	value = strings.TrimSpace(s[eq+1:])
	value = strings.Trim(value, `"`)
	return key, value, true
}
