package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateConfigMapRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if _, err := helper.CreateConfigMap(context.Background(), "", "name", map[string]string{}, nil); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
	if _, err := helper.CreateConfigMap(context.Background(), "default", "", map[string]string{}, nil); err == nil {
		t.Fatalf("expected error for missing name")
	}
}

func TestCreateJobRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if _, err := helper.CreateJob(context.Background(), nil); err == nil {
		t.Fatalf("expected error for missing job")
	}
}

func TestDeleteConfigMapRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.DeleteConfigMap(context.Background(), "", "name"); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
	if err := helper.DeleteConfigMap(context.Background(), "default", ""); err == nil {
		t.Fatalf("expected error for missing name")
	}
}

func TestSetConfigMapOwnerRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.SetConfigMapOwner(context.Background(), "", "name", emptyOwnerRef()); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
	if err := helper.SetConfigMapOwner(context.Background(), "default", "", emptyOwnerRef()); err == nil {
		t.Fatalf("expected error for missing name")
	}
}

func emptyOwnerRef() metav1.OwnerReference {
	return metav1.OwnerReference{}
}

func TestSetConfigMapOwnerUpdatesOwnerReferences(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	helper := &KubernetesHelper{clientset: clientset}
	_, err := clientset.CoreV1().ConfigMaps("default").Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-spec",
			Namespace: "default",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}
	owner := metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       "job-1",
		UID:        "uid-1",
	}
	if err := helper.SetConfigMapOwner(context.Background(), "default", "job-spec", owner); err != nil {
		t.Fatalf("SetConfigMapOwner returned error: %v", err)
	}
	updated, err := clientset.CoreV1().ConfigMaps("default").Get(context.Background(), "job-spec", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get configmap: %v", err)
	}
	if len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].Name != "job-1" {
		t.Fatalf("expected owner reference to be set")
	}
}
