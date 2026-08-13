package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strings"
)

// ParseLocalModelPath extracts the job-id and remaining path from a /model/<job-id>/<path>
// path string. The input must be a clean path without query or fragment.
// Returns ("", "", false) if the path does not match the expected pattern.
func ParseLocalModelPath(path string) (jobID, remainingPath string, ok bool) {
	rest, found := strings.CutPrefix(path, "/model/")
	if !found || rest == "" {
		return "", "", false
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		if i == 0 {
			return "", "", false
		}
		return rest[:i], rest[i:], true
	}
	return rest, "", true
}

// Sentinel headers for local model proxy error signaling.
// The Rewrite function sets these when it cannot resolve a job; the round tripper
// intercepts them and returns the appropriate HTTP error without forwarding.
const (
	xLocalModelError      = "X-Local-Model-Error"        // #nosec G101 -- internal HTTP header, not a credential
	xLocalModelErrorJobID = "X-Local-Model-Error-Job-Id" // #nosec G101
)

type contextKeyJobHTTPClient struct{}

func jobClientFromContext(ctx context.Context) *http.Client {
	v, _ := ctx.Value(contextKeyJobHTTPClient{}).(*http.Client)
	return v
}

// NewLocalModelReverseProxy creates a reverse proxy for local mode that routes
// model requests per-job using the TTL cache. Each request to /model/<job-id>/...
// is forwarded to the upstream model URL from the job's sidecar-job-info.json,
// with the /model/<job-id> prefix stripped from the path.
// Each job gets its own HTTP client with TLS configured from the job's secret cache.
func NewLocalModelReverseProxy(cache *JobInfoCache, logger *slog.Logger) *httputil.ReverseProxy {
	rp := &httputil.ReverseProxy{
		Transport: &localModelRoundTripper{
			inner:  http.DefaultTransport,
			logger: logger,
		},
	}

	rp.Rewrite = func(pr *httputil.ProxyRequest) {
		reqID := getOrCreateRequestID(pr.In)
		reqLog := logger.With("request_id", reqID)
		pr.Out.Header.Set(globalTransactionIDHeader, reqID)

		jobID, remaining, ok := ParseLocalModelPath(pr.In.URL.Path)
		if !ok {
			reqLog.Error("Invalid model path", "path", pr.In.URL.Path, "method", pr.In.Method)
			pr.Out.Header.Set(xLocalModelError, "invalid model path format, expected /model/<job-id>/<path>")
			setDummyTarget(pr)
			return
		}

		target, jobClient, err := cache.Get(jobID)
		if err != nil {
			reqLog.Error("Job info lookup failed", "job_id", jobID, "error", err)
			pr.Out.Header.Set(xLocalModelError, "unknown job-id")
			pr.Out.Header.Set(xLocalModelErrorJobID, jobID)
			setDummyTarget(pr)
			return
		}

		pr.Out.URL.Scheme = target.Scheme
		pr.Out.URL.Host = target.Host
		pr.Out.Host = target.Host
		pr.Out.URL.Path = remaining
		pr.Out.URL.RawPath = ""
		pr.Out.RequestURI = ""

		pr.Out = pr.Out.WithContext(context.WithValue(pr.Out.Context(), contextKeyJobHTTPClient{}, jobClient))

		reqLog.Info("Proxying local model request",
			"job_id", jobID, "method", pr.Out.Method, "url", pr.Out.URL.String())
	}

	rp.ModifyResponse = proxyModifyResponse(logger, "Response from local model proxy")

	rp.ErrorHandler = proxyErrorHandler(logger, "Error proxying local model request")

	return rp
}

// setDummyTarget fills required URL fields on pr.Out so the proxy framework
// does not panic when the Rewrite function bails out early on error.
func setDummyTarget(pr *httputil.ProxyRequest) {
	pr.Out.URL.Scheme = "http"
	pr.Out.URL.Host = "localhost"
	pr.Out.Host = "localhost"
	pr.Out.RequestURI = ""
}

// localModelRoundTripper intercepts requests marked with xLocalModelError and
// returns the appropriate HTTP error without forwarding to an upstream.
type localModelRoundTripper struct {
	inner  http.RoundTripper
	logger *slog.Logger
}

func (t *localModelRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	errMsg := req.Header.Get(xLocalModelError)
	if errMsg == "" {
		if client := jobClientFromContext(req.Context()); client != nil {
			return client.Do(req)
		}
		return t.inner.RoundTrip(req)
	}
	req.Header.Del(xLocalModelError)
	jobID := req.Header.Get(xLocalModelErrorJobID)
	req.Header.Del(xLocalModelErrorJobID)

	statusCode := http.StatusNotFound
	respBody := map[string]string{"error": "unknown job-id", "job_id": jobID}
	if jobID == "" {
		statusCode = http.StatusBadRequest
		respBody = map[string]string{"error": errMsg}
	}
	t.logger.Error("Local model proxy error",
		"request_id", getOrCreateRequestID(req), "status", statusCode, "job_id", jobID)

	body, _ := json.Marshal(respBody)
	resp := newSyntheticResponse(req, statusCode, bytes.NewReader(append(body, '\n')))
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}
