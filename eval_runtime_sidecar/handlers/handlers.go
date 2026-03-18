package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxy"
	"github.com/eval-hub/eval-hub/internal/config"
)

const ServiceAccountTokenPathDefault = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const MLFlowTokenPathDefault = "/var/run/secrets/mlflow/token"

// OCIAuthConfigPathDefault is the default path for the registry auth config file. Must match the OCI secret
// mount path on adapter and sidecar: internal/runtimes/k8s/job_builders.go ociCredentialsMountPath.
const OCIAuthConfigPathDefault = "/etc/evalhub/.docker/config.json"

// JobSpecPathDefault is the default path for the job spec file. Must match the job-spec mount on the sidecar:
// internal/runtimes/k8s/job_builders.go jobSpecMountPath + subPath jobSpecFileName.
const JobSpecPathDefault = "/meta/job.json"

// Handlers holds service state for HTTP handlers.
// Reverse proxies are created once at startup and reused for all requests.
type Handlers struct {
	logger           *slog.Logger
	serviceConfig    *config.Config
	evalHubProxy     *httputil.ReverseProxy
	mlflowProxy      *httputil.ReverseProxy
	ociProxy         *httputil.ReverseProxy
	ociTokenProducer *proxy.TokenProducer // created once at startup for OCI auth
	ociRepository    string               // from job spec; used to route requests to /registry/{ociRepository}
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
	mlflowHTTPClient, err := proxy.NewMLFlowHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create mlflow HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	mlflowTrackingURI := os.Getenv("MLFLOW_TRACKING_URI")
	if mlflowTrackingURI == "" {
		logger.Warn("MLFLOW_TRACKING_URI is not set")
	}
	mlflowTarget, err := url.Parse(strings.TrimSuffix(mlflowTrackingURI, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid MLFLOW_TRACKING_URI: %w", err)
	}

	mlflowProxy := proxy.NewReverseProxy(mlflowTarget, mlflowHTTPClient, logger)
	return mlflowProxy, nil
}

func newEvalhubProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	evalHubHTTPClient, err := proxy.NewEvalHubHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create eval-hub HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	evalHubBaseURL := os.Getenv("EVALHUB_URL")
	if evalHubBaseURL == "" {
		return nil, fmt.Errorf("EVALHUB_URL environment is not set")
	}
	evalHubTarget, err := url.Parse(strings.TrimSuffix(evalHubBaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid EVALHUB_URL: %w", err)
	}
	evalHubProxy := proxy.NewReverseProxy(evalHubTarget, evalHubHTTPClient, logger)
	return evalHubProxy, nil
}

func newOciProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, *proxy.TokenProducer, string, error) {
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
	tp, err := proxy.LoadTokenProducerFromOCISecret(ociSecretMountPath, host, repository, ociHTTPClient)
	if err != nil {
		logger.Error("failed to create OCI token producer from OCI secret", "path", ociSecretMountPath, "error", err)
		return nil, nil, "", fmt.Errorf("OCI token producer: %w", err)
	}
	ociTarget, err := url.Parse(strings.TrimSuffix(host, "/"))
	if err != nil {
		return nil, nil, "", fmt.Errorf("invalid OCI registry host from job spec %q: %w", host, err)
	}
	return proxy.NewReverseProxy(ociTarget, ociHTTPClient, logger), tp, repository, nil
}

func (h *Handlers) HandleProxyCall(w http.ResponseWriter, r *http.Request) {
	proxyHandler, tokenParams, err := h.parseProxyCall(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = r.WithContext(proxy.ContextWithAuthInput(r.Context(), *tokenParams))
	proxyHandler.ServeHTTP(w, r)
}

// ociRouteMatch returns true if the request URI should be routed to the OCI proxy.
// The URI need not have a /registry/ prefix: if it contains the repository name from
// /meta/job.json as a path segment (e.g. "org/repo"), the request is routed to OCI.
// Left boundary: seg must be at path start, after '/', or immediately after the OCI
// "/v2" prefix (so /v2/org/repo matches but /v2/ac/org/repo does not for repo "org/repo").
// Right boundary: end of URI or next char is '/'.
func (h *Handlers) ociRouteMatch(uri string) bool {
	if h.ociRepository == "" {
		return false
	}
	seg := "/" + h.ociRepository
	for searchStart := 0; ; {
		idx := strings.Index(uri[searchStart:], seg)
		if idx < 0 {
			return false
		}
		idx += searchStart
		prefix := uri[:idx]
		leftOK := idx == 0 || uri[idx-1] == '/' || strings.HasSuffix(prefix, "/v2")
		after := idx + len(seg)
		rightOK := after == len(uri) || uri[after] == '/'
		if leftOK && rightOK {
			return true
		}
		searchStart = idx + 1
	}
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

	case strings.Contains(r.RequestURI, "/mlflow/"):
		mlflowClientConfig := h.serviceConfig.MLFlow
		if mlflowClientConfig != nil && h.mlflowProxy != nil {
			return h.mlflowProxy, &proxy.AuthTokenInput{
				TargetEndpoint: "mlflow",
				AuthTokenPath:  MLFlowTokenPathDefault,
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
