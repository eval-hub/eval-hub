package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// modelRefSuffix is the value suffix that signals credential injection.
// A Bearer token "api-key:ref" means: look up "api-key" in the secret cache.
const modelRefSuffix = ":ref"

// Key-naming conventions for multi-service secrets.
//
// _api-key holds an opaque API key forwarded as-is.
// _token holds a bearer token; when its value is empty the sidecar injects the pod SA token —
// this is the mechanism for KFP and other SA-token-authenticated in-cluster services.
// _url holds the real upstream URL for the corresponding prefix.
const (
	modelSingleAPIKey = "api-key"
	modelAPIKeySuffix = "_api-key"
	modelTokenSuffix  = "_token"
	modelURLSuffix    = "_url"
)

// xModelCredError is an internal sentinel header set by the Director when ref resolution fails.
// The modelRoundTripper checks for it and returns 400 to the eval container instead of
// forwarding a request with a literal ref token.
const xModelCredError = "X-Model-Cred-Error"

const globalTransactionIDHeader = "X-Global-Transaction-Id"

// getOrCreateRequestID returns X-Global-Transaction-Id from req or a new UUID.
func getOrCreateRequestID(req *http.Request) string {
	if req != nil {
		if id := strings.TrimSpace(req.Header.Get(globalTransactionIDHeader)); id != "" {
			return id
		}
	}
	return uuid.New().String()
}

func loggerForRequest(logger *slog.Logger, req *http.Request) *slog.Logger {
	return logger.With("request_id", getOrCreateRequestID(req))
}

// NewModelReverseProxy returns an httputil.ReverseProxy that performs model credential injection.
//
// All credential files under secretMountPath are read once at construction time into an
// in-memory cache. The mount is a static Kubernetes secret volume — files do not change
// for the pod's lifetime — so per-request file reads are unnecessary.
//
// Per-request behaviour:
//  1. If the Authorization header carries a ref token (e.g. "Bearer kfp_token:ref"):
//     a. The credential is looked up from the in-memory cache by key name.
//     b. For *_api-key keys: credential must be non-empty; upstream URL comes from *_url.
//     c. For *_token keys: when the secret value is empty the SA token from saTokenPath is
//     injected instead — this is the KFP path (kfp_token: "" in the secret, SA injected).
//     d. The upstream URL is looked up from the cache under *_url for both suffixes;
//     falls back to defaultTarget when absent.
//  2. If resolution fails (key not in cache, path traversal, empty _api-key) the proxy
//     returns HTTP 400 to the eval container — the request is never forwarded.
//  3. If no Authorization header is present, the SA token from saTokenPath is injected as
//     a Bearer token. This covers SA-token-authenticated models: the adapter has no access
//     to the SA token (pod-level auto-mount is disabled), so the sidecar injects it on
//     its behalf.
//  4. Non-ref, non-empty tokens are forwarded unchanged to defaultTarget.
func NewModelReverseProxy(defaultTarget *url.URL, client *http.Client, logger *slog.Logger, secretMountPath, saTokenPath string) *httputil.ReverseProxy {
	secretCache := loadSecretCache(secretMountPath, logger)

	rp := &httputil.ReverseProxy{
		Transport: &modelRoundTripper{
			inner:  &roundTripperFromClient{client: client},
			logger: logger,
		},
	}

	rp.Rewrite = func(pr *httputil.ProxyRequest) {
		reqID := getOrCreateRequestID(pr.In)
		reqLog := logger.With("request_id", reqID)
		// Propagate the request ID to the upstream service.
		pr.Out.Header.Set(globalTransactionIDHeader, reqID)

		target := defaultTarget

		authHeader := pr.In.Header.Get("Authorization")
		if isModelRefToken(authHeader) {
			resolvedTarget, realToken, err := resolveModelCredential(reqLog, authHeader, secretCache, defaultTarget, saTokenPath)
			if err != nil {
				// Signal the RoundTripper to return 400 without forwarding.
				pr.Out.Header.Set(xModelCredError, err.Error())
				pr.Out.URL.Scheme = defaultTarget.Scheme
				pr.Out.URL.Host = defaultTarget.Host
				pr.Out.Host = defaultTarget.Host
				pr.Out.RequestURI = ""
				return
			}
			target = resolvedTarget
			SetAuthHeader(pr.Out, realToken)
		} else if isBearerEmpty(authHeader) {
			// No usable Authorization from adapter (absent or empty Bearer value). The adapter
			// cannot read the SA token because pod-level auto-mount is disabled. Inject it here
			// so SA-token-authenticated model endpoints receive a valid Bearer token.
			if tok := resolveEvalHubOrMLflowToken(reqLog, AuthTokenInput{
				TargetEndpoint: "model-sa",
				AuthTokenPath:  saTokenPath,
			}); tok != "" {
				SetAuthHeader(pr.Out, tok)
				reqLog.Info("Injected SA token for model request (no Authorization from adapter)")
			} else {
				reqLog.Warn("SA token injection skipped: token unavailable", "path", saTokenPath)
			}
		}

		// Set only scheme and host from the target — do not join the target path with the
		// incoming path. The incoming path already contains the full API path (e.g. /v1/completions)
		// so prepending the target's path prefix (e.g. /v1) would produce /v1/v1/completions.
		pr.Out.URL.Scheme = target.Scheme
		pr.Out.URL.Host = target.Host
		pr.Out.Host = target.Host
		pr.Out.RequestURI = ""
		reqLog.Info("Proxying model request", "method", pr.Out.Method, "url", pr.Out.URL.String(), "headers", headersForLog(pr.Out.Header))
	}

	rp.ModifyResponse = func(resp *http.Response) error {
		if resp.Request != nil {
			loggerForRequest(logger, resp.Request).Info("Response from model proxy", "method", resp.Request.Method, "url", resp.Request.URL.String(), "status", resp.StatusCode)
		}
		return nil
	}

	rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		loggerForRequest(logger, req).Error("Error proxying model request", "method", req.Method, "url", req.URL.String(), "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	return rp
}

// modelRoundTripper wraps an inner RoundTripper and intercepts requests marked with the
// xModelCredError sentinel header, returning 400 Bad Request without forwarding.
type modelRoundTripper struct {
	inner  http.RoundTripper
	logger *slog.Logger
}

func (t *modelRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if errMsg := req.Header.Get(xModelCredError); errMsg != "" {
		req.Header.Del(xModelCredError)
		reqID := getOrCreateRequestID(req)
		t.logger.Error("model credential resolution failed, returning 400", "request_id", reqID, "error", errMsg)
		respHeader := make(http.Header)
		respHeader.Set(globalTransactionIDHeader, reqID)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader(errMsg + "\n")),
			Header:     respHeader,
			Request:    req,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
		}, nil
	}
	return t.inner.RoundTrip(req)
}

// isModelRefToken reports whether authHeader is a Bearer ref token.
func isModelRefToken(authHeader string) bool {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	return strings.HasSuffix(strings.TrimPrefix(authHeader, "Bearer "), modelRefSuffix)
}

// isBearerEmpty reports whether authHeader carries no usable token.
//
// Returns true when the header is absent, is exactly "Bearer" (Go's HTTP parser
// strips the trailing space from "Bearer " sent by Python's requests library when
// OPENAI_API_KEY=""), or is "Bearer " with a whitespace-only value.
func isBearerEmpty(authHeader string) bool {
	if authHeader == "" || authHeader == "Bearer" {
		return true
	}
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer ")) == ""
	}
	return false
}

// loadSecretCache reads all files in mountPath into a map at proxy startup.
// The model secret mount is static for the pod's lifetime, so this avoids
// per-request file reads. Returns an empty map if mountPath is empty or unreadable.
func loadSecretCache(mountPath string, logger *slog.Logger) map[string]string {
	cache := make(map[string]string)
	if mountPath == "" {
		return cache
	}
	entries, err := os.ReadDir(mountPath)
	if err != nil {
		logger.Warn("model secret mount unreadable; credential resolution will fail for ref tokens", "path", mountPath, "error", err)
		return cache
	}
	for _, e := range entries {
		// Skip directories and Kubernetes secret mount internals (..data, ..2024_... symlinks).
		if e.IsDir() || strings.HasPrefix(e.Name(), "..") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(mountPath, e.Name()))
		if err != nil {
			logger.Warn("skipping unreadable secret file", "file", e.Name(), "error", err)
			continue
		}
		// Store all values including empty ones: _token keys may legitimately be empty
		// (signals SA token injection), and we need to distinguish "key present, value
		// empty" from "key absent".
		cache[e.Name()] = strings.TrimSpace(string(data))
	}
	keys := make([]string, 0, len(cache))
	for k := range cache {
		keys = append(keys, k)
	}
	logger.Info("Loaded model secret cache", "path", mountPath, "keys", keys)
	return cache
}

// isCredentialKey reports whether key is a valid credential ref key.
// Only "api-key", *_api-key, and *_token keys may appear as Bearer ref tokens.
// _url and other keys are not credentials and must not be forwarded as tokens.
func isCredentialKey(key string) bool {
	return key == modelSingleAPIKey ||
		strings.HasSuffix(key, modelAPIKeySuffix) ||
		strings.HasSuffix(key, modelTokenSuffix)
}

// resolveModelCredential resolves a Bearer ref token to a (upstream URL, credential) pair
// using the pre-loaded in-memory secret cache.
//
// Key derivation:
//   - "*_api-key:ref" → credential must be non-empty; upstream URL from *_url.
//   - "*_token:ref"   → when secret value is empty, the SA token from saTokenPath is injected
//     instead (KFP path); upstream URL from *_url.
//   - "api-key:ref"   → single-model shorthand; always uses defaultTarget.
//
// Any other key suffix (e.g. *_url) is rejected with an error — non-credential
// keys must not be forwarded as bearer tokens.
func resolveModelCredential(logger *slog.Logger, authHeader string, secretCache map[string]string, defaultTarget *url.URL, saTokenPath string) (*url.URL, string, error) {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	key := strings.TrimSuffix(token, modelRefSuffix)

	if key == "" || strings.ContainsAny(key, "/\\") {
		return nil, "", fmt.Errorf("model ref token has invalid key %q", key)
	}

	// Reject non-credential key suffixes early — _url and any unknown suffixes
	// must not be resolved as bearer tokens.
	if !isCredentialKey(key) {
		return nil, "", fmt.Errorf("ref key %q is not a credential key (must be api-key, *_api-key, or *_token)", key)
	}

	secretValue, ok := secretCache[key]
	if !ok {
		logger.Error("model credential not found in cache", "key", key)
		return nil, "", fmt.Errorf("credential not found for key %q", key)
	}

	var realToken string
	switch {
	case strings.HasSuffix(key, modelTokenSuffix):
		// _token keys: empty value means inject the pod SA token.
		if secretValue != "" {
			realToken = secretValue
		} else {
			realToken = resolveEvalHubOrMLflowToken(logger, AuthTokenInput{
				TargetEndpoint: "model-sa",
				AuthTokenPath:  saTokenPath,
			})
			if realToken == "" {
				return nil, "", fmt.Errorf("_token credential for key %q is empty and SA token is unavailable", key)
			}
			logger.Info("Injected SA token for _token credential", "key", key)
		}
	default:
		// api-key and *_api-key: value must be non-empty.
		if secretValue == "" {
			return nil, "", fmt.Errorf("credential for key %q is empty", key)
		}
		realToken = secretValue
	}

	target := resolveUpstreamURL(logger, key, secretCache, defaultTarget)
	logger.Info("Resolved model ref token", "key", key, "target_host", target.Host)
	return target, realToken, nil
}

// resolveUpstreamURL returns the upstream URL for a secret key by stripping the credential
// suffix (_api-key or _token) and looking up <prefix>_url in the cache.
// Falls back to defaultTarget when no URL entry exists or the entry is invalid.
func resolveUpstreamURL(logger *slog.Logger, key string, secretCache map[string]string, defaultTarget *url.URL) *url.URL {
	var prefix string
	switch {
	case strings.HasSuffix(key, modelAPIKeySuffix):
		prefix = strings.TrimSuffix(key, modelAPIKeySuffix)
	case strings.HasSuffix(key, modelTokenSuffix):
		prefix = strings.TrimSuffix(key, modelTokenSuffix)
	default:
		return defaultTarget
	}

	urlKey := prefix + modelURLSuffix
	rawURL, exists := secretCache[urlKey]
	if !exists || rawURL == "" {
		return defaultTarget
	}
	parsed, err := url.Parse(strings.TrimSuffix(rawURL, "/"))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		logger.Warn("service URL in secret cache is invalid, using default target",
			"url_key", urlKey, "error", err)
		return defaultTarget
	}
	return parsed
}
