package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
)

// TokenResponse is the JSON response from an OCI registry token endpoint.
type TokenResponse struct {
	Token       string `json:"token,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
}

// OCITokenProducer holds credentials and registry context for obtaining an OCI registry token.
// Values come from the OCI secret mounted on the container (registry auth config file).
// Registry holds the registry host as passed to LoadTokenProducerFromOCISecret (may include
// http:// or https://) so challenge() can use http when the job spec uses an http registry.
type OCITokenProducer struct {
	Username   string
	Password   string
	Registry   string
	Repository string
	token      atomic.Pointer[string] // OCI bearer token; use GetToken / SetToken
	HTTPClient *http.Client           // from NewOCIHTTPClient: TLS, timeout for challenge + token
}

// GetToken returns the current OCI registry bearer token, or "" if none.
func (tp *OCITokenProducer) GetToken() string {
	if tp == nil {
		return ""
	}
	p := tp.token.Load()
	if p == nil {
		return ""
	}
	return *p
}

// SetToken stores the OCI bearer token; empty s clears it.
func (tp *OCITokenProducer) SetToken(s string) {
	if tp == nil {
		return
	}
	if s == "" {
		tp.token.Store(nil)
		return
	}
	copy := s
	tp.token.Store(&copy)
}

// registryAuthEntry represents one registry entry in the auth config (format matches Docker config.json / kubernetes.io/dockerconfigjson).
type registryAuthEntry struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// registryAuthConfig is the structure of the mounted OCI/registry auth file (same layout as ~/.docker/config.json).
type registryAuthConfig struct {
	Auths map[string]registryAuthEntry `json:"auths"`
}

// GetOCICoordinatesFromJobSpec reads the job spec at path (e.g. /meta/job.json) and returns the OCI registry host
// and repository from exports.oci.coordinates using shared.JobSpec. Host is normalized to a URL (https:// if no scheme).
// Returns ("", "", nil) when the file has no OCI exports; returns ("", "", err) on read/parse errors.
func GetOCICoordinatesFromJobSpec(path string) (host, repository string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read job spec: %w", err)
	}
	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return "", "", fmt.Errorf("parse job spec: %w", err)
	}
	if spec.Exports == nil || spec.Exports.OCI == nil {
		return "", "", nil
	}
	host = strings.TrimSpace(spec.Exports.OCI.Coordinates.OCIHost)
	repository = strings.TrimSpace(spec.Exports.OCI.Coordinates.OCIRepository)
	if host == "" {
		return "", "", nil
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	return host, repository, nil
}

// LoadTokenProducerFromOCISecret reads the OCI secret (registry auth config) at ociSecretMountPath and builds a TokenProducer
// for the given registry host. httpClient must be non-nil (typically NewOCIHTTPClient) so challenge/token use configured TLS and timeout.
// The file format is the same as Docker config.json and kubernetes.io/dockerconfigjson
// (auths map with per-registry username/password or auth base64). RegistryHost should match the key in auths
// (e.g. "https://registry:5000" or "registry:5000"). Repository is used as the scope in the token request; if empty, "default/repo" is used.
func LoadTokenProducerFromOCISecret(ociSecretMountPath, registryHost, repository string, httpClient *http.Client) (*OCITokenProducer, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("oci http client is required")
	}
	data, err := os.ReadFile(ociSecretMountPath)
	if err != nil {
		return nil, fmt.Errorf("read OCI secret: %w", err)
	}
	var cfg registryAuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse registry auth config: %w", err)
	}
	if cfg.Auths == nil {
		return nil, fmt.Errorf("registry auth config: no auths")
	}
	want := canonicalRegistryHost(registryHost)
	if want == "" {
		return nil, fmt.Errorf("registry auth config: empty or unparseable registry host")
	}
	var auth registryAuthEntry
	var found bool
	for k, v := range cfg.Auths {
		if canonicalRegistryHost(k) == want {
			auth = v
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("registry auth config: no auth for registry %s (no auths key matches canonical host %q)", registryHost, want)
	}
	username := auth.Username
	password := auth.Password
	if username == "" || password == "" {
		if auth.Auth != "" {
			dec, err := base64.StdEncoding.DecodeString(auth.Auth)
			if err != nil {
				return nil, fmt.Errorf("decode auth: %w", err)
			}
			parts := strings.SplitN(string(dec), ":", 2)
			if len(parts) == 2 {
				username = parts[0]
				password = parts[1]
			}
		}
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("registry auth config: missing username/password for registry")
	}
	if repository == "" {
		repository = "default/repo"
	}
	return &OCITokenProducer{
		Username:   username,
		Password:   password,
		Registry:   strings.TrimSpace(registryHost),
		Repository: repository,
		HTTPClient: httpClient,
	}, nil
}

// canonicalRegistryHost returns a comparable host form for matching dockerconfigjson auths keys
// to the job-spec registry: lowercase, scheme stripped, path discarded (host only), trailing
// slashes removed, and Docker Hub aliases (index.docker.io, registry-1.docker.io) mapped to docker.io.
func canonicalRegistryHost(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	raw := s
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		s = raw
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		s = strings.Trim(s, "/")
		if i := strings.IndexByte(s, '/'); i >= 0 {
			s = s[:i]
		}
	} else {
		s = u.Host
	}
	s = strings.TrimSuffix(s, "/")
	switch s {
	case "index.docker.io", "registry-1.docker.io":
		s = "docker.io"
	}
	return s
}

// parseBearerRealm parses WWW-Authenticate: Bearer realm="...",service="...",scope="..."
// and returns the realm URL with query (service and scope as query params).
func parseBearerRealm(header string) (string, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return "", fmt.Errorf("not a Bearer challenge")
	}
	header = header[7:]
	var realm, service, scope string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if k, v, ok := parseParam(part); ok {
			switch k {
			case "realm":
				realm = v
			case "service":
				service = v
			case "scope":
				scope = v
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
		realm += sep + "service=" + service
		sep = "&"
	}
	if scope != "" {
		realm += sep + "scope=" + scope
	}
	return realm, nil
}

func parseParam(s string) (key, value string, ok bool) {
	eq := strings.Index(s, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:eq])
	value = strings.TrimSpace(s[eq+1:])
	value = strings.Trim(value, `"`)
	return key, value, true
}

func (tp *OCITokenProducer) initiateChallenge() (string, error) {
	scheme := "https"
	if strings.HasPrefix(tp.Registry, "http://") {
		scheme = "http"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(tp.Registry, "https://"), "http://")
	authURL := scheme + "://" + host + "/v2"
	req, err := http.NewRequest(http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := tp.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

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
	// If scope was not in challenge, add default scope
	if !strings.Contains(nextURL, "scope=") {
		if strings.Contains(nextURL, "?") {
			nextURL += "&scope=repository:" + tp.Repository + ":push"
		} else {
			nextURL += "?scope=repository:" + tp.Repository + ":push"
		}
	}
	return nextURL, nil
}

func (tp *OCITokenProducer) createNewToken(nextURL string) error {
	req, err := http.NewRequest(http.MethodGet, nextURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(tp.Username, tp.Password)

	resp, err := tp.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var tokenData TokenResponse
	if err := json.Unmarshal(body, &tokenData); err != nil {
		return err
	}

	if tokenData.Token != "" {
		tp.SetToken(tokenData.Token)
	} else if tokenData.AccessToken != "" {
		tp.SetToken(tokenData.AccessToken)
	}
	return nil
}

// RefreshToken performs the OCI registry auth flow (challenge + token) and stores the token atomically.
func (tp *OCITokenProducer) RefreshToken() error {
	nextURL, err := tp.initiateChallenge()
	if err != nil {
		return err
	}
	if nextURL == "" {
		return nil
	}
	return tp.createNewToken(nextURL)
}

// ModifyOCIRegistryResponse applies OCI/registry-specific response tweaks (same ideas as oci-proxy):
// strip WWW-Authenticate; rewrite absolute redirect Location to the client-facing host; if the registry
// returns a Bearer token in Authorization, store it on TokenProducer and cache, and strip from response.
func ModifyOCIRegistryResponse(resp *http.Response, logger *slog.Logger, tokenProducer *OCITokenProducer) {
	if resp == nil {
		return
	}
	if resp.Header.Get("WWW-Authenticate") != "" {
		resp.Header.Del("WWW-Authenticate")
	}

	if resp.Request != nil {
		if orig, ok := OriginalRequestFromContext(resp.Request.Context()); ok {
			ociRewriteLocationHeader(resp, orig)
		}
	}

	ociConsumeResponseAuthorizationToken(resp, tokenProducer, logger)
}

func ociRewriteLocationHeader(resp *http.Response, client OriginalRequest) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	locURL, err := url.Parse(loc)
	if err != nil || locURL.Host == "" {
		return
	}
	locURL.Scheme = client.Scheme
	locURL.Host = client.Host
	resp.Header.Set("Location", locURL.String())
}

func ociConsumeResponseAuthorizationToken(resp *http.Response, tokenProducer *OCITokenProducer, logger *slog.Logger) {
	if tokenProducer == nil || resp.Request == nil {
		return
	}
	authz := strings.TrimSpace(resp.Header.Get("Authorization"))
	if authz == "" {
		return
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return
	}
	newTok := strings.TrimSpace(authz[len(prefix):])
	if newTok == "" {
		return
	}
	resp.Header.Del("Authorization")

	tokenProducer.SetToken(newTok)

	if input, ok := AuthInputFromContext(resp.Request.Context()); ok {
		UpdateCachedToken(input, newTok)
	}
	if logger != nil {
		logger.Debug("OCI proxy: stored registry token from upstream Authorization response header")
	}
}
