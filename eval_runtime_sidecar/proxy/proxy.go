package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type contextKeyAuthInput struct{}

// ContextWithAuthInput returns a context that carries authInput for the reverse proxy Director.
func ContextWithAuthInput(ctx context.Context, authInput AuthTokenInput) context.Context {
	return context.WithValue(ctx, contextKeyAuthInput{}, authInput)
}

// AuthInputFromContext returns the AuthTokenInput from ctx, or a zero value if none.
func AuthInputFromContext(ctx context.Context) (AuthTokenInput, bool) {
	v := ctx.Value(contextKeyAuthInput{})
	if v == nil {
		return AuthTokenInput{}, false
	}
	a, ok := v.(AuthTokenInput)
	return a, ok
}

// headersForLog returns a copy of h suitable for logging, with Authorization values obfuscated.
func headersForLog(h http.Header) http.Header {
	out := h.Clone()
	if v := out.Get("Authorization"); v != "" {
		if strings.HasPrefix(v, "Bearer ") {
			out.Set("Authorization", "Bearer ***")
		} else if strings.HasPrefix(v, "Basic ") {
			out.Set("Authorization", "Basic ***")
		} else {
			out.Set("Authorization", "***")
		}
	} else {
		out.Set("Authorization", "Empty")
	}
	return out
}

// roundTripperFromClient adapts *http.Client to http.RoundTripper so ReverseProxy can use client's Transport, timeout, etc.
type roundTripperFromClient struct {
	client *http.Client
}

func (r *roundTripperFromClient) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.client.Do(req)
}

// SetAuthHeader sets the Authorization header on req if token is non-empty.
// If token does not already start with "Bearer " or "Basic ", it is prefixed with "Bearer ".
func SetAuthHeader(req *http.Request, token string) {
	if token == "" {
		return
	}
	if !strings.HasPrefix(token, "Bearer ") && !strings.HasPrefix(token, "Basic ") {
		token = "Bearer " + token
	}
	req.Header.Set("Authorization", token)
}

// NewReverseProxy returns an httputil.ReverseProxy that forwards to target using client.
// Per-request auth is read from the request context (ContextWithAuthInput). Logger is used for request/response logging.
// The returned proxy is safe to reuse for many requests.
func NewReverseProxy(target *url.URL, client *http.Client, logger *slog.Logger) *httputil.ReverseProxy {
	transport := &roundTripperFromClient{client: client}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		req.RequestURI = "" // required for client requests
		// Content-Type and X-Tenant are already on req (copied from incoming by ReverseProxy)
		authInput, ok := AuthInputFromContext(req.Context())
		if ok {
			authToken := ResolveAuthToken(logger, authInput)
			SetAuthHeader(req, authToken)
		}
		logger.Info("Proxying request", "method", req.Method, "url", req.URL.String(), "headers", headersForLog(req.Header))
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Request != nil {
			logger.Info("Response from proxy", "method", resp.Request.Method, "url", resp.Request.URL.String(), "status", resp.StatusCode)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		logger.Error("Error proxying request", "method", req.Method, "url", req.URL.String(), "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	return proxy
}
