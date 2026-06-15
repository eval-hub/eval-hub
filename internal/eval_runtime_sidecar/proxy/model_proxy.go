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
// A Bearer token "api-key:ref" means: look up "api-key" in the real secret mount.
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
// Per-request behaviour:
//  1. If the Authorization header carries a ref token (e.g. "Bearer model-1_api-key:ref"):
//     a. The real credential is read from realSecretMountPath/model-1_api-key.
//     b. The upstream URL is determined by reading realSecretMountPath/model-1_url
//     (derived by replacing "_api-key" → "_url"). Falls back to defaultTarget when
//     the URL file is absent (single-model / hf-token case).
//  2. If resolution fails (missing file, empty value, path traversal) the proxy returns
//     HTTP 400 to the eval container — the request is never forwarded.
//  3. Non-ref tokens are forwarded unchanged to defaultTarget.
func NewModelReverseProxy(defaultTarget *url.URL, client *http.Client, logger *slog.Logger, realSecretMountPath string) *httputil.ReverseProxy {
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
			resolvedTarget, realToken, err := resolveModelCredential(logger, reqID, authHeader, realSecretMountPath, defaultTarget)
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

// resolveModelCredential resolves a Bearer ref token to a (upstream URL, real credential) pair.
//
// Key derivation for the upstream URL:
//   - "model-1_api-key:ref" → reads realSecretMountPath/model-1_api-key (token) and
//     realSecretMountPath/model-1_url (URL); falls back to defaultTarget if _url absent.
//   - "api-key:ref" / "hf-token:ref" → reads the credential file; always uses defaultTarget.
func resolveModelCredential(logger *slog.Logger, requestID, authHeader, realSecretMountPath string, defaultTarget *url.URL) (*url.URL, string, error) {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	key := strings.TrimSuffix(token, modelRefSuffix)

	if key == "" || strings.ContainsAny(key, "/\\") {
		return nil, "", fmt.Errorf("model ref token has invalid key %q", key)
	}

	realToken, err := readSecretFile(realSecretMountPath, key)
	if err != nil {
		logger.Error("failed to read model credential", "request_id", requestID, "key", key)
		return nil, "", fmt.Errorf("credential not found for key %q", key)
	}
	if realToken == "" {
		return nil, "", fmt.Errorf("credential for key %q is empty", key)
	}

	target := defaultTarget

	// For multi-model keys (*_api-key), try to read the corresponding URL file.
	if strings.HasSuffix(key, modelAPIKeySuffix) {
		prefix := strings.TrimSuffix(key, modelAPIKeySuffix)
		urlKey := prefix + modelURLSuffix
		rawURL, urlErr := readSecretFile(realSecretMountPath, urlKey)
		if urlErr == nil && rawURL != "" {
			parsed, parseErr := url.Parse(strings.TrimSuffix(rawURL, "/"))
			if parseErr != nil || !parsed.IsAbs() || parsed.Host == "" {
				logger.Warn("model URL file has invalid or relative URL, using default target",
					"request_id", requestID, "url_key", urlKey, "error", parseErr)
			} else {
				target = parsed
			}
		}
	}

	logger.Info("Resolved model ref token", "request_id", requestID, "key", key, "target_host", target.Host)
	return target, realToken, nil
}

// readSecretFile reads and trims a single file from a Kubernetes secret mount.
func readSecretFile(mountPath, key string) (string, error) {
	data, err := os.ReadFile(filepath.Join(mountPath, key))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
