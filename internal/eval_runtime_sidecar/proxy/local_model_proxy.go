package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
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

type localProxyState struct {
	target    *url.URL
	remaining string
	jobID     string
	client    *http.Client
}

type contextKeyLocalProxyState struct{}

func localProxyStateFromContext(ctx context.Context) *localProxyState {
	v, _ := ctx.Value(contextKeyLocalProxyState{}).(*localProxyState)
	return v
}

// NewLocalModelReverseProxy creates an http.Handler for local mode that routes
// model requests per-job using the TTL cache. Each request to /model/<job-id>/...
// is forwarded to the upstream model URL from the job's sidecar-job-info.json,
// with the /model/<job-id> prefix stripped from the path.
// Each job gets its own HTTP client with TLS configured from the job's secret cache.
//
// Path and job-ID validation happen in the outer handler, which has access to the
// ResponseWriter and can return errors directly. Only valid requests reach the
// reverse proxy.
func NewLocalModelReverseProxy(cache *JobInfoCache, logger *slog.Logger) http.Handler {
	rp := &httputil.ReverseProxy{
		Transport: &localModelRoundTripper{
			inner: http.DefaultTransport,
		},
	}

	rp.Rewrite = func(pr *httputil.ProxyRequest) {
		reqID := getOrCreateRequestID(pr.In)
		pr.Out.Header.Set(globalTransactionIDHeader, reqID)

		state := localProxyStateFromContext(pr.In.Context())

		pr.Out.URL.Scheme = state.target.Scheme
		pr.Out.URL.Host = state.target.Host
		pr.Out.Host = state.target.Host
		pr.Out.URL.Path = state.remaining
		pr.Out.URL.RawPath = ""
		pr.Out.RequestURI = ""

		logger.With("request_id", reqID).Info("Proxying local model request",
			"job_id", state.jobID, "method", pr.Out.Method, "url", pr.Out.URL.String())
	}

	rp.ModifyResponse = proxyModifyResponse(logger, "Response from local model proxy")
	rp.ErrorHandler = proxyErrorHandler(logger, "Error proxying local model request")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := getOrCreateRequestID(r)
		reqLog := logger.With("request_id", reqID)

		jobID, remaining, ok := ParseLocalModelPath(r.URL.Path)
		if !ok {
			reqLog.Error("Invalid model path", "path", r.URL.Path, "method", r.Method)
			writeJSONError(w, reqID, http.StatusBadRequest, map[string]string{
				"error": "invalid model path format, expected /model/<job-id>/<path>",
			})
			return
		}

		target, jobClient, err := cache.Get(jobID)
		if err != nil {
			reqLog.Error("Job info lookup failed", "job_id", jobID, "error", err)
			if errors.Is(err, ErrInvalidAuthSecretPath) {
				writeJSONError(w, reqID, http.StatusBadRequest, map[string]string{
					"error":  "invalid auth_secret_mount_path: missing file:/// prefix",
					"job_id": jobID,
				})
			} else {
				writeJSONError(w, reqID, http.StatusNotFound, map[string]string{
					"error":  "unknown job-id",
					"job_id": jobID,
				})
			}
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyLocalProxyState{}, &localProxyState{
			target:    target,
			remaining: remaining,
			jobID:     jobID,
			client:    jobClient,
		})
		rp.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSONError(w http.ResponseWriter, reqID string, statusCode int, body map[string]string) {
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(globalTransactionIDHeader, reqID)
	w.WriteHeader(statusCode)
	_, _ = w.Write(append(data, '\n'))
}

// localModelRoundTripper dispatches requests to per-job HTTP clients (for
// per-job TLS), falling back to the default transport.
type localModelRoundTripper struct {
	inner http.RoundTripper
}

func (t *localModelRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if state := localProxyStateFromContext(req.Context()); state != nil && state.client != nil {
		return state.client.Do(req)
	}
	return t.inner.RoundTrip(req)
}
