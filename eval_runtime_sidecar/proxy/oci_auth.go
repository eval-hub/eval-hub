package proxy

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// TokenResponse is the JSON response from an OCI registry token endpoint.
type TokenResponse struct {
	Token string `json:"token"`
}

// TokenProducer holds credentials and registry context for obtaining an OCI registry token.
// Values come from the OCI secret mounted on the container (registry auth config file).
type TokenProducer struct {
	Username   string
	Password   string
	Registry   string
	Repository string
	Token      string
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

// GetRegistryHostFromAuthConfig reads the mounted registry auth config and returns the registry host (first key in auths).
// For OCI target the hostname comes from this mounted file, unlike eval-hub/mlflow where it comes from env or config.
func GetRegistryHostFromAuthConfig(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read registry auth config: %w", err)
	}
	var cfg registryAuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse registry auth config: %w", err)
	}
	if len(cfg.Auths) == 0 {
		return "", fmt.Errorf("registry auth config: no auths")
	}
	for k := range cfg.Auths {
		return k, nil
	}
	return "", nil
}

// LoadTokenProducerFromRegistryAuthConfig reads the registry auth config at configPath and builds a TokenProducer
// for the given registry host. The file format is the same as Docker config.json and kubernetes.io/dockerconfigjson
// (auths map with per-registry username/password or auth base64). RegistryHost should match the key in auths
// (e.g. "https://registry:5000" or "registry:5000"). Repository is used as the scope in the token request; if empty, "default/repo" is used.
func LoadTokenProducerFromRegistryAuthConfig(configPath, registryHost, repository string) (*TokenProducer, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read registry auth config: %w", err)
	}
	var cfg registryAuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse registry auth config: %w", err)
	}
	if cfg.Auths == nil {
		return nil, fmt.Errorf("registry auth config: no auths")
	}
	// Match registry: exact key, then normalized host, then single auth entry
	normHost := normalizeRegistryKey(registryHost)
	var auth registryAuthEntry
	var found bool
	for k, v := range cfg.Auths {
		if k == registryHost || normalizeRegistryKey(k) == normHost {
			auth = v
			found = true
			break
		}
	}
	if !found && len(cfg.Auths) == 1 {
		for _, v := range cfg.Auths {
			auth = v
			found = true
			break // one entry; map has no index by position, so range is the only way to get it
		}
	}
	if !found {
		return nil, fmt.Errorf("registry auth config: no auth for registry %s", registryHost)
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
	return &TokenProducer{
		Username:   username,
		Password:   password,
		Registry:   strings.TrimPrefix(strings.TrimPrefix(registryHost, "https://"), "http://"),
		Repository: repository,
	}, nil
}

func normalizeRegistryKey(s string) string {
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
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

func (tp *TokenProducer) challenge() (string, error) {
	scheme := "https"
	if strings.HasPrefix(tp.Registry, "http://") {
		scheme = "http"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(tp.Registry, "https://"), "http://")
	authURL := scheme + "://" + host + "/v2"
	resp, err := http.Get(authURL)
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
			nextURL += "&scope=repository:" + tp.Repository + ":push,pull"
		} else {
			nextURL += "?scope=repository:" + tp.Repository + ":push,pull"
		}
	}
	return nextURL, nil
}

func (tp *TokenProducer) token(nextURL string) error {
	req, err := http.NewRequest(http.MethodGet, nextURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(tp.Username, tp.Password)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(req)
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

	tp.Token = tokenData.Token
	return nil
}

// GetToken performs the OCI registry auth flow (challenge + token) and sets tp.Token.
func (tp *TokenProducer) GetToken() error {
	nextURL, err := tp.challenge()
	if err != nil {
		return err
	}
	if nextURL == "" {
		return nil
	}
	return tp.token(nextURL)
}
