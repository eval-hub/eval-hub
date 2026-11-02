package e2e

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	devDeployManifestName        = "evalhub-dev-deploy-manifest"
	devDeployManagedByLabel      = "app.kubernetes.io/managed-by"
	devDeployManagedByValue      = "evalhub-dev-deploy"
	devDeployOwnershipAnnotation = "io.evalhub.kind-smoke.cluster"
	devDeployManifestVersion     = "1"
)

// deployedAdapter records the selected provider image and build provenance.
type deployedAdapter struct {
	image       string
	imageID     string
	sourceSHA   string
	sourceDirty string
}

// deploymentManifest is present only for environments created by dev-deploy.
// When it is absent, e2e tests retain their provider/resource auto-discovery
// behavior for manually configured clusters.
type deploymentManifest struct {
	managed          bool
	clusterName      string
	airgap           bool
	selectedAdapters map[string]deployedAdapter
}

func discoverDeploymentManifest(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
) (deploymentManifest, error) {
	configMap, err := kube.CoreV1().ConfigMaps(namespace).Get(ctx, devDeployManifestName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return deploymentManifest{}, nil
	}
	if err != nil {
		return deploymentManifest{}, fmt.Errorf("get ConfigMap/%s: %w", devDeployManifestName, err)
	}
	if configMap.Labels[devDeployManagedByLabel] != devDeployManagedByValue {
		return deploymentManifest{}, nil
	}

	if configMap.Data["schema_version"] != devDeployManifestVersion {
		return deploymentManifest{}, fmt.Errorf("ConfigMap/%s has unsupported schema_version %q",
			devDeployManifestName, configMap.Data["schema_version"])
	}
	clusterName := configMap.Data["cluster_name"]
	if clusterName == "" || configMap.Annotations[devDeployOwnershipAnnotation] != clusterName {
		return deploymentManifest{}, fmt.Errorf("ConfigMap/%s is missing matching cluster ownership metadata", devDeployManifestName)
	}

	manifest := deploymentManifest{
		managed:          true,
		clusterName:      clusterName,
		selectedAdapters: make(map[string]deployedAdapter),
	}
	switch configMap.Data["airgap"] {
	case "true":
		manifest.airgap = true
	case "false":
	default:
		return deploymentManifest{}, fmt.Errorf("ConfigMap/%s has invalid airgap value %q",
			devDeployManifestName, configMap.Data["airgap"])
	}

	seen := make(map[string]struct{})
	for _, adapterID := range strings.Split(configMap.Data["selected_adapters"], ",") {
		adapterID = strings.TrimSpace(adapterID)
		if adapterID == "" {
			return deploymentManifest{}, fmt.Errorf("ConfigMap/%s contains an empty selected adapter", devDeployManifestName)
		}
		if !supportedCuratedAdapter(adapterID) {
			return deploymentManifest{}, fmt.Errorf("ConfigMap/%s lists unsupported adapter %q", devDeployManifestName, adapterID)
		}
		if _, exists := seen[adapterID]; exists {
			return deploymentManifest{}, fmt.Errorf("ConfigMap/%s lists adapter %q more than once", devDeployManifestName, adapterID)
		}
		seen[adapterID] = struct{}{}

		adapter := deployedAdapter{
			image:       configMap.Data["image."+adapterID],
			imageID:     configMap.Data["image_id."+adapterID],
			sourceSHA:   configMap.Data["source."+adapterID+".revision"],
			sourceDirty: configMap.Data["source."+adapterID+".dirty"],
		}
		if adapter.image == "" {
			return deploymentManifest{}, fmt.Errorf("ConfigMap/%s has no image.%s for selected adapter %q",
				devDeployManifestName, adapterID, adapterID)
		}
		if adapter.imageID == "" {
			return deploymentManifest{}, fmt.Errorf("ConfigMap/%s has no image_id.%s for selected adapter %q",
				devDeployManifestName, adapterID, adapterID)
		}
		manifest.selectedAdapters[adapterID] = adapter
	}
	if len(manifest.selectedAdapters) == 0 {
		return deploymentManifest{}, fmt.Errorf("ConfigMap/%s selects no adapters", devDeployManifestName)
	}
	return manifest, nil
}

func discoverManagedDeploymentNamespaces(ctx context.Context, kube kubernetes.Interface) ([]string, error) {
	configMaps, err := kube.CoreV1().ConfigMaps(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector: devDeployManagedByLabel + "=" + devDeployManagedByValue,
	})
	if err != nil {
		return nil, fmt.Errorf("list dev-deploy manifests: %w", err)
	}

	namespaces := make([]string, 0, len(configMaps.Items))
	seen := make(map[string]struct{}, len(configMaps.Items))
	for _, configMap := range configMaps.Items {
		if _, exists := seen[configMap.Namespace]; exists {
			continue
		}
		seen[configMap.Namespace] = struct{}{}
		namespaces = append(namespaces, configMap.Namespace)
	}
	sort.Strings(namespaces)
	return namespaces, nil
}

func supportedCuratedAdapter(adapterID string) bool {
	switch adapterID {
	case "inspect", "lm-evaluation-harness", "lighteval", "ruler", "ifbench", "followbench":
		return true
	default:
		return false
	}
}

func (manifest deploymentManifest) adapterSelected(adapterID string) bool {
	if !manifest.managed {
		return true
	}
	_, selected := manifest.selectedAdapters[adapterID]
	return selected
}

func requireDisconnectedDeployment(t *testing.T, manifest deploymentManifest) {
	t.Helper()
	if manifest.managed && !manifest.airgap {
		t.Skipf("dev-deploy cluster %q was not configured with AIRGAP=true", manifest.clusterName)
	}
}

func (manifest deploymentManifest) verifyProviderImage(adapterID, providerImage string) error {
	if !manifest.managed {
		return nil
	}
	selected, ok := manifest.selectedAdapters[adapterID]
	if !ok {
		return fmt.Errorf("adapter %q is not selected in ConfigMap/%s", adapterID, devDeployManifestName)
	}
	if providerImage != selected.image {
		return fmt.Errorf("adapter %q provider image %q does not match selected deployment image %q",
			adapterID, providerImage, selected.image)
	}
	return nil
}

func findEvaluationAdapterPod(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
	jobID string,
) (*corev1.Pod, error) {
	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job_id=" + jobID})
	if err != nil {
		return nil, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, container := range pod.Spec.Containers {
			if container.Name == "adapter" {
				return pod, nil
			}
		}
	}
	return nil, nil
}

func adapterImageDetails(pod *corev1.Pod) (image, imageID string) {
	if pod == nil {
		return "", ""
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == "adapter" {
			image = container.Image
			break
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "adapter" {
			imageID = status.ImageID
			break
		}
	}
	return image, imageID
}

func logDeployedAdapter(t *testing.T, manifest deploymentManifest, adapterID string) {
	t.Helper()
	if !manifest.managed {
		return
	}
	adapter := manifest.selectedAdapters[adapterID]
	t.Logf("dev-deploy selection: adapter=%s image=%s imageID=%s sourceRevision=%s sourceDirty=%s cluster=%s airgap=%t",
		adapterID, adapter.image, adapter.imageID, adapter.sourceSHA, adapter.sourceDirty,
		manifest.clusterName, manifest.airgap)
}

func devDeployManifestForTest(namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devDeployManifestName,
			Namespace: namespace,
			Labels: map[string]string{
				devDeployManagedByLabel: devDeployManagedByValue,
			},
			Annotations: map[string]string{
				devDeployOwnershipAnnotation: data["cluster_name"],
			},
		},
		Data: data,
	}
}
