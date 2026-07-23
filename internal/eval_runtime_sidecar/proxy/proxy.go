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

// ContextWithAuthInput returns a context that carries authInput for the reverse proxy Rewrite hook.
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

type contextKeyOriginalRequest struct{}

// OriginalRequest captures the client-to-sidecar request Host and scheme at proxy entry.
// Use with ContextWithOriginalRequest before ServeHTTP so ModifyResponse can rewrite redirects.
type OriginalRequest struct {
	Host   string
	Scheme string // "http" or "https"
}

// ContextWithOriginalRequest records r's Host and client-facing scheme on ctx.
func ContextWithOriginalRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, contextKeyOriginalRequest{}, OriginalRequest{
		Host:   r.Host,
		Scheme: clientScheme(r),
	})
}

// OriginalRequestFromContext returns the OriginalRequest from ctx, if any.
func OriginalRequestFromContext(ctx context.Context) (OriginalRequest, bool) {
	v := ctx.Value(contextKeyOriginalRequest{})
	if v == nil {
		return OriginalRequest{}, false
	}
	o, ok := v.(OriginalRequest)
	return o, ok
}

func clientScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
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
// If modifyResponse is non-nil, it runs before the built-in response log (same contract as httputil.ReverseProxy.ModifyResponse).
// The returned proxy is safe to reuse for many requests.
func NewReverseProxy(target *url.URL, client *http.Client, logger *slog.Logger, modifyResponse func(*http.Response) error) *httputil.ReverseProxy {
	transport := &roundTripperFromClient{client: client}
	proxy := &httputil.ReverseProxy{Transport: transport}

	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.RequestURI = "" // required for client requests
		// Content-Type and X-Tenant are already on Out (copied from inbound by ReverseProxy)
		reqID := getOrCreateRequestID(pr.In)
		pr.Out.Header.Set(globalTransactionIDHeader, reqID)
		reqLog := logger.With("request_id", reqID)
		authInput, ok := AuthInputFromContext(pr.In.Context())
		if ok {
			authToken := ResolveAuthToken(reqLog, authInput)
			SetAuthHeader(pr.Out, authToken)
		}
		// Do not log request headers: CodeQL treats http.Header as sensitive even when
		// Authorization is masked (go/clear-text-logging).
		reqLog.Info("Proxying request", "method", pr.Out.Method, "url", pr.Out.URL.String())
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		if modifyResponse != nil {
			if err := modifyResponse(resp); err != nil {
				return err
			}
		}
		if resp.Request != nil {
			loggerForRequest(logger, resp.Request).Info("Response from proxy", "method", resp.Request.Method, "url", resp.Request.URL.String(), "status", resp.StatusCode)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		loggerForRequest(logger, req).Error("Error proxying request", "method", req.Method, "url", req.URL.String(), "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	return proxy
}
