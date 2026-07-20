package handlers

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	evalHubInstanceNameEnv   = "EVALHUB_INSTANCE_NAME"
	defaultEvalHubPort       = "8443"
	defaultSidecarListenPort = 8080
	inClusterNamespaceFile   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// rewriteSidecarURLsInBenchmarkStatus replaces sidecar localhost URLs in status
// messages with real upstream hosts, using the same path→target rules as the sidecar.
func (h *Handlers) rewriteSidecarURLsInBenchmarkStatus(event *api.BenchmarkStatusEvent, job *api.EvaluationJobResource, logger *slog.Logger) {
	if event == nil {
		return
	}
	event.RewriteSidecarURLsInMessages(h.sidecarBaseURL(), h.sidecarURLTargets(job), logger)
}

func (h *Handlers) sidecarBaseURL() string {
	port := defaultSidecarListenPort
	baseURL := ""
	if h.serviceConfig != nil && h.serviceConfig.Sidecar != nil {
		if h.serviceConfig.Sidecar.Port != 0 {
			port = h.serviceConfig.Sidecar.Port
		}
		baseURL = strings.TrimSpace(h.serviceConfig.Sidecar.BaseURL)
	}
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", port)
	}
	return baseURL
}

func (h *Handlers) sidecarURLTargets(job *api.EvaluationJobResource) api.SidecarURLTargets {
	targets := api.SidecarURLTargets{
		EvalHub: evalHubServiceURL(tenantFromJob(job)),
		MLFlow:  mlflowTrackingURIFromConfig(h.serviceConfig),
	}
	if job == nil {
		return targets
	}
	targets.Model = strings.TrimSpace(job.Model.URL)
	if job.Exports != nil && job.Exports.OCI != nil {
		coords := job.Exports.OCI.Coordinates
		targets.OCIRepository = strings.TrimSpace(coords.OCIRepository)
		targets.OCI = normalizeRegistryURL(coords.OCIHost)
	}
	return targets
}

func tenantFromJob(job *api.EvaluationJobResource) string {
	if job == nil {
		return ""
	}
	return string(job.Resource.Tenant)
}

func mlflowTrackingURIFromConfig(cfg *config.Config) string {
	if cfg == nil || cfg.MLFlow == nil {
		return ""
	}
	return strings.TrimSpace(cfg.MLFlow.TrackingURI)
}

// evalHubServiceURL builds the in-cluster eval-hub URL the same way job pods do
// (https://<EVALHUB_INSTANCE_NAME>.<ns>.svc.cluster.local:8443).
func evalHubServiceURL(tenantNamespace string) string {
	instanceName := strings.TrimSpace(os.Getenv(evalHubInstanceNameEnv))
	if instanceName == "" {
		return ""
	}
	saNamespace := readInClusterNamespace()
	if saNamespace == "" {
		saNamespace = strings.TrimSpace(tenantNamespace)
	}
	if saNamespace == "" {
		return ""
	}
	return fmt.Sprintf("https://%s.%s.svc.cluster.local:%s", instanceName, saNamespace, defaultEvalHubPort)
}

func readInClusterNamespace() string {
	content, err := os.ReadFile(inClusterNamespaceFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func normalizeRegistryURL(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		return "https://" + host
	}
	return host
}
