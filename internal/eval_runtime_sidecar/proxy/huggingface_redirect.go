package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// ClientOriginFromRequest builds scheme://host as seen by the adapter (pod localhost:8080, etc.).
func ClientOriginFromRequest(req *http.Request) string {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if p := req.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := req.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func joinURLPathQueryFragment(u *url.URL) string {
	if u == nil {
		return ""
	}
	s := u.Path
	if s == "" {
		s = "/"
	}
	if u.RawQuery != "" {
		s += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		s += "#" + u.Fragment
	}
	return s
}

func isHuggingFaceHubHostname(host, configuredHubHost string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	configuredHubHost = strings.ToLower(strings.TrimSpace(configuredHubHost))
	if host == "" {
		return false
	}
	if configuredHubHost != "" && host == configuredHubHost {
		return true
	}
	return strings.HasSuffix(host, ".huggingface.co")
}

// rewriteHuggingFaceRedirectLocation rewrites the Location header on Hub 3xx responses so clients using
// HF_ENDPOINT (http://pod:8080/huggingface) stay under /huggingface instead of http://pod:8080/api/...
//
// huggingface_hub's get_hf_file_metadata uses HEAD with allow_redirects=False and only follows *relative*
// Location URLs (empty host). Absolute Locations are not followed, so HEAD stops on the 307 and reads
// wrong Content-Length / x-linked-size; GET then downloads the real file and hits a size mismatch.
// Emit relative paths such as /huggingface/api/resolve-cache/... so the client follows and metadata matches GET.
//
// Rewrites:
//   - Relative paths such as /api/resolve-cache/... → /huggingface/api/resolve-cache/...
//   - Absolute https://huggingface.co/... (and *.huggingface.co) → /huggingface/... (relative)
//
// Leaves other absolute URLs unchanged (e.g. https://cas-bridge.xethub.hf.co/...) so clients may follow CAS directly.
func rewriteHuggingFaceRedirectLocation(location, configuredHubHost string) string {
	u, err := url.Parse(location)
	if err != nil || strings.TrimSpace(location) == "" {
		return location
	}
	pq := joinURLPathQueryFragment(u)
	// Relative redirect (common for Hub resolve-cache).
	if u.Scheme == "" && strings.HasPrefix(u.Path, "/") {
		if u.Path == huggingfaceProxyPathPrefix || strings.HasPrefix(u.Path, huggingfaceProxyPathPrefix+"/") {
			return location
		}
		return huggingfaceProxyPathPrefix + pq
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return location
	}
	if !isHuggingFaceHubHostname(u.Hostname(), configuredHubHost) {
		return location
	}
	return huggingfaceProxyPathPrefix + pq
}
