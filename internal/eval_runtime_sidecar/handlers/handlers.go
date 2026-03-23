package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

const ServiceAccountTokenPathDefault = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const MLFlowTokenPathDefault = "/var/run/secrets/mlflow/token"

// OCIAuthConfigPathDefault is the default path for the registry auth config file. Must match the OCI secret
// mount path on adapter and sidecar: internal/runtimes/k8s/job_builders.go ociCredentialsMountPath.
const OCIAuthConfigPathDefault = "/etc/evalhub/.docker/config.json"

// JobSpecPathDefault is the default path for the job spec file. Must match the job-spec mount on the sidecar:
// internal/runtimes/k8s/job_builders.go jobSpecMountPath + subPath jobSpecFileName.
const JobSpecPathDefault = "/meta/job.json"

// mlflowAPIPathPrefix is the path prefix routed to the MLflow tracking proxy (any suffix allowed).
const mlflowAPIPathPrefix = "/api/2.0/mlflow"

func isMLflowProxyPath(path string) bool {
	return strings.HasPrefix(path, mlflowAPIPathPrefix)
}

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
}

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

	return &Handlers{
		logger:           logger,
		serviceConfig:    config,
		evalHubProxy:     evalHubProxy,
		mlflowProxy:      mlflowProxy,
		ociProxy:         ociProxy,
		ociTokenProducer: ociTokenProducer,
		ociRepository:    ociRepository,
	}, nil
}

func newMlflowProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	mlflowTrackingURI := ""
	if config.MLFlow != nil {
		mlflowTrackingURI = strings.TrimSpace(config.MLFlow.TrackingURI)
	}
	if mlflowTrackingURI == "" {
		logger.Warn("mlflow.tracking_uri is not set in sidecar config")
		return nil, nil
	}
	mlflowHTTPClient, err := proxy.NewMLFlowHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create mlflow HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	mlflowTarget, err := url.Parse(strings.TrimSuffix(mlflowTrackingURI, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid mlflow.tracking_uri: %w", err)
	}

	mlflowProxy := proxy.NewReverseProxy(mlflowTarget, mlflowHTTPClient, logger, nil)
	return mlflowProxy, nil
}

func newEvalhubProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	evalHubHTTPClient, err := proxy.NewEvalHubHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create eval-hub HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	evalHubBaseURL := ""
	if config.Sidecar != nil && config.Sidecar.EvalHub != nil {
		evalHubBaseURL = strings.TrimSpace(config.Sidecar.EvalHub.BaseURL)
	}
	if evalHubBaseURL == "" {
		return nil, fmt.Errorf("eval_hub.base_url is not set in sidecar config")
	}
	evalHubTarget, err := url.Parse(strings.TrimSuffix(evalHubBaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid EVALHUB_URL: %w", err)
	}
	evalHubProxy := proxy.NewReverseProxy(evalHubTarget, evalHubHTTPClient, logger, nil)
	return evalHubProxy, nil
}

func newOciProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, *proxy.OCITokenProducer, string, error) {
	if config == nil || config.Sidecar == nil || config.Sidecar.OCI == nil {
		return nil, nil, "", nil
	}
	jobSpecPath := os.Getenv("JOB_SPEC_PATH")
	if jobSpecPath == "" {
		jobSpecPath = JobSpecPathDefault
	}
	host, repository, err := proxy.GetOCICoordinatesFromJobSpec(jobSpecPath)
	if err != nil {
		logger.Debug("OCI disabled: could not read job spec for OCI coordinates", "path", jobSpecPath, "error", err)
		return nil, nil, "", nil
	}
	if host == "" {
		logger.Debug("OCI disabled: job spec has no OCI exports or oci_host", "path", jobSpecPath)
		return nil, nil, "", nil
	}
	ociHTTPClient, err := proxy.NewOCIHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create OCI HTTP client", "error", err)
		return nil, nil, "", fmt.Errorf("failed to create OCI HTTP client: %w", err)
	}
	if ociHTTPClient == nil {
		return nil, nil, "", fmt.Errorf("OCI HTTP client is required for OCI proxy")
	}
	ociSecretMountPath := os.Getenv("OCI_AUTH_CONFIG_PATH")
	if ociSecretMountPath == "" {
		ociSecretMountPath = OCIAuthConfigPathDefault
	}
	tokenProducer, err := proxy.LoadTokenProducerFromOCISecret(ociSecretMountPath, host, repository, ociHTTPClient)
	if err != nil {
		logger.Error("failed to create OCI token producer from OCI secret", "path", ociSecretMountPath, "error", err)
		return nil, nil, "", fmt.Errorf("OCI token producer: %w", err)
	}
	ociTarget, err := url.Parse(strings.TrimSuffix(host, "/"))
	if err != nil {
		return nil, nil, "", fmt.Errorf("invalid OCI registry host from job spec %q: %w", host, err)
	}
	rp := proxy.NewReverseProxy(ociTarget, ociHTTPClient, logger, func(resp *http.Response) error {
		proxy.ModifyOCIRegistryResponse(resp, logger, tokenProducer)
		return nil
	})
	return rp, tokenProducer, repository, nil
}

func (h *Handlers) HandleProxyCall(w http.ResponseWriter, r *http.Request) {
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

// requestPathForRouting returns the URL path only (no query or fragment) for proxy routing.
func requestPathForRouting(uri string) string {
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		uri = uri[:i]
	}
	if i := strings.IndexByte(uri, '#'); i >= 0 {
		uri = uri[:i]
	}
	return uri
}

func splitPathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func pathSegmentsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ociRouteMatch returns true if the request should be routed to the OCI proxy.
// Matching uses only the path (query and fragment are ignored). The job-spec repository
// must appear as a full consecutive sequence of path segments, either at the start of
// the path or immediately after a "v2" segment (OCI distribution API), e.g. /v2/org/repo/...
// matches repo "org/repo" but /v2/ac/org/repo/... does not.
func (h *Handlers) ociRouteMatch(uri string) bool {
	if h.ociRepository == "" {
		return false
	}
	path := requestPathForRouting(uri)
	repoParts := splitPathSegments(h.ociRepository)
	if len(repoParts) == 0 {
		return false
	}
	pathParts := splitPathSegments(path)
	if len(pathParts) < len(repoParts) {
		return false
	}
	n := len(repoParts)
	for i := 0; i+n <= len(pathParts); i++ {
		if !pathSegmentsEqual(pathParts[i:i+n], repoParts) {
			continue
		}
		if i == 0 || pathParts[i-1] == "v2" {
			return true
		}
	}
	return false
}

func (h *Handlers) parseProxyCall(r *http.Request) (*httputil.ReverseProxy, *proxy.AuthTokenInput, error) {
	switch {
	case strings.HasPrefix(r.RequestURI, "/api/v1/evaluations/"):
		ehClientConfig := h.serviceConfig.Sidecar.EvalHub
		if ehClientConfig != nil {
			return h.evalHubProxy, &proxy.AuthTokenInput{
				TargetEndpoint:    "eval-hub",
				AuthTokenPath:     ServiceAccountTokenPathDefault,
				AuthToken:         ehClientConfig.Token,
				TokenCacheTimeout: ehClientConfig.TokenCacheTimeout,
			}, nil
		}
		return nil, nil, fmt.Errorf("eval-hub proxy is not configured")

	case isMLflowProxyPath(requestPathForRouting(r.RequestURI)):
		if h.serviceConfig.MLFlow != nil && strings.TrimSpace(h.serviceConfig.MLFlow.TrackingURI) != "" && h.mlflowProxy != nil {
			tokenPath := MLFlowTokenPathDefault
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
		ociConfig := h.serviceConfig.Sidecar.OCI
		if ociConfig != nil && h.ociProxy != nil {
			// Reuse the TokenProducer created at startup; token cache and refresh in resolveOCIAuthToken.
			return h.ociProxy, &proxy.AuthTokenInput{
				TargetEndpoint:   "oci",
				OCITokenProducer: h.ociTokenProducer,
				OCIRepository:    h.ociRepository,
			}, nil
		}
		return nil, nil, fmt.Errorf("oci proxy is not configured")
	default:
		return nil, nil, fmt.Errorf("unknown proxy call: %s", r.RequestURI)
	}
}
