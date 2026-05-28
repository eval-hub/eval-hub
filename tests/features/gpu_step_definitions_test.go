package features

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// GPU-specific step definitions for testing GPU resource management

// GPU test resource state
type gpuTestResources struct {
	resourceFlavorsCreated []string
	clusterQueuesCreated   []string
	localQueuesCreated     map[string][]string          // namespace -> queue names
	nodeLabelsAdded        map[string]map[string]string // node -> label key -> original value
}

var (
	gpuResources = &gpuTestResources{
		localQueuesCreated: make(map[string][]string),
		nodeLabelsAdded:    make(map[string]map[string]string),
	}
	gpuResourcesSetup = false
)

// setupBasicGPUResources creates ResourceFlavors and ClusterQueues without GPU-specific nodeLabels
func setupBasicGPUResources() error {
	// Create default flavor without nodeSelector
	if err := applyYAML(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ResourceFlavor
metadata:
  name: test-default-flavor
spec: {}
`); err != nil {
		return fmt.Errorf("failed to create test-default-flavor ResourceFlavor: %w", err)
	}
	gpuResources.resourceFlavorsCreated = append(gpuResources.resourceFlavorsCreated, "test-default-flavor")

	// Create basic ClusterQueue without GPU-specific nodeLabels
	if err := applyYAML(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: gpu-cluster-queue
spec:
  namespaceSelector: {}
  resourceGroups:
  - coveredResources: ["cpu", "memory", "nvidia.com/gpu"]
    flavors:
    - name: test-default-flavor
      resources:
      - name: "cpu"
        nominalQuota: 100
      - name: "memory"
        nominalQuota: 200Gi
      - name: "nvidia.com/gpu"
        nominalQuota: 4
`); err != nil {
		return fmt.Errorf("failed to create gpu-cluster-queue ClusterQueue: %w", err)
	}
	gpuResources.clusterQueuesCreated = append(gpuResources.clusterQueuesCreated, "gpu-cluster-queue")

	// Create CPU-only ClusterQueue
	if err := applyYAML(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: cpu-only-cluster-queue
spec:
  namespaceSelector: {}
  resourceGroups:
  - coveredResources: ["cpu", "memory"]
    flavors:
    - name: test-default-flavor
      resources:
      - name: "cpu"
        nominalQuota: 100
      - name: "memory"
        nominalQuota: 200Gi
`); err != nil {
		return fmt.Errorf("failed to create cpu-only-cluster-queue ClusterQueue: %w", err)
	}
	gpuResources.clusterQueuesCreated = append(gpuResources.clusterQueuesCreated, "cpu-only-cluster-queue")

	return nil
}

// setupGPUResourcesWithNodeSelector creates ResourceFlavors and ClusterQueues with GPU-specific nodeLabels
func setupGPUResourcesWithNodeSelector(gpuProduct string) error {
	// Create ResourceFlavor for detected GPU
	resourceFlavorYAML := fmt.Sprintf(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ResourceFlavor
metadata:
  name: gpu-detected
spec:
  nodeLabels:
    nvidia.com/gpu.product: %s
`, gpuProduct)
	if err := applyYAML(resourceFlavorYAML); err != nil {
		return fmt.Errorf("failed to create gpu-detected ResourceFlavor: %w", err)
	}
	gpuResources.resourceFlavorsCreated = append(gpuResources.resourceFlavorsCreated, "gpu-detected")

	// Create default flavor without GPU
	if err := applyYAML(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ResourceFlavor
metadata:
  name: test-default-flavor
spec: {}
`); err != nil {
		return fmt.Errorf("failed to create test-default-flavor ResourceFlavor: %w", err)
	}
	gpuResources.resourceFlavorsCreated = append(gpuResources.resourceFlavorsCreated, "test-default-flavor")

	// Create ClusterQueue with detected GPU
	if err := applyYAML(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: gpu-cluster-queue
spec:
  namespaceSelector: {}
  resourceGroups:
  - coveredResources: ["cpu", "memory", "nvidia.com/gpu"]
    flavors:
    - name: gpu-detected
      resources:
      - name: "cpu"
        nominalQuota: 100
      - name: "memory"
        nominalQuota: 200Gi
      - name: "nvidia.com/gpu"
        nominalQuota: 4
`); err != nil {
		return fmt.Errorf("failed to create gpu-cluster-queue ClusterQueue: %w", err)
	}
	gpuResources.clusterQueuesCreated = append(gpuResources.clusterQueuesCreated, "gpu-cluster-queue")

	// Create ClusterQueue without GPU (CPU-only)
	if err := applyYAML(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: cpu-only-cluster-queue
spec:
  namespaceSelector: {}
  resourceGroups:
  - coveredResources: ["cpu", "memory"]
    flavors:
    - name: test-default-flavor
      resources:
      - name: "cpu"
        nominalQuota: 100
      - name: "memory"
        nominalQuota: 200Gi
`); err != nil {
		return fmt.Errorf("failed to create cpu-only-cluster-queue ClusterQueue: %w", err)
	}
	gpuResources.clusterQueuesCreated = append(gpuResources.clusterQueuesCreated, "cpu-only-cluster-queue")

	return nil
}

// setupGPUTestResources creates Kueue resources for GPU testing
func setupGPUTestResources(namespace string) error {
	logDebug("Setting up GPU test resources in namespace: %s\n", namespace)

	// Check if Kueue is installed
	cmd := exec.Command("oc", "get", "crd", "clusterqueues.kueue.x-k8s.io")
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.ToLower(string(output))
		if strings.Contains(out, "notfound") || strings.Contains(out, "not found") {
			logDebug("Kueue not installed, skipping GPU resource setup\n")
			return nil
		}
		return fmt.Errorf("failed to check Kueue CRD availability: %v, output: %s", err, string(output))
	}

	// Get GPU product from environment variable (skip nodeSelector tests if not set)
	gpuProduct := os.Getenv("GPU_PRODUCT")

	// Create ResourceFlavors and ClusterQueues based on GPU_PRODUCT setting
	if gpuProduct == "" {
		logDebug("GPU_PRODUCT environment variable not set, skipping GPU nodeSelector tests\n")
		// Create basic resources without GPU-specific nodeLabels
		if err := setupBasicGPUResources(); err != nil {
			return err
		}
	} else {
		logDebug("Using GPU product from environment: %s\n", gpuProduct)
		// Create resources with GPU-specific nodeLabels
		if err := setupGPUResourcesWithNodeSelector(gpuProduct); err != nil {
			return err
		}
	}
	// Create namespace if needed
	cmd = exec.Command("oc", "create", "namespace", namespace, "--dry-run=client", "-o", "yaml")
	out, _ := cmd.Output()
	cmd = exec.Command("oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(string(out))
	_ = cmd.Run()

	// Create LocalQueue pointing to GPU ClusterQueue
	if err := applyYAML(fmt.Sprintf(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  name: test-local-queue
  namespace: %s
spec:
  clusterQueue: gpu-cluster-queue
`, namespace)); err != nil {
		return fmt.Errorf("failed to create test-local-queue LocalQueue: %w", err)
	}
	gpuResources.localQueuesCreated[namespace] = append(gpuResources.localQueuesCreated[namespace], "test-local-queue")

	// Create LocalQueue pointing to CPU-only ClusterQueue
	if err := applyYAML(fmt.Sprintf(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  name: cpu-local-queue
  namespace: %s
spec:
  clusterQueue: cpu-only-cluster-queue
`, namespace)); err != nil {
		return fmt.Errorf("failed to create cpu-local-queue LocalQueue: %w", err)
	}
	gpuResources.localQueuesCreated[namespace] = append(gpuResources.localQueuesCreated[namespace], "cpu-local-queue")

	// Label one worker node with detected GPU for testing (only if GPU_PRODUCT is set)
	if gpuProduct != "" {
		cmd = exec.Command("oc", "get", "nodes", "-l", "node-role.kubernetes.io/worker", "-o", "jsonpath={.items[0].metadata.name}")
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			nodeName := strings.TrimSpace(string(output))
			labelKey := "nvidia.com/gpu.product"
			labelValue := gpuProduct

			// Save original label value if exists - use bracket notation for dotted keys
			cmd = exec.Command("oc", "get", "node", nodeName, "-o", fmt.Sprintf("jsonpath={.metadata.labels['%s']}", labelKey))
			origValue, _ := cmd.Output()

			if gpuResources.nodeLabelsAdded[nodeName] == nil {
				gpuResources.nodeLabelsAdded[nodeName] = make(map[string]string)
			}
			gpuResources.nodeLabelsAdded[nodeName][labelKey] = strings.TrimSpace(string(origValue))

			// Add the label
			cmd = exec.Command("oc", "label", "node", nodeName, fmt.Sprintf("%s=%s", labelKey, labelValue), "--overwrite")
			if err := cmd.Run(); err != nil {
				logDebug("WARNING: Failed to label node %s: %v\n", nodeName, err)
			} else {
				logDebug("Labeled node %s with %s=%s\n", nodeName, labelKey, labelValue)
			}
		}
	}

	logDebug("GPU test resources setup complete\n")
	return nil
}

// cleanupGPUTestResources removes Kueue resources created for testing
func cleanupGPUTestResources() error {
	logDebug("Cleaning up GPU test resources\n")

	// Delete LocalQueues
	for namespace, queues := range gpuResources.localQueuesCreated {
		for _, queue := range queues {
			cmd := exec.Command("oc", "delete", "localqueue", queue, "-n", namespace, "--ignore-not-found=true")
			_ = cmd.Run()
			logDebug("Deleted LocalQueue %s in namespace %s\n", queue, namespace)
		}

		// Delete GPU test provider ConfigMaps from this namespace
		for _, providerCM := range []string{"gpu-test-provider", "gpu-test-provider-a100", "gpu-test-provider-unavailable"} {
			cmd := exec.Command("oc", "delete", "configmap", providerCM, "-n", namespace, "--ignore-not-found=true")
			_ = cmd.Run()
			logDebug("Deleted ConfigMap %s in namespace %s\n", providerCM, namespace)
		}
	}

	// Delete ClusterQueues
	for _, cq := range gpuResources.clusterQueuesCreated {
		cmd := exec.Command("oc", "delete", "clusterqueue", cq, "--ignore-not-found=true", "--timeout=30s")
		_ = cmd.Run()
		logDebug("Deleted ClusterQueue %s\n", cq)
	}

	// Wait for Kueue to process ClusterQueue deletions and remove finalizers from ResourceFlavors
	// This prevents ResourceFlavor deletion from hanging
	time.Sleep(5 * time.Second)

	// Delete ResourceFlavors with timeout to prevent hanging
	for _, rf := range gpuResources.resourceFlavorsCreated {
		cmd := exec.Command("oc", "delete", "resourceflavor", rf, "--ignore-not-found=true", "--timeout=10s")
		_ = cmd.Run()
		logDebug("Deleted ResourceFlavor %s\n", rf)
	}

	// Restore original node labels
	for nodeName, labels := range gpuResources.nodeLabelsAdded {
		for labelKey, originalValue := range labels {
			if originalValue == "" {
				// Remove label if it didn't exist before
				cmd := exec.Command("oc", "label", "node", nodeName, labelKey+"-", "--ignore-not-found=true")
				_ = cmd.Run()
				logDebug("Removed label %s from node %s\n", labelKey, nodeName)
			} else {
				// Restore original value
				cmd := exec.Command("oc", "label", "node", nodeName, fmt.Sprintf("%s=%s", labelKey, originalValue), "--overwrite")
				_ = cmd.Run()
				logDebug("Restored label %s=%s on node %s\n", labelKey, originalValue, nodeName)
			}
		}
	}

	// Reset state
	gpuResources.resourceFlavorsCreated = nil
	gpuResources.clusterQueuesCreated = nil
	gpuResources.localQueuesCreated = make(map[string][]string)
	gpuResources.nodeLabelsAdded = make(map[string]map[string]string)

	logDebug("GPU test resources cleanup complete\n")
	return nil
}

// applyYAML applies YAML configuration using oc apply
func applyYAML(yaml string) error {
	cmd := exec.Command("oc", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yaml)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply YAML: %v, output: %s", err, string(output))
	}
	return nil
}

// setupGPUTestEnvironment is called before GPU test scenarios
func setupGPUTestEnvironment(namespace string) error {
	// Create GPU test provider ConfigMaps first
	if err := createGPUTestProviders(namespace); err != nil {
		logDebug("WARNING: Failed to create GPU test providers: %v\n", err)
	}

	// Setup Kueue resources
	return setupGPUTestResources(namespace)
}

// createGPUTestProviders creates ConfigMaps with GPU provider definitions
func createGPUTestProviders(namespace string) error {
	logDebug("Creating GPU test provider ConfigMaps in namespace: %s\n", namespace)

	// Create basic GPU test provider (no nodeSelector)
	basicProviderYAML := `id: gpu_test_provider
name: GPU Test Provider
title: GPU Test Provider (minimal lm_eval_harness with GPU)
description: >
  Minimal test provider based on lm_evaluation_harness with GPU configuration for testing GPU resource allocation

runtime:
  k8s:
    image: quay.io/opendatahub/ta-lmes-job:odh-3.4-ea2
    entrypoint:
      - /opt/app-root/bin/python
      - /opt/app-root/src/main.py
    cpu_request: 100m
    memory_request: 128Mi
    cpu_limit: 500m
    memory_limit: 4Gi
    gpu:
      resource: nvidia.com/gpu
      count: 1
  local:
    command: python tests/features/test_data/runtime/main.py

benchmarks:
  - id: arc_easy
    name: "Basic science Q&A (GPU test)"
    description: |-
      Grade-school science questions testing basic reasoning and scientific knowledge (AI2 Reasoning Challenge, easy split).
    category: reasoning
    metrics:
      - acc
      - acc_norm
    num_few_shot: 0
    dataset_size: 2376
    tags:
      - reasoning
      - science
      - gpu_test
    primary_score:
      metric: acc_norm
      lower_is_better: false
    pass_criteria:
      threshold: 0.25
`

	cmd := exec.Command("oc", "create", "configmap", "gpu-test-provider",
		"--from-literal=provider_gpu_test.yaml="+basicProviderYAML,
		"-n", namespace,
		"--dry-run=client", "-o", "yaml")
	output, _ := cmd.Output()

	applyCmd := exec.Command("oc", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(string(output))
	if err := applyCmd.Run(); err != nil {
		logDebug("WARNING: Could not create gpu-test-provider configmap: %v\n", err)
	}

	// Create GPU test provider with A100 nodeSelector
	a100ProviderYAML := `id: gpu_test_provider_a100
name: GPU Test Provider A100
title: GPU Test Provider with A100 nodeSelector
description: >
  GPU test provider with A100 nodeSelector for testing GPU node selection

runtime:
  k8s:
    image: quay.io/opendatahub/ta-lmes-job:odh-3.4-ea2
    entrypoint:
      - /opt/app-root/bin/python
      - /opt/app-root/src/main.py
    cpu_request: 100m
    memory_request: 128Mi
    cpu_limit: 500m
    memory_limit: 4Gi
    gpu:
      resource: nvidia.com/gpu
      count: 1
      node_selector:
        nvidia.com/gpu.product: A100-SXM4-40GB
  local:
    command: python tests/features/test_data/runtime/main.py

benchmarks:
  - id: arc_easy
    name: "Basic science Q&A (GPU test)"
    description: |-
      Grade-school science questions testing basic reasoning and scientific knowledge (AI2 Reasoning Challenge, easy split).
    category: reasoning
    metrics:
      - acc
      - acc_norm
    num_few_shot: 0
    dataset_size: 2376
    tags:
      - reasoning
      - science
      - gpu_test
    primary_score:
      metric: acc_norm
      lower_is_better: false
    pass_criteria:
      threshold: 0.25
`

	cmd = exec.Command("oc", "create", "configmap", "gpu-test-provider-a100",
		"--from-literal=provider_gpu_a100.yaml="+a100ProviderYAML,
		"-n", namespace,
		"--dry-run=client", "-o", "yaml")
	output, _ = cmd.Output()

	applyCmd = exec.Command("oc", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(string(output))
	if err := applyCmd.Run(); err != nil {
		logDebug("WARNING: Could not create gpu-test-provider-a100 configmap: %v\n", err)
	}

	// Create GPU test provider for unavailable GPU type (H100)
	unavailableProviderYAML := `id: gpu_test_provider_unavailable
name: GPU Test Provider Unavailable
title: GPU Test Provider with H100 nodeSelector (unavailable)
description: >
  GPU test provider with H100 nodeSelector for testing unavailable GPU scenarios

runtime:
  k8s:
    image: quay.io/opendatahub/ta-lmes-job:odh-3.4-ea2
    entrypoint:
      - /opt/app-root/bin/python
      - /opt/app-root/src/main.py
    cpu_request: 100m
    memory_request: 128Mi
    cpu_limit: 500m
    memory_limit: 4Gi
    gpu:
      resource: nvidia.com/gpu
      count: 1
      node_selector:
        nvidia.com/gpu.product: NVIDIA-H100-80GB-HBM3
  local:
    command: python tests/features/test_data/runtime/main.py

benchmarks:
  - id: arc_easy
    name: "Basic science Q&A (GPU test)"
    description: |-
      Grade-school science questions testing basic reasoning and scientific knowledge (AI2 Reasoning Challenge, easy split).
    category: reasoning
    metrics:
      - acc
      - acc_norm
    num_few_shot: 0
    dataset_size: 2376
    tags:
      - reasoning
      - science
      - gpu_test
    primary_score:
      metric: acc_norm
      lower_is_better: false
    pass_criteria:
      threshold: 0.25
`

	cmd = exec.Command("oc", "create", "configmap", "gpu-test-provider-unavailable",
		"--from-literal=provider_gpu_unavailable.yaml="+unavailableProviderYAML,
		"-n", namespace,
		"--dry-run=client", "-o", "yaml")
	output, _ = cmd.Output()

	applyCmd = exec.Command("oc", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(string(output))
	if err := applyCmd.Run(); err != nil {
		logDebug("WARNING: Could not create gpu-test-provider-unavailable configmap: %v\n", err)
	}

	logDebug("GPU test provider ConfigMaps created\n")
	return nil
}

// GPU-specific step definitions for testing GPU resource management

func (tc *scenarioConfig) iWaitForKubernetesJobToBeCreated(evalJobID string) error {
	id, err := tc.getValue(evalJobID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	for {
		select {
		case <-ctx.Done():
			return tc.logError(fmt.Errorf("timeout waiting for Kubernetes Job to be created for eval job %s", id))
		case <-ticker.C:
			cmd := exec.Command("oc", "get", "job", "-n", namespace, "-l", fmt.Sprintf("job_id=%s", id), "-o", "jsonpath={.items[0].metadata.name}")
			output, err := cmd.CombinedOutput()
			if err == nil && string(output) != "" {
				logDebug("Kubernetes Job created: %s\n", string(output))
				return nil
			}
		}
	}
}

func (tc *scenarioConfig) iWaitForKubernetesJobToComplete(evalJobID string) error {
	id, err := tc.getValue(evalJobID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	for {
		select {
		case <-ctx.Done():
			return tc.logError(fmt.Errorf("timeout waiting for Kubernetes Job to complete for eval job %s", id))
		case <-ticker.C:
			// Check Job status
			cmd := exec.Command("oc", "get", "job", "-n", namespace, "-l", fmt.Sprintf("job_id=%s", id), "-o", "jsonpath={.items[0].status.conditions[?(@.type=='Complete')].status}")
			output, err := cmd.CombinedOutput()
			if err == nil && strings.TrimSpace(string(output)) == "True" {
				logDebug("Kubernetes Job completed for eval job %s\n", id)
				return nil
			}

			// Check for Job failure
			cmd = exec.Command("oc", "get", "job", "-n", namespace, "-l", fmt.Sprintf("job_id=%s", id), "-o", "jsonpath={.items[0].status.conditions[?(@.type=='Failed')].status}")
			output, err = cmd.CombinedOutput()
			if err == nil && strings.TrimSpace(string(output)) == "True" {
				// Get failure reason
				cmd = exec.Command("oc", "get", "job", "-n", namespace, "-l", fmt.Sprintf("job_id=%s", id), "-o", "jsonpath={.items[0].status.conditions[?(@.type=='Failed')].message}")
				msg, _ := cmd.CombinedOutput()
				return tc.logError(fmt.Errorf("Kubernetes Job failed for eval job %s: %s", id, string(msg)))
			}
		}
	}
}

func (tc *scenarioConfig) jobSpecShouldHaveGPURequest(expectedValue string) error {
	return tc.checkJobResourceSpec("requests", "nvidia.com/gpu", expectedValue)
}

func (tc *scenarioConfig) jobSpecShouldHaveGPULimit(expectedValue string) error {
	return tc.checkJobResourceSpec("limits", "nvidia.com/gpu", expectedValue)
}

func (tc *scenarioConfig) checkJobResourceSpec(resourceType, resourceName, expectedValue string) error {
	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	id := tc.lastId
	if id == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}

	// First verify the Job exists
	checkCmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].metadata.name}")
	checkOutput, err := checkCmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(checkOutput)) == "" {
		return tc.logError(fmt.Errorf("Job with job_id=%s not found in namespace %s: %v, output: %s", id, namespace, err, string(checkOutput)))
	}
	jobName := strings.TrimSpace(string(checkOutput))
	logDebug("Found Job: %s\n", jobName)

	// GPU resources are on the adapter container
	// Escape dots in resource name for JSONPath (nvidia.com/gpu -> nvidia\.com/gpu)
	escapedResourceName := strings.ReplaceAll(resourceName, ".", "\\.")
	jsonPath := fmt.Sprintf("jsonpath={.items[0].spec.template.spec.containers[0].resources.%s.%s}", resourceType, escapedResourceName)
	cmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", jsonPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get Job %s for resource %s: %v, output: %s", resourceType, resourceName, err, string(output)))
	}

	actualValue := strings.TrimSpace(string(output))
	if actualValue != expectedValue {
		return tc.logError(fmt.Errorf("expected GPU %s to be %s, got %s", resourceType, expectedValue, actualValue))
	}

	logDebug("Job has GPU %s set to %s\n", resourceType, actualValue)
	return nil
}

func (tc *scenarioConfig) jobSpecShouldHaveNodeSelector(selectorKeyValue string) error {
	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	id := tc.lastId
	if id == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}

	parts := strings.SplitN(selectorKeyValue, "=", 2)
	if len(parts) != 2 {
		return tc.logError(fmt.Errorf("invalid nodeSelector format: %s, expected key=value", selectorKeyValue))
	}
	key := parts[0]
	expectedValue := parts[1]

	// First, let's debug by getting the full nodeSelector to see what's actually set
	debugCmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].spec.template.spec.nodeSelector}")
	debugOutput, _ := debugCmd.CombinedOutput()
	logDebug("DEBUG: Full nodeSelector: %s\n", string(debugOutput))

	// Escape dots in key for JSONPath (nvidia.com/gpu.product -> nvidia\.com/gpu\.product)
	escapedKey := strings.ReplaceAll(key, ".", "\\.")
	jsonPath := fmt.Sprintf("jsonpath={.items[0].spec.template.spec.nodeSelector.%s}", escapedKey)
	cmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", jsonPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get Job nodeSelector for %s: %v, output: %s", key, err, string(output)))
	}

	actualValue := strings.TrimSpace(string(output))
	if actualValue != expectedValue {
		return tc.logError(fmt.Errorf("expected nodeSelector %s to be %s, got %s", key, expectedValue, actualValue))
	}

	logDebug("Job has nodeSelector %s=%s\n", key, actualValue)
	return nil
}

func (tc *scenarioConfig) jobSpecShouldNotHaveNodeSelector() error {
	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	id := tc.lastId
	if id == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}

	cmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].spec.template.spec.nodeSelector}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get Job nodeSelector: %v, output: %s", err, string(output)))
	}

	actualValue := strings.TrimSpace(string(output))
	if actualValue != "" && actualValue != "<nil>" && actualValue != "null" && actualValue != "map[]" {
		return tc.logError(fmt.Errorf("expected no nodeSelector, but found: %s", actualValue))
	}

	logDebug("Job has no nodeSelector as expected\n")
	return nil
}

func (tc *scenarioConfig) jobSpecShouldHaveLabel(labelKeyValue string) error {
	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	id := tc.lastId
	if id == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}

	// First, debug by getting all labels
	debugCmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].metadata.labels}")
	debugOutput, _ := debugCmd.CombinedOutput()
	logDebug("DEBUG: Full Job labels: %s\n", string(debugOutput))

	parts := strings.SplitN(labelKeyValue, "=", 2)
	if len(parts) != 2 {
		return tc.logError(fmt.Errorf("invalid label format: %s, expected key=value", labelKeyValue))
	}
	key := parts[0]
	expectedValue := parts[1]

	// Escape both dots and slashes for JSONPath (kueue.x-k8s.io/queue-name -> kueue\.x-k8s\.io\/queue-name)
	escapedKey := strings.ReplaceAll(key, ".", "\\.")
	escapedKey = strings.ReplaceAll(escapedKey, "/", "\\/")
	jsonPath := fmt.Sprintf("jsonpath={.items[0].metadata.labels.%s}", escapedKey)
	cmd := exec.Command("oc", "get", "job", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", jsonPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get Job label %s: %v, output: %s", key, err, string(output)))
	}

	actualValue := strings.TrimSpace(string(output))
	if actualValue != expectedValue {
		return tc.logError(fmt.Errorf("expected label %s to be %s, got %s", key, expectedValue, actualValue))
	}

	logDebug("Job has label %s=%s\n", key, actualValue)
	return nil
}

func (tc *scenarioConfig) podShouldHaveGPUAttached(evalJobID string) error {
	id, err := tc.getValue(evalJobID)
	if err != nil {
		return err
	}

	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	// Escape dots in resource name for JSONPath
	// Use index 0 (single container mode)
	cmd := exec.Command("oc", "get", "pod", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].spec.containers[0].resources.limits.nvidia\\.com/gpu}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to check GPU on pod: %v, output: %s", err, string(output)))
	}

	gpuLimit := strings.TrimSpace(string(output))
	if gpuLimit == "" || gpuLimit == "0" {
		return tc.logError(fmt.Errorf("pod does not have GPU attached, gpu limit: %s", gpuLimit))
	}

	logDebug("Pod has GPU attached with limit: %s\n", gpuLimit)
	return nil
}

func (tc *scenarioConfig) podShouldBeOnNodeWithLabel(evalJobID, labelKeyValue string) error {
	// Skip if GPU_PRODUCT is not set (nodeSelector tests are skipped)
	if os.Getenv("GPU_PRODUCT") == "" {
		logDebug("Skipping pod node label check (GPU_PRODUCT not set)\n")
		return nil
	}

	id, err := tc.getValue(evalJobID)
	if err != nil {
		return err
	}

	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	parts := strings.SplitN(labelKeyValue, "=", 2)
	if len(parts) != 2 {
		return tc.logError(fmt.Errorf("invalid label format: %s, expected key=value", labelKeyValue))
	}
	key := parts[0]
	expectedValue := parts[1]

	// First get the node name
	cmd := exec.Command("oc", "get", "pod", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].spec.nodeName}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get pod node name: %v, output: %s", err, string(output)))
	}

	nodeName := strings.TrimSpace(string(output))
	if nodeName == "" {
		return tc.logError(fmt.Errorf("pod is not scheduled on any node"))
	}

	// Then check the node label - use bracket notation with single quotes for dotted keys
	jsonPath := fmt.Sprintf("jsonpath={.metadata.labels['%s']}", key)
	cmd = exec.Command("oc", "get", "node", nodeName, "-o", jsonPath)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get node label %s: %v, output: %s", key, err, string(output)))
	}

	actualValue := strings.TrimSpace(string(output))
	if actualValue != expectedValue {
		return tc.logError(fmt.Errorf("expected node label %s to be %s, got %s", key, expectedValue, actualValue))
	}

	logDebug("Pod is on node %s with label %s=%s\n", nodeName, key, actualValue)
	return nil
}

func (tc *scenarioConfig) podShouldNotBeScheduled(evalJobID string) error {
	id, err := tc.getValue(evalJobID)
	if err != nil {
		return err
	}

	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	cmd := exec.Command("oc", "get", "pod", "-n", namespace, "-l",
		fmt.Sprintf("job_id=%s", id),
		"-o", "jsonpath={.items[0].status.phase}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.ToLower(string(output))
		// Pod might not exist yet, which is fine
		if strings.Contains(out, "not found") || strings.Contains(out, "array index out of bounds") {
			return nil
		}
		return tc.logError(fmt.Errorf("failed to check pod scheduling state: %v, output: %s", err, string(output)))
	}

	phase := strings.TrimSpace(string(output))
	if phase != "Pending" && phase != "" {
		return tc.logError(fmt.Errorf("expected pod not to be scheduled, but it's in phase: %s", phase))
	}

	logDebug("Pod is not scheduled as expected (phase: %s)\n", phase)
	return nil
}

func (tc *scenarioConfig) iWaitForSchedulingError(duration string) error {
	// Parse duration
	waitDuration, err := time.ParseDuration(duration)
	if err != nil {
		return tc.logError(fmt.Errorf("invalid duration format: %s", duration))
	}

	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}

	id := tc.lastId
	if id == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitDuration)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logDebug("Finished waiting for scheduling error\n")
			return nil
		case <-ticker.C:
			// Check pod events for scheduling errors
			cmd := exec.Command("oc", "get", "events", "-n", namespace,
				"--field-selector", "involvedObject.kind=Pod,reason=FailedScheduling",
				"-o", "jsonpath={.items[*].message}")
			output, err := cmd.CombinedOutput()
			if err == nil && strings.Contains(string(output), "gpu") {
				logDebug("Found scheduling error: %s\n", string(output))
				return nil
			}
		}
	}
}

func (tc *scenarioConfig) responseShouldContainGPUErrorMessage() error {
	bodyStr := string(tc.body)
	if !strings.Contains(strings.ToLower(bodyStr), "gpu") &&
		!strings.Contains(strings.ToLower(bodyStr), "unavailable") &&
		!strings.Contains(strings.ToLower(bodyStr), "not available") &&
		!strings.Contains(strings.ToLower(bodyStr), "scheduling") {
		return tc.logError(fmt.Errorf("response does not contain error message about GPU availability: %s", bodyStr))
	}
	logDebug("Response contains GPU error message\n")
	return nil
}

func (tc *scenarioConfig) responseShouldContainQueueGPUErrorMessage() error {
	bodyStr := string(tc.body)
	if !strings.Contains(strings.ToLower(bodyStr), "gpu") &&
		!strings.Contains(strings.ToLower(bodyStr), "queue") &&
		!strings.Contains(strings.ToLower(bodyStr), "unavailable") {
		return tc.logError(fmt.Errorf("response does not contain error message about GPU availability in queue: %s", bodyStr))
	}
	logDebug("Response contains queue GPU error message\n")
	return nil
}

func (tc *scenarioConfig) gpuNodeWithLabelExists(labelKeyValue string) error {
	// Skip if GPU_PRODUCT is not set (nodeSelector tests are skipped)
	if os.Getenv("GPU_PRODUCT") == "" {
		logDebug("Skipping GPU node label check (GPU_PRODUCT not set)\n")
		return nil
	}

	parts := strings.SplitN(labelKeyValue, "=", 2)
	if len(parts) != 2 {
		return tc.logError(fmt.Errorf("invalid label format: %s, expected key=value", labelKeyValue))
	}
	key := parts[0]
	value := parts[1]

	cmd := exec.Command("oc", "get", "nodes", "-l", fmt.Sprintf("%s=%s", key, value), "-o", "jsonpath={.items[*].metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to check for GPU nodes: %v, output: %s", err, string(output)))
	}

	if strings.TrimSpace(string(output)) == "" {
		logDebug("WARNING: No GPU nodes found with label %s=%s, test may fail\n", key, value)
	} else {
		logDebug("Found GPU node(s) with label %s=%s: %s\n", key, value, string(output))
	}

	return nil
}

func (tc *scenarioConfig) kueueIsInstalled() error {
	cmd := exec.Command("oc", "get", "crd", "clusterqueues.kueue.x-k8s.io")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("Kueue is not installed on the cluster: %v, output: %s", err, string(output)))
	}
	logDebug("Kueue is installed\n")
	return nil
}

func (tc *scenarioConfig) clusterQueueWithGPUExists(clusterQueueName string) error {
	cmd := exec.Command("oc", "get", "clusterqueue", clusterQueueName, "-o", "jsonpath={.metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return tc.logError(fmt.Errorf("ClusterQueue %s does not exist: %v, output: %s", clusterQueueName, err, string(output)))
	}
	logDebug("ClusterQueue %s exists\n", clusterQueueName)
	return nil
}

func (tc *scenarioConfig) clusterQueueWithGPUFlavorExists(clusterQueueName, flavorName string) error {
	// Create or update the ClusterQueue to use the specified flavor
	yaml := fmt.Sprintf(`
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: %s
spec:
  namespaceSelector: {}
  resourceGroups:
  - coveredResources: ["cpu", "memory", "nvidia.com/gpu"]
    flavors:
    - name: %s
      resources:
      - name: "cpu"
        nominalQuota: 100
      - name: "memory"
        nominalQuota: 200Gi
      - name: "nvidia.com/gpu"
        nominalQuota: 4
`, clusterQueueName, flavorName)

	if err := applyYAML(yaml); err != nil {
		return tc.logError(fmt.Errorf("failed to create ClusterQueue %s with flavor %s: %v", clusterQueueName, flavorName, err))
	}

	// Track for cleanup (add to gpuResources if not already there)
	found := false
	for _, cq := range gpuResources.clusterQueuesCreated {
		if cq == clusterQueueName {
			found = true
			break
		}
	}
	if !found {
		gpuResources.clusterQueuesCreated = append(gpuResources.clusterQueuesCreated, clusterQueueName)
	}

	logDebug("ClusterQueue %s with flavor %s exists\n", clusterQueueName, flavorName)
	return nil
}

func (tc *scenarioConfig) clusterQueueWithoutGPUExists(clusterQueueName string) error {
	cmd := exec.Command("oc", "get", "clusterqueue", clusterQueueName, "-o", "jsonpath={.metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return tc.logError(fmt.Errorf("ClusterQueue %s does not exist: %v, output: %s", clusterQueueName, err, string(output)))
	}
	logDebug("ClusterQueue %s (without GPU) exists\n", clusterQueueName)
	return nil
}

func (tc *scenarioConfig) localQueueExists(localQueueName, namespace string) error {
	ns, err := tc.getValue(namespace)
	if err != nil {
		return err
	}

	cmd := exec.Command("oc", "get", "localqueue", localQueueName, "-n", ns, "-o", "jsonpath={.metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return tc.logError(fmt.Errorf("LocalQueue %s does not exist in namespace %s: %v, output: %s", localQueueName, ns, err, string(output)))
	}
	logDebug("LocalQueue %s exists in namespace %s\n", localQueueName, ns)
	return nil
}

func (tc *scenarioConfig) resourceFlavorHasNodeSelector(flavorName, selectorKeyValue string) error {
	// Skip if GPU_PRODUCT is not set (nodeSelector tests are skipped)
	if os.Getenv("GPU_PRODUCT") == "" {
		logDebug("Skipping ResourceFlavor nodeSelector check (GPU_PRODUCT not set)\n")
		return nil
	}

	parts := strings.SplitN(selectorKeyValue, "=", 2)
	if len(parts) != 2 {
		return tc.logError(fmt.Errorf("invalid nodeSelector format: %s, expected key=value", selectorKeyValue))
	}
	key := parts[0]
	expectedValue := parts[1]

	// Debug: get full nodeLabels first
	debugCmd := exec.Command("oc", "get", "resourceflavor", flavorName, "-o", "jsonpath={.spec.nodeLabels}")
	debugOutput, _ := debugCmd.CombinedOutput()
	logDebug("DEBUG: ResourceFlavor %s nodeLabels: %s\n", flavorName, string(debugOutput))

	// Escape dots in key for JSONPath (nvidia.com/gpu.product -> nvidia\.com/gpu\.product)
	escapedKey := strings.ReplaceAll(key, ".", "\\.")
	jsonPath := fmt.Sprintf("jsonpath={.spec.nodeLabels.%s}", escapedKey)
	cmd := exec.Command("oc", "get", "resourceflavor", flavorName, "-o", jsonPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get ResourceFlavor %s nodeSelector: %v, output: %s", flavorName, err, string(output)))
	}

	actualValue := strings.TrimSpace(string(output))
	if actualValue != expectedValue {
		return tc.logError(fmt.Errorf("expected ResourceFlavor %s nodeSelector %s to be %s, got %s", flavorName, key, expectedValue, actualValue))
	}

	logDebug("ResourceFlavor %s has nodeSelector %s=%s\n", flavorName, key, actualValue)
	return nil
}

func (tc *scenarioConfig) gpuTestProviderIsLoaded() error {
	baseURL := os.Getenv("SERVICE_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8443"
	}

	tenant := tc.reqHeaders["X-Tenant"]
	if tenant == "" {
		tenant = "test-tenant"
	}

	// Get auth token
	token := os.Getenv("AUTH_TOKEN")
	if token == "" {
		// Try to get from oc command
		saName := os.Getenv("SERVICE_ACCOUNT_NAME")
		if saName == "" {
			saName = "evalhub-service"
		}

		cmd := exec.Command("oc", "create", "token", saName, "-n", tenant, "--duration=10m")
		output, err := cmd.Output()
		if err != nil {
			return tc.logError(fmt.Errorf("failed to get auth token: %v", err))
		}
		token = strings.TrimSpace(string(output))
	}

	// Check if GPU provider exists and has GPU configuration
	// Remove -f flag to allow checking response even on HTTP errors
	cmd := exec.Command("curl", "-s", "-k",
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
		"-H", fmt.Sprintf("X-Tenant: %s", tenant),
		fmt.Sprintf("%s/api/v1/evaluations/providers/gpu_test_provider", baseURL))

	output, err := cmd.CombinedOutput()
	if err != nil {
		logDebug("WARNING: Failed to check GPU test provider: %v\n", err)
		logDebug("Skipping provider validation - tests will proceed but may fail if provider doesn't exist\n")
		return nil
	}

	// Check if provider exists (not a 404 error)
	if strings.Contains(string(output), "resource_not_found") || strings.Contains(string(output), "not found") {
		logDebug("WARNING: GPU test provider 'gpu_test_provider' not found on server\n")
		logDebug("Tests will use whatever GPU providers are available on the server\n")
		return nil
	}

	// Verify GPU config exists in response
	if !strings.Contains(string(output), `"gpu"`) {
		logDebug("WARNING: GPU test provider exists but has no GPU configuration - check eval-hub image version\n")
		return nil
	}

	logDebug("GPU test provider validated successfully\n")
	return nil
}

// InitializeGPUTestSuite registers GPU test suite-level hooks
func InitializeGPUTestSuite(ctx *godog.TestSuiteContext) {
	ctx.BeforeSuite(func() {
		// Check if we're running GPU tests by looking at tags
		// This will be called for all test runs, but we only setup if needed
		namespace := os.Getenv("X_TENANT")
		if namespace == "" {
			namespace = "test-tenant"
		}

		logDebug("Setting up GPU test resources for test suite\n")
		if err := setupGPUTestEnvironment(namespace); err != nil {
			logDebug("WARNING: Failed to setup GPU test environment: %v\n", err)
			logDebug("GPU tests may be skipped or will fail if they run\n")
		} else {
			gpuResourcesSetup = true
		}
	})

	ctx.AfterSuite(func() {
		if gpuResourcesSetup {
			logDebug("Cleaning up GPU test resources for test suite\n")
			if err := cleanupGPUTestResources(); err != nil {
				logDebug("WARNING: Failed to cleanup GPU test resources: %v\n", err)
			}
			gpuResourcesSetup = false
		}
	})
}

// InitializeGPUSteps registers GPU-specific step definitions
func InitializeGPUSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {

	ctx.Step(`^I wait for the Kubernetes Job to be created for evaluation job "([^"]*)"$`, tc.iWaitForKubernetesJobToBeCreated)
	ctx.Step(`^I wait for the Kubernetes Job to complete for evaluation job "([^"]*)"$`, tc.iWaitForKubernetesJobToComplete)
	ctx.Step(`^the Job spec should have GPU request set to "([^"]*)"$`, tc.jobSpecShouldHaveGPURequest)
	ctx.Step(`^the Job spec should have GPU limit set to "([^"]*)"$`, tc.jobSpecShouldHaveGPULimit)
	ctx.Step(`^the Job spec should have nodeSelector "([^"]*)"$`, tc.jobSpecShouldHaveNodeSelector)
	ctx.Step(`^the Job spec should not have nodeSelector$`, tc.jobSpecShouldNotHaveNodeSelector)
	ctx.Step(`^the Job spec should have label "([^"]*)"$`, tc.jobSpecShouldHaveLabel)
	ctx.Step(`^the pod for evaluation job "([^"]*)" should have a GPU attached$`, tc.podShouldHaveGPUAttached)
	ctx.Step(`^the pod for evaluation job "([^"]*)" should be on a node with label "([^"]*)"$`, tc.podShouldBeOnNodeWithLabel)
	ctx.Step(`^the pod for evaluation job "([^"]*)" should not be scheduled$`, tc.podShouldNotBeScheduled)
	ctx.Step(`^I wait up to "([^"]*)" for the evaluation job to have scheduling error$`, tc.iWaitForSchedulingError)
	ctx.Step(`^the response should contain an error message about GPU not being available$`, tc.responseShouldContainGPUErrorMessage)
	ctx.Step(`^the response should contain an error message about GPU not being available in queue$`, tc.responseShouldContainQueueGPUErrorMessage)
	ctx.Step(`^GPU node with label "([^"]*)" exists$`, tc.gpuNodeWithLabelExists)
	ctx.Step(`^the GPU test provider is loaded$`, tc.gpuTestProviderIsLoaded)
	ctx.Step(`^Kueue is installed on the cluster$`, tc.kueueIsInstalled)
	ctx.Step(`^ClusterQueue "([^"]*)" with GPU ResourceFlavor exists$`, tc.clusterQueueWithGPUExists)
	ctx.Step(`^ClusterQueue "([^"]*)" with GPU ResourceFlavor "([^"]*)" exists$`, tc.clusterQueueWithGPUFlavorExists)
	ctx.Step(`^ClusterQueue "([^"]*)" without GPU ResourceFlavor exists$`, tc.clusterQueueWithoutGPUExists)
	ctx.Step(`^LocalQueue "([^"]*)" in namespace "([^"]*)" exists$`, tc.localQueueExists)
	ctx.Step(`^ResourceFlavor "([^"]*)" has nodeSelector "([^"]*)"$`, tc.resourceFlavorHasNodeSelector)
}
