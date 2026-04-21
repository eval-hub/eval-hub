package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const huggingfaceProxyPathPrefix = "/huggingface"

// IsHuggingFaceProxyPath reports whether path should be handled by the Hugging Face reverse proxy
// (/huggingface or /huggingface/...). It does not match paths like /huggingfaceExtra.
func IsHuggingFaceProxyPath(path string) bool {
	if !strings.HasPrefix(path, huggingfaceProxyPathPrefix) {
		return false
	}
	if len(path) == len(huggingfaceProxyPathPrefix) {
		return true
	}
	return path[len(huggingfaceProxyPathPrefix)] == '/'
}

// stripHuggingFaceProxyPathPrefix removes the /huggingface prefix so the request path matches the upstream Hub path.
func stripHuggingFaceProxyPathPrefix(p string) string {
	if p == huggingfaceProxyPathPrefix {
		return "/"
	}
	if strings.HasPrefix(p, huggingfaceProxyPathPrefix+"/") {
		return strings.TrimPrefix(p, huggingfaceProxyPathPrefix)
	}
	return p
}

// NewHuggingFaceReverseProxy returns a reverse proxy to the Hugging Face origin (scheme + host only).
// It strips the /huggingface prefix from the request path, then forwards. Authorization is set only when
// sidecar.huggingface.token_path is non-empty (HF token file, e.g. from model auth secret key "hf-token").
func NewHuggingFaceReverseProxy(target *url.URL, client *http.Client, logger *slog.Logger) *httputil.ReverseProxy {
	transport := &roundTripperFromClient{client: client}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

	proxy.Director = func(req *http.Request) {
		req.URL.Path = stripHuggingFaceProxyPathPrefix(req.URL.Path)
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
		logger.Info("Hugging Face proxy request", "method", req.Method, "url", req.URL.String(), "headers", headersForLog(req.Header))
	}

	hubHost := target.Hostname()
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Request != nil {
			logger.Info("Response from Hugging Face proxy", "method", resp.Request.Method, "url", resp.Request.URL.String(), "status", resp.StatusCode)
		}
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return nil
		}
		loc := resp.Header.Get("Location")
		if loc == "" {
			return nil
		}
		newLoc := rewriteHuggingFaceRedirectLocation(loc, hubHost)
		if newLoc != loc {
			resp.Header.Set("Location", newLoc)
			logger.Info("Hugging Face proxy rewritten Location", "from", loc, "to", newLoc)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		logger.Error("Error proxying Hugging Face request", "method", req.Method, "url", req.URL.String(), "error", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	return proxy
}
