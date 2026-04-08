package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const modelProxyPathPrefix = "/model"

// IsModelProxyPath reports whether path should be handled by the model reverse proxy (/model or /model/...).
// It does not match paths like /modelExtra (no boundary after /model).
func IsModelProxyPath(path string) bool {
	if !strings.HasPrefix(path, modelProxyPathPrefix) {
		return false
	}
	if len(path) == len(modelProxyPathPrefix) {
		return true
	}
	return path[len(modelProxyPathPrefix)] == '/'
}

// stripModelProxyPathPrefix removes the /model prefix so the request path matches the upstream OpenAI base path.
func stripModelProxyPathPrefix(p string) string {
	if p == modelProxyPathPrefix {
		return "/"
	}
	if strings.HasPrefix(p, modelProxyPathPrefix+"/") {
		return strings.TrimPrefix(p, modelProxyPathPrefix)
	}
	return p
}

// NewModelReverseProxy returns a reverse proxy to the model origin (scheme + host only).
// It strips the /model prefix from the request path, then forwards. Authorization is set in the Director
// using ResolveAuthToken from context (TargetEndpoint "model"); when auth_api_key_path is unset, the pod
// default service account token path is used (see handlers.ServiceAccountTokenPathDefault).
func NewModelReverseProxy(target *url.URL, client *http.Client, logger *slog.Logger) *httputil.ReverseProxy {
	transport := &roundTripperFromClient{client: client}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

	proxy.Director = func(req *http.Request) {
		req.URL.Path = stripModelProxyPathPrefix(req.URL.Path)
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		req.RequestURI = ""

		authInput, ok := AuthInputFromContext(req.Context())
		if ok {
			authToken := ResolveAuthToken(logger, authInput)
			SetAuthHeader(req, authToken)
		}
		logger.Info("Model proxy request", "method", req.Method, "url", req.URL.String(), "headers", headersForLog(req.Header))
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Request != nil {
			logger.Info("Response from model proxy", "method", resp.Request.Method, "url", resp.Request.URL.String(), "status", resp.StatusCode)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		logger.Error("Error proxying model request", "method", req.Method, "url", req.URL.String(), "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	return proxy
}
