package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
	"github.com/eval-hub/eval-hub/internal/runtimeenv"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// Handlers holds service state for HTTP handlers.
// Reverse proxies are created once at startup and reused for all requests.
type Handlers struct {
	logger           *slog.Logger
	serviceConfig    *config.Config
	evalHubProxy     *httputil.ReverseProxy
	mlflowProxy      *httputil.ReverseProxy
	ociProxy         *httputil.ReverseProxy
	ociTokenProducer *proxy.OCITokenProducer // created once at startup for OCI auth
	ociRepository    string                  // from job spec; used to route requests to /registry/{ociRepository}
	modelProxy       *httputil.ReverseProxy  // model credential-injection proxy; nil when not configured

	// gitSHA is the commit SHA from .git-metadata (isGitJob only). Best-effort load in New();
	// if still empty, tryLoadGitSHA retries on each events POST until success, then the value
	// is read-only. All reads and writes go through gitSHAMu so concurrent events POSTs
	// observe a consistent value.
	gitSHAMu        sync.Mutex
	gitSHA          string
	gitMetadataPath string // empty means runtimeenv.GitMetadataFile; tests may override
}

// New creates handlers and builds reverse proxies for eval-hub, MLflow, OCI, and optionally model.
func New(config *config.Config, logger *slog.Logger) (*Handlers, error) {
	evalHubProxy, err := newEvalhubProxy(config, logger)
	if err != nil {
		return nil, err
	}

	mlflowProxy, err := newMlflowProxy(config, logger)
	if err != nil {
		return nil, err
	}

	ociProxy, ociTokenProducer, ociRepository, err := newOciProxy(config, logger)
	if err != nil {
		return nil, err
	}

	modelProxy, err := newModelProxy(config, logger)
	if err != nil {
		return nil, err
	}

	h := &Handlers{
		logger:           logger,
		serviceConfig:    config,
		evalHubProxy:     evalHubProxy,
		mlflowProxy:      mlflowProxy,
		ociProxy:         ociProxy,
		ociTokenProducer: ociTokenProducer,
		ociRepository:    ociRepository,
		modelProxy:       modelProxy,
	}

	// Best-effort read of .git-metadata at startup for git-source jobs (KEP-753: init has
	// exited before this container starts). If the file is missing or empty, tryLoadGitSHA
	// retries on subsequent events POSTs until a SHA is loaded.
	if h.isGitJob() {
		h.tryLoadGitSHA()
		if sha := h.currentGitSHA(); sha == "" {
			logger.Warn("git metadata not available at startup; will retry on status events",
				"path", h.gitMetadataFile())
		} else {
			logger.Info("git commit SHA loaded at startup", "sha", sha)
		}
	}

	return h, nil
}

// HandleHealth responds OK for liveness.
func (h *Handlers) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// HandleProxyCall routes the request to the correct reverse proxy (Eval Hub API, MLflow, or OCI).
// For git-source jobs, it injects the resolved commit SHA into every BenchmarkStatusEvent POST.
// The SHA is loaded from .git-metadata at startup when possible, and retried while still empty.
func (h *Handlers) HandleProxyCall(w http.ResponseWriter, r *http.Request) {
	if h.isGitJob() && r.Method == http.MethodPost && isEventsPath(r.RequestURI) {
		r = h.maybeInjectResolvedSHA(r)
	}

	proxyHandler, tokenParams, err := h.parseProxyCall(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := proxy.ContextWithAuthInput(r.Context(), *tokenParams)
	ctx = proxy.ContextWithOriginalRequest(ctx, r)
	r = r.WithContext(ctx)
	proxyHandler.ServeHTTP(w, r)
}

// isGitJob reports whether the sidecar is running for a git-source job.
func (h *Handlers) isGitJob() bool {
	return h.serviceConfig != nil &&
		h.serviceConfig.Sidecar != nil &&
		h.serviceConfig.Sidecar.InitContainer != nil &&
		h.serviceConfig.Sidecar.InitContainer.IsGitJob
}

var eventsPathRe = regexp.MustCompile(`^/api/v1/evaluations/jobs/[^/]+/events$`)

// isEventsPath returns true when the path is exactly a job events endpoint.
func isEventsPath(uri string) bool {
	return eventsPathRe.MatchString(requestPathForRouting(uri))
}

// maybeInjectResolvedSHA ensures a commit SHA is loaded (retrying if still empty), then injects
// it into JobMeta.ResolvedSHA on every BenchmarkStatusEvent body. If the SHA is still unavailable,
// any client-supplied job_meta.resolved_sha is stripped so a failed metadata read cannot become a
// forge path. The server skips persist if the SHA is already stored. Non-BenchmarkStatusEvent
// bodies are unchanged.
func (h *Handlers) maybeInjectResolvedSHA(r *http.Request) *http.Request {
	h.tryLoadGitSHA()

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read request body for resolved SHA injection", "error", err)
		r.Body = io.NopCloser(bytes.NewReader(rawBody))
		return r
	}
	_ = r.Body.Close()

	var ev api.StatusEvent
	if json.Unmarshal(rawBody, &ev) != nil || ev.BenchmarkStatusEvent == nil {
		// Not a BenchmarkStatusEvent — restore body and skip injection.
		r.Body = io.NopCloser(bytes.NewReader(rawBody))
		return r
	}

	sha := h.currentGitSHA()
	clientSHA := ""
	if ev.BenchmarkStatusEvent.JobMeta != nil {
		clientSHA = ev.BenchmarkStatusEvent.JobMeta.ResolvedSHA
	}
	if sha != "" {
		ev.BenchmarkStatusEvent.JobMeta = &api.JobMeta{ResolvedSHA: sha}
	} else if clientSHA == "" {
		// Nothing to inject or strip.
		r.Body = io.NopCloser(bytes.NewReader(rawBody))
		return r
	} else {
		// Metadata still missing — drop any client-supplied SHA.
		ev.BenchmarkStatusEvent.JobMeta = nil
	}

	injected, err := json.Marshal(ev)
	if err != nil {
		h.logger.Error("failed to marshal status event with resolved SHA", "error", err)
		r.Body = io.NopCloser(bytes.NewReader(rawBody))
		return r
	}

	if sha != "" {
		h.logger.Info("injected resolved SHA into status event", "sha", sha)
	}
	newReq := r.Clone(r.Context())
	newReq.Body = io.NopCloser(bytes.NewReader(injected))
	newReq.ContentLength = int64(len(injected))
	return newReq
}

// currentGitSHA returns the loaded commit SHA under gitSHAMu.
func (h *Handlers) currentGitSHA() string {
	h.gitSHAMu.Lock()
	defer h.gitSHAMu.Unlock()
	return h.gitSHA
}

// tryLoadGitSHA reads .git-metadata when gitSHA is still empty. After a successful load the
// SHA is never re-read. Safe under concurrent events POSTs.
func (h *Handlers) tryLoadGitSHA() {
	h.gitSHAMu.Lock()
	defer h.gitSHAMu.Unlock()
	if h.gitSHA != "" {
		return
	}
	path := h.gitMetadataFile()
	sha, err := readGitMetadata(path)
	if err != nil {
		h.logger.Debug("git metadata not ready yet", "path", path, "error", err)
		return
	}
	if sha == "" {
		h.logger.Debug("git metadata file present but empty", "path", path)
		return
	}
	h.gitSHA = sha
	h.logger.Info("git commit SHA loaded", "sha", sha, "path", path)
}

func (h *Handlers) gitMetadataFile() string {
	if h.gitMetadataPath != "" {
		return h.gitMetadataPath
	}
	return runtimeenv.GitMetadataFile
}

// readGitMetadata reads the commit SHA written by the init container.
// Reads are confined to the metadata directory via os.Root.
func readGitMetadata(path string) (string, error) {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	base := filepath.Base(clean)
	if !filepath.IsLocal(base) {
		return "", fmt.Errorf("invalid git metadata path: %q", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(base)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// requestPathForRouting returns the URL path only (no query or fragment) for proxy routing.
func requestPathForRouting(uri string) string {
	// Strip query/fragment first so invalid characters there cannot poison the path
	// when url.Parse fails (e.g. control bytes in the query).
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return u.EscapedPath()
}

func (h *Handlers) parseProxyCall(r *http.Request) (*httputil.ReverseProxy, *proxy.AuthTokenInput, error) {
	switch {
	case strings.HasPrefix(r.RequestURI, "/api/v1/evaluations/"):
		ehClientConfig := h.serviceConfig.Sidecar.EvalHub
		if ehClientConfig != nil {
			return h.evalHubProxy, &proxy.AuthTokenInput{
				TargetEndpoint:    "eval-hub",
				AuthTokenPath:     ServiceAccountAuthFileDefault,
				AuthToken:         ehClientConfig.Token,
				TokenCacheTimeout: ehClientConfig.TokenCacheTimeout,
			}, nil
		}
		return nil, nil, fmt.Errorf("eval-hub proxy is not configured")

	case isMLflowProxyPath(requestPathForRouting(r.RequestURI)):
		if h.serviceConfig.MLFlow != nil && strings.TrimSpace(h.serviceConfig.MLFlow.TrackingURI) != "" && h.mlflowProxy != nil {
			tokenPath := MLFlowAuthFileDefault
			if h.serviceConfig.Sidecar != nil && h.serviceConfig.Sidecar.MLFlow != nil {
				if p := strings.TrimSpace(h.serviceConfig.Sidecar.MLFlow.TokenPath); p != "" {
					tokenPath = p
				}
			}
			return h.mlflowProxy, &proxy.AuthTokenInput{
				TargetEndpoint: "mlflow",
				AuthTokenPath:  tokenPath,
			}, nil
		}
		return nil, nil, fmt.Errorf("mlflow proxy is not configured")

	case h.ociRouteMatch(r.RequestURI):
		if h.ociProxy != nil {
			// Reuse the TokenProducer created at startup; token cache and refresh in resolveOCIAuthToken.
			return h.ociProxy, &proxy.AuthTokenInput{
				TargetEndpoint:   "oci",
				OCITokenProducer: h.ociTokenProducer,
				OCIRepository:    h.ociRepository,
			}, nil
		}
		return nil, nil, fmt.Errorf("oci proxy is not configured")
	default:
		// Model credential-injection proxy is the catch-all: when configured, any request
		// not matched by the specific prefixes above is forwarded to the model target URL.
		// The model proxy's Rewrite function performs ref-token substitution and dynamic URL
		// routing; it does not use AuthTokenInput, so an empty value is returned.
		if h.modelProxy != nil {
			return h.modelProxy, &proxy.AuthTokenInput{}, nil
		}
		return nil, nil, fmt.Errorf("unknown proxy call: %s", r.RequestURI)
	}
}
