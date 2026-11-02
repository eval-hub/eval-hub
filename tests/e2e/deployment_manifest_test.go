package e2e

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDiscoverDeploymentManifest(t *testing.T) {
	const namespace = "system"
	validData := map[string]string{
		"schema_version":                        devDeployManifestVersion,
		"cluster_name":                          "evalhub-dev",
		"airgap":                                "true",
		"selected_adapters":                     "inspect,lm-evaluation-harness",
		"image.inspect":                         "localhost:32769/evalhub/community-inspect:kind-local",
		"image_id.inspect":                      "sha256:inspect-image-config",
		"source.inspect.revision":               "abc123",
		"source.inspect.dirty":                  "false",
		"image.lm-evaluation-harness":           "localhost:32769/evalhub/community-lmes:kind-local",
		"image_id.lm-evaluation-harness":        "sha256:lmes-image-config",
		"source.lm-evaluation-harness.revision": "def456",
		"source.lm-evaluation-harness.dirty":    "true",
	}

	tests := []struct {
		name      string
		configMap *corev1.ConfigMap
		wantApply bool
		wantError bool
	}{
		{name: "absent manifest falls back to auto-discovery"},
		{
			name: "unowned configmap is ignored",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: devDeployManifestName, Namespace: namespace},
				Data:       validData,
			},
		},
		{
			name:      "valid manifest discovers selected images and provenance",
			configMap: devDeployManifestForTest(namespace, validData),
			wantApply: true,
		},
		{
			name: "unsupported schema is rejected",
			configMap: devDeployManifestForTest(namespace, map[string]string{
				"schema_version": "99",
				"cluster_name":   "evalhub-dev",
			}),
			wantError: true,
		},
		{
			name: "unsupported adapter is rejected",
			configMap: devDeployManifestForTest(namespace, map[string]string{
				"schema_version":    devDeployManifestVersion,
				"cluster_name":      "evalhub-dev",
				"airgap":            "false",
				"selected_adapters": "unknown",
			}),
			wantError: true,
		},
		{
			name: "selected adapter requires image and image ID",
			configMap: devDeployManifestForTest(namespace, map[string]string{
				"schema_version":    devDeployManifestVersion,
				"cluster_name":      "evalhub-dev",
				"airgap":            "false",
				"selected_adapters": "inspect",
			}),
			wantError: true,
		},
		{
			name: "ownership annotation must match cluster",
			configMap: func() *corev1.ConfigMap {
				cm := devDeployManifestForTest(namespace, validData)
				cm.Annotations[devDeployOwnershipAnnotation] = "another-cluster"
				return cm
			}(),
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var objects []runtime.Object
			if test.configMap != nil {
				objects = append(objects, test.configMap)
			}
			kube := fake.NewSimpleClientset(objects...)
			got, err := discoverDeploymentManifest(context.Background(), kube, namespace)
			if (err != nil) != test.wantError {
				t.Fatalf("discoverDeploymentManifest() error = %v, wantError %t", err, test.wantError)
			}
			if err != nil {
				return
			}
			if got.managed != test.wantApply {
				t.Fatalf("managed = %t, want %t", got.managed, test.wantApply)
			}
			if !test.wantApply {
				return
			}
			if got.clusterName != "evalhub-dev" || !got.airgap {
				t.Fatalf("manifest identity = cluster %q, airgap %t", got.clusterName, got.airgap)
			}
			if len(got.selectedAdapters) != 2 {
				t.Fatalf("selected adapter count = %d, want 2", len(got.selectedAdapters))
			}
			inspect := got.selectedAdapters["inspect"]
			if inspect.image != validData["image.inspect"] || inspect.imageID != validData["image_id.inspect"] ||
				inspect.sourceSHA != validData["source.inspect.revision"] || inspect.sourceDirty != "false" {
				t.Fatalf("Inspect provenance = %+v", inspect)
			}
		})
	}
}

func TestDeploymentManifestAdapterSelection(t *testing.T) {
	managed := deploymentManifest{managed: true, selectedAdapters: map[string]deployedAdapter{
		"inspect": {image: "inspect:local", imageID: "sha256:inspect"},
	}}
	if !managed.adapterSelected("inspect") {
		t.Fatal("selected Inspect image was not recognized")
	}
	if managed.adapterSelected("ruler") {
		t.Fatal("unselected RULER image was recognized as selected")
	}
	if !(deploymentManifest{}).adapterSelected("ruler") {
		t.Fatal("unmanaged cluster should retain provider auto-discovery")
	}
}

func TestSelectedAdapterProviderMappingsCoverSupportedAdapters(t *testing.T) {
	if len(selectedAdapterProviders) != 6 {
		t.Fatalf("selected adapter provider mappings = %d, want 6", len(selectedAdapterProviders))
	}
	seen := make(map[string]struct{}, len(selectedAdapterProviders))
	for _, mapping := range selectedAdapterProviders {
		if !supportedCuratedAdapter(mapping.adapterID) {
			t.Errorf("mapping contains unsupported adapter %q", mapping.adapterID)
		}
		if mapping.providerID == "" {
			t.Errorf("mapping for adapter %q has no provider ID", mapping.adapterID)
		}
		if _, exists := seen[mapping.adapterID]; exists {
			t.Errorf("adapter %q appears more than once in mappings", mapping.adapterID)
		}
		seen[mapping.adapterID] = struct{}{}
	}
}

func TestDeploymentManifestVerifiesProviderImage(t *testing.T) {
	manifest := deploymentManifest{managed: true, selectedAdapters: map[string]deployedAdapter{
		"inspect": {image: "localhost:32769/evalhub/community-inspect:kind-local"},
	}}
	if err := manifest.verifyProviderImage("inspect", "localhost:32769/evalhub/community-inspect:kind-local"); err != nil {
		t.Fatalf("verify selected provider image: %v", err)
	}
	if err := manifest.verifyProviderImage("inspect", "quay.io/evalhub/community-inspect:latest"); err == nil {
		t.Fatal("expected mismatch between packaged and selected image to fail")
	}
	if err := manifest.verifyProviderImage("ruler", "quay.io/evalhub/community-ruler:latest"); err == nil {
		t.Fatal("expected unselected adapter verification to fail")
	}
}

func TestDiscoverManagedDeploymentNamespaces(t *testing.T) {
	kube := fake.NewSimpleClientset(
		devDeployManifestForTest("system", map[string]string{"cluster_name": "evalhub-dev"}),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unmanaged",
				Namespace: "tenant-a",
			},
		},
	)

	namespaces, err := discoverManagedDeploymentNamespaces(context.Background(), kube)
	if err != nil {
		t.Fatalf("discoverManagedDeploymentNamespaces() error = %v", err)
	}
	if len(namespaces) != 1 || namespaces[0] != "system" {
		t.Fatalf("managed namespaces = %v, want [system]", namespaces)
	}
}
