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

// modelAPIKeySuffix and modelURLSuffix are the key-naming conventions for multi-model secrets.
const (
	modelAPIKeySuffix = "_api-key"
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
//  1. If the Authorization header carries a ref token (e.g. "Bearer model-1_api-key:ref"):
//     a. The credential is looked up from the in-memory cache by key name.
//     b. The upstream URL is looked up from the cache under model-1_url; falls back to
//     defaultTarget when absent (single-model case).
//  2. If resolution fails (key not in cache, path traversal) the proxy returns HTTP 400
//     to the eval container — the request is never forwarded.
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
			resolvedTarget, realToken, err := resolveModelCredential(logger, reqID, authHeader, secretCache, defaultTarget)
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
		if v := strings.TrimSpace(string(data)); v != "" {
			cache[e.Name()] = v
		}
	}
	keys := make([]string, 0, len(cache))
	for k := range cache {
		keys = append(keys, k)
	}
	logger.Info("Loaded model secret cache", "path", mountPath, "keys", keys)
	return cache
}

// resolveModelCredential resolves a Bearer ref token to a (upstream URL, credential) pair
// using the pre-loaded in-memory secret cache.
//
// Key derivation for the upstream URL:
//   - "model-1_api-key:ref" → looks up "model-1_api-key" (token) and "model-1_url" (URL)
//     in secretCache; falls back to defaultTarget when the URL key is absent.
//   - "api-key:ref" → looks up the credential; always uses defaultTarget.
func resolveModelCredential(logger *slog.Logger, requestID, authHeader string, secretCache map[string]string, defaultTarget *url.URL) (*url.URL, string, error) {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	key := strings.TrimSuffix(token, modelRefSuffix)

	if key == "" || strings.ContainsAny(key, "/\\") {
		return nil, "", fmt.Errorf("model ref token has invalid key %q", key)
	}

	realToken, ok := secretCache[key]
	if !ok {
		logger.Error("model credential not found in cache", "request_id", requestID, "key", key)
		return nil, "", fmt.Errorf("credential not found for key %q", key)
	}
	if realToken == "" {
		return nil, "", fmt.Errorf("credential for key %q is empty", key)
	}

	target := defaultTarget

	// For multi-model keys (*_api-key), look up the corresponding URL entry.
	if strings.HasSuffix(key, modelAPIKeySuffix) {
		prefix := strings.TrimSuffix(key, modelAPIKeySuffix)
		urlKey := prefix + modelURLSuffix
		if rawURL, exists := secretCache[urlKey]; exists && rawURL != "" {
			parsed, parseErr := url.Parse(strings.TrimSuffix(rawURL, "/"))
			if parseErr != nil || !parsed.IsAbs() || parsed.Host == "" {
				logger.Warn("model URL in secret cache is invalid, using default target",
					"request_id", requestID, "url_key", urlKey, "error", parseErr)
			} else {
				target = parsed
			}
		}
	}

	logger.Info("Resolved model ref token", "request_id", requestID, "key", key, "target_host", target.Host)
	return target, realToken, nil
}
