package ociclient

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const dockerConfigJSONKey = ".dockerconfigjson"

// Credentials holds registry username and password.
type Credentials struct {
	Username string
	Password string
}

type registryAuthEntry struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type registryAuthConfig struct {
	Auths map[string]registryAuthEntry `json:"auths"`
}

// ParseDockerConfigJSON extracts registry username and password for registryHost from the
// JSON payload stored in a kubernetes.io/dockerconfigjson secret (.dockerconfigjson key).
// It is required because eval-hub reads tenant secrets on demand at export time rather than
// mounting them on the service pod; the secret format follows Docker's config.json layout.
func ParseDockerConfigJSON(data []byte, registryHost string) (Credentials, error) {
	var cfg registryAuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Credentials{}, fmt.Errorf("parse registry auth config: %w", err)
	}
	if cfg.Auths == nil {
		return Credentials{}, fmt.Errorf("registry auth config: no auths")
	}
	canonicalHost := canonicalRegistryHost(registryHost)
	if canonicalHost == "" {
		return Credentials{}, fmt.Errorf("registry auth config: empty or unparseable registry host")
	}
	var auth registryAuthEntry
	var found bool
	for key, entry := range cfg.Auths {
		if canonicalRegistryHost(key) == canonicalHost {
			auth = entry
			found = true
			break
		}
	}
	if !found {
		return Credentials{}, fmt.Errorf("registry auth config: no auth for registry %s", registryHost)
	}
	username := auth.Username
	password := auth.Password
	if username == "" || password == "" {
		if auth.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
			if err != nil {
				return Credentials{}, fmt.Errorf("decode auth: %w", err)
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				username = parts[0]
				password = parts[1]
			}
		}
	}
	if username == "" || password == "" {
		return Credentials{}, fmt.Errorf("registry auth config: missing username/password for registry")
	}
	return Credentials{Username: username, Password: password}, nil
}

// DockerConfigJSONFromSecret returns the .dockerconfigjson entry from a Kubernetes Secret's
// data map. Kubernetes stores docker registry credentials under that fixed key name for secrets
// of type kubernetes.io/dockerconfigjson; this helper isolates that convention from callers.
func DockerConfigJSONFromSecret(data map[string][]byte) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("secret data is nil")
	}
	raw, ok := data[dockerConfigJSONKey]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("secret missing %q entry", dockerConfigJSONKey)
	}
	return raw, nil
}

// canonicalRegistryHost returns a normalized host string for comparing registry identifiers.
// Docker config auths keys and job export coordinates may spell the same registry differently
// (e.g. "quay.io", "https://quay.io/", "QUAY.IO"); normalization lowercases the host, strips
// scheme and path, removes trailing slashes, and maps Docker Hub aliases
// (index.docker.io, registry-1.docker.io) to docker.io so credential lookup is reliable.
func canonicalRegistryHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	host = strings.ToLower(host)
	raw := host
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil || parsed.Host == "" {
		host = raw
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		host = strings.Trim(host, "/")
		if i := strings.IndexByte(host, '/'); i >= 0 {
			host = host[:i]
		}
	} else {
		host = parsed.Host
	}
	host = strings.TrimSuffix(host, "/")
	switch host {
	case "index.docker.io", "registry-1.docker.io":
		host = "docker.io"
	}
	return host
}

// NormalizeRegistryHost returns a registry base URL with an explicit scheme (https by default).
// OCI Distribution API requests need a full origin (scheme + host); export coordinates from job
// specs often omit the scheme, so this converts user-facing host values into a usable base URL.
func NormalizeRegistryHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	return strings.TrimSuffix(host, "/")
}
