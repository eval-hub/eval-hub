package k8s

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func sidecarPortFromInt(port int) (int32, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d out of range (1-65535)", port)
	}
	return int32(port), nil
}

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
func buildEnvVars(cfg *jobConfig) []corev1.EnvVar {
	var env []corev1.EnvVar
	seen := map[string]bool{}

	env = append(env, corev1.EnvVar{
		Name:  envEvalHubModeName,
		Value: "k8s",
	})
	seen[envEvalHubModeName] = true

	// When sidecar is at play, mlflow calls are proxied through the sidecar.
	mlflowTrackingURI := cfg.sidecarBaseURL
	// Add MLFlow environment variables if tracking is configured
	if cfg.mlflowTrackingURI != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowTrackingURIName,
			Value: mlflowTrackingURI,
		})
		seen[envMLFlowTrackingURIName] = true

	}
	if cfg.mlflowWorkspace != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowWorkspaceName,
			Value: cfg.mlflowWorkspace,
		})
		seen[envMLFlowWorkspaceName] = true
	}

	// Add OCI auth config path when credentials secret is configured
	if cfg.ociCredentialsSecret != "" {
		env = append(env, corev1.EnvVar{
			Name:  envOCIAuthConfigPathName,
			Value: ociCredentialsMountPath,
		})
		seen[envOCIAuthConfigPathName] = true
	}

	// Set MLFLOW_TRACKING_SERVER_CERT_PATH so mlflow's tracking client
	// trusts the OpenShift service-serving CA certificate for internal calls.
	// Note: we intentionally do NOT set REQUESTS_CA_BUNDLE, because it
	// overrides the system CA bundle globally for all Python requests calls,
	// breaking external HTTPS connections (e.g. HuggingFace tokenizer downloads).
	// The adapter SDK's httpx client auto-detects the service CA independently.
	if cfg.serviceCAConfigMap != "" && cfg.mlflowTrackingURI != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowCertPathName,
			Value: serviceCAMountPath + "/" + serviceCABundleFile,
		})
		seen[envMLFlowCertPathName] = true
	}

	// Add provider-specific environment variables
	for _, item := range cfg.defaultEnv {
		if item.Name == "" || seen[item.Name] {
			continue
		}
		seen[item.Name] = true
		env = append(env, corev1.EnvVar{
			Name:  item.Name,
			Value: item.Value,
		})
	}
	return env
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
	if len(resources.Requests) == 0 {
		resources.Requests = nil
	}
	if len(resources.Limits) == 0 {
		resources.Limits = nil
	}
	return resources, nil
}
