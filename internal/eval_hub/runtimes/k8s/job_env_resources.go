package k8s

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func buildContainerCommand(entrypoint []string) []string {
	if len(entrypoint) == 0 {
		return nil
	}
	var command []string
	for _, part := range entrypoint {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		command = append(command, item)
	}
	if len(command) == 0 {
		return nil
	}
	return command
}

func defaultSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(defaultAllowPrivilegeEscalation),
		RunAsNonRoot:             boolPtr(true),
		// RunAsUser and RunAsGroup omitted to let OpenShift assign from allowed range
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				capabilityDropAll,
			},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

// buildEnvVars builds environment variables for the adapter container.
func buildEnvVars(jc *jobConfig, serviceConfig *config.Config) []corev1.EnvVar {
	var env []corev1.EnvVar
	seen := map[string]bool{}

	env = append(env, corev1.EnvVar{
		Name:  envEvalHubModeName,
		Value: "k8s",
	})
	seen[envEvalHubModeName] = true

	// When sidecar is at play, mlflow calls are proxied through the sidecar.
	mlflowTrackingURI := jc.sidecarBaseURL
	// Add MLFlow environment variables if tracking is configured
	if jc.mlflowTrackingURI != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowTrackingURIName,
			Value: mlflowTrackingURI,
		})
		seen[envMLFlowTrackingURIName] = true

	}
	if jc.mlflowWorkspace != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowWorkspaceName,
			Value: jc.mlflowWorkspace,
		})
		seen[envMLFlowWorkspaceName] = true
	}

	// Add OCI auth config path when credentials secret is configured
	if jc.ociCredentialsSecret != "" {
		env = append(env, corev1.EnvVar{
			Name:  envOCIAuthConfigPathName,
			Value: ociAuthMountPath,
		})
		seen[envOCIAuthConfigPathName] = true
	}

	if len(jc.postProcessorSecrets) > 0 {
		env = append(env, corev1.EnvVar{Name: envPostProcessorSecretRoot, Value: postProcessorSecretMountPath})
		seen[envPostProcessorSecretRoot] = true
	}

	if len(jc.postProcessorPVCs) > 0 {
		// Map claim roots; the adapter resolves each reference's sub_path.
		mountPaths := make(map[string]string, len(jc.postProcessorPVCs))
		for _, pvc := range jc.postProcessorPVCs {
			mountPaths[pvc.claimName] = pvc.mountPath
		}
		// A map of strings cannot fail JSON encoding.
		encodedMountPaths, _ := json.Marshal(mountPaths)
		env = append(env, corev1.EnvVar{
			Name:  envPostProcessorPVCMountsName,
			Value: string(encodedMountPaths),
		})
		seen[envPostProcessorPVCMountsName] = true
	}

	// Set MLFLOW_TRACKING_SERVER_CERT_PATH so mlflow's tracking client trusts the
	// same CA bundle the sidecar uses (operator-merged MLflow CA, else service CA).
	// Note: we intentionally do NOT set REQUESTS_CA_BUNDLE, because it
	// overrides the system CA bundle globally for all Python requests calls,
	// breaking external HTTPS connections (e.g. HuggingFace tokenizer downloads).
	// The adapter SDK's httpx client auto-detects the service CA independently.
	if jc.mlflowTrackingURI != "" {
		if certPath := mlflowCACertPathForJob(jc, serviceConfig); certPath != "" {
			env = append(env, corev1.EnvVar{
				Name:  envMLFlowCertPathName,
				Value: certPath,
			})
			seen[envMLFlowCertPathName] = true
		}
	}

	// Add provider-specific environment variables
	for _, item := range jc.defaultEnv {
		if item.Name == "" || seen[item.Name] {
			continue
		}
		seen[item.Name] = true
		env = append(env, corev1.EnvVar{
			Name:  item.Name,
			Value: item.Value,
		})
	}

	// Inject OTEL endpoint so the adapter SDK can send traces and logs directly
	// to the collector. The adapter reads standard OTEL_EXPORTER_OTLP_ENDPOINT
	// and OTEL_SERVICE_NAME env vars; without them its configure_telemetry()
	// is a no-op and no telemetry is exported from the adapter container.
	if otelEndpoint := otelEndpointForAdapter(serviceConfig); otelEndpoint != "" {
		if !seen[envOTELExporterEndpointName] {
			env = append(env, corev1.EnvVar{
				Name:  envOTELExporterEndpointName,
				Value: otelEndpoint,
			})
			seen[envOTELExporterEndpointName] = true
		}
		if !seen[envOTELServiceNameName] {
			env = append(env, corev1.EnvVar{
				Name:  envOTELServiceNameName,
				Value: "evalhub-adapter",
			})
			seen[envOTELServiceNameName] = true
		}
	}

	return env
}

// otelEndpointForAdapter returns the OTLP endpoint URL the adapter SDK should
// use, or an empty string when OTEL is not configured. The scheme is derived
// from ExporterInsecure so the SDK can infer gRPC channel security.
func otelEndpointForAdapter(serviceConfig *config.Config) string {
	if serviceConfig == nil || serviceConfig.OTEL == nil || !serviceConfig.OTEL.Enabled {
		return ""
	}
	ep := strings.TrimSpace(serviceConfig.OTEL.ExporterEndpoint)
	if ep == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ep), "http://") || strings.HasPrefix(strings.ToLower(ep), "https://") {
		return ep
	}
	if serviceConfig.OTEL.ExporterInsecure {
		return "http://" + ep
	}
	return "https://" + ep
}

func buildResources(cfg *jobConfig) (corev1.ResourceRequirements, error) {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	if cfg.cpuRequest != "" {
		quantity, err := resource.ParseQuantity(cfg.cpuRequest)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse cpu request: %w", err)
		}
		resources.Requests[corev1.ResourceCPU] = quantity
	}
	if cfg.memoryRequest != "" {
		quantity, err := resource.ParseQuantity(cfg.memoryRequest)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse memory request: %w", err)
		}
		resources.Requests[corev1.ResourceMemory] = quantity
	}
	if cfg.cpuLimit != "" {
		quantity, err := resource.ParseQuantity(cfg.cpuLimit)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse cpu limit: %w", err)
		}
		resources.Limits[corev1.ResourceCPU] = quantity
	}
	if cfg.memoryLimit != "" {
		quantity, err := resource.ParseQuantity(cfg.memoryLimit)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse memory limit: %w", err)
		}
		resources.Limits[corev1.ResourceMemory] = quantity
	}
	if cfg.gpuCount > 0 && cfg.gpuResource != "" {
		gpuQty, err := resource.ParseQuantity(fmt.Sprintf("%d", cfg.gpuCount))
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse gpu count: %w", err)
		}
		// Kubernetes requires requests == limits for GPU extended resources.
		resources.Requests[corev1.ResourceName(cfg.gpuResource)] = gpuQty
		resources.Limits[corev1.ResourceName(cfg.gpuResource)] = gpuQty
	}
	// DRA: the adapter container consumes the pod-level claim by its pod-local
	// name (see buildResourceClaims); the sidecar and init containers stay CPU-only.
	// Kueue reads the claim reference for quota accounting, replacing the extended
	// resource requests above.
	if cfg.draResourceClaimTemplate != "" {
		resources.Claims = []corev1.ResourceClaim{{Name: cfg.draClaimName}}
	}
	if len(resources.Requests) == 0 {
		resources.Requests = nil
	}
	if len(resources.Limits) == 0 {
		resources.Limits = nil
	}
	return resources, nil
}

// buildResourceClaims returns the pod-level DRA claim declarations for the Job.
// A single claim references a namespace-local ResourceClaimTemplate — never a
// pre-created ResourceClaim, which would make the Job inadmissible to Kueue
// (Kueue only admits workload objects that create their own claims). The claim
// is consumed by the adapter container via resources.claims; the template, not
// the Job, defines the device class, attribute selectors, and topology
// constraints. Returns nil when DRA is not configured, leaving legacy pod specs
// unchanged.
func buildResourceClaims(cfg *jobConfig) []corev1.PodResourceClaim {
	if cfg.draResourceClaimTemplate == "" {
		return nil
	}
	templateName := cfg.draResourceClaimTemplate
	return []corev1.PodResourceClaim{
		{
			Name:                      cfg.draClaimName,
			ResourceClaimTemplateName: &templateName,
		},
	}
}
