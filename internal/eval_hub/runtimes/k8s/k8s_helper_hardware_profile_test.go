package k8s

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func testHardwareProfileUnstructured(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": hardwareProfileAPIGroup + "/" + hardwareProfileAPIVersion,
			"kind":       "HardwareProfile",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"identifiers": []any{
					map[string]any{
						"identifier":   "cpu",
						"resourceType": "CPU",
						"defaultCount": int64(4),
					},
				},
			},
		},
	}
}

func TestGetHardwareProfileValidation(t *testing.T) {
	t.Parallel()
	helper := &KubernetesHelper{dynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme())}

	if _, err := helper.GetHardwareProfile(context.Background(), "", "profile"); err == nil {
		t.Fatal("expected error for empty namespace")
	}
	if _, err := helper.GetHardwareProfile(context.Background(), "ns", ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestGetHardwareProfileRequiresDynamicClient(t *testing.T) {
	t.Parallel()
	helper := &KubernetesHelper{}
	if _, err := helper.GetHardwareProfile(context.Background(), "ns", "profile"); err == nil {
		t.Fatal("expected error when dynamic client is nil")
	}
}

func TestGetHardwareProfileSuccess(t *testing.T) {
	t.Parallel()
	const namespace = "tenant-a"
	const name = "cpu-profile"
	profile := testHardwareProfileUnstructured(namespace, name)
	helper := &KubernetesHelper{
		dynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme(), profile),
	}

	got, err := helper.GetHardwareProfile(context.Background(), namespace, name)
	if err != nil {
		t.Fatalf("GetHardwareProfile returned error: %v", err)
	}
	if got.GetName() != name {
		t.Fatalf("name = %q, want %q", got.GetName(), name)
	}
}

func TestGetHardwareProfileNotFoundUnit(t *testing.T) {
	t.Parallel()
	helper := &KubernetesHelper{
		dynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme()),
	}
	_, err := helper.GetHardwareProfile(context.Background(), "tenant-a", "missing-profile")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected not found error, got: %v", err)
	}
}

func TestDeleteHardwareProfileValidation(t *testing.T) {
	t.Parallel()
	helper := &KubernetesHelper{dynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme())}

	if err := helper.DeleteHardwareProfile(context.Background(), "", "profile"); err == nil {
		t.Fatal("expected error for empty namespace")
	}
	if err := helper.DeleteHardwareProfile(context.Background(), "ns", ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestDeleteHardwareProfileRequiresDynamicClient(t *testing.T) {
	t.Parallel()
	helper := &KubernetesHelper{}
	if err := helper.DeleteHardwareProfile(context.Background(), "ns", "profile"); err == nil {
		t.Fatal("expected error when dynamic client is nil")
	}
}

func TestDeleteHardwareProfileSuccess(t *testing.T) {
	t.Parallel()
	const namespace = "tenant-a"
	const name = "cpu-profile"
	profile := testHardwareProfileUnstructured(namespace, name)
	helper := &KubernetesHelper{
		dynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme(), profile),
	}

	if err := helper.DeleteHardwareProfile(context.Background(), namespace, name); err != nil {
		t.Fatalf("DeleteHardwareProfile returned error: %v", err)
	}
	if _, err := helper.GetHardwareProfile(context.Background(), namespace, name); !apierrors.IsNotFound(err) {
		t.Fatalf("expected profile to be deleted, got: %v", err)
	}
}
