package common

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const DefaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

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

// ProxyRequest forwards r to targetBaseURL (path and query from r), sets Content-Type and optional auth,
// performs the request with client, and copies the response to w.
func ProxyRequest(logger *slog.Logger, w http.ResponseWriter, r *http.Request, client *http.Client, targetBaseURL string, authToken string) {
	targetURL := strings.TrimSuffix(targetBaseURL, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.ContentLength = int64(len(body))
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if tenant := r.Header.Get("X-Tenant"); tenant != "" {
		req.Header.Set("X-Tenant", tenant)
	}
	SetAuthHeader(req, authToken)

	logger.Info("Proxying request", "method", req.Method, "url", req.URL.String(), "headers", headersForLog(req.Header))
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Error proxying request", "method", req.Method, "url", req.URL.String(), "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	logger.Info("Response from proxy", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode)
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
