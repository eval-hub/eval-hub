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

// OCIAuthConfigPathDefault is the default path for the registry auth config file. Must match the OCI secret
// mount path on adapter and sidecar: internal/runtimes/k8s/job_builders.go ociCredentialsMountPath.
const OCIAuthConfigPathDefault = "/etc/evalhub/.docker/config.json"

// JobSpecPathDefault is the default path for the job spec file. Must match the job-spec mount on the sidecar:
// internal/runtimes/k8s/job_builders.go jobSpecMountPath + subPath jobSpecFileName.
const JobSpecPathDefault = "/meta/job.json"

func newOciProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, *proxy.OCITokenProducer, string, error) {
	if config == nil || config.Sidecar == nil {
		return nil, nil, "", nil
	}
	jobSpecPath := os.Getenv("JOB_SPEC_PATH")
	if jobSpecPath == "" {
		jobSpecPath = JobSpecPathDefault
	}
	host, repository, err := proxy.GetOCICoordinatesFromJobSpec(jobSpecPath)
	if err != nil {
		logger.Debug("OCI proxy disabled: could not read job spec for OCI coordinates", "path", jobSpecPath, "error", err)
		return nil, nil, "", nil
	}
	if host == "" {
		logger.Debug("OCI proxy disabled: job spec has no exports.oci with registry host", "path", jobSpecPath)
		return nil, nil, "", nil
	}
	// OCI reverse proxy is enabled from job spec (exports.oci + coordinates), not from eval-hub
	// service YAML sidecar.oci. sidecar_config.json "oci" is optional: TLS/timeout overrides only
	// (see NewOCIHTTPClient).
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
	logger.Info("OCI registry proxy enabled",
		"registry_host", host,
		"oci_repository", repository,
		"job_spec", jobSpecPath,
	)
	return rp, tokenProducer, repository, nil
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
