package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetSecretDockerConfigJSON(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "oci-secret", Namespace: "tenant-a"},
		Data: map[string][]byte{
			".dockerconfigjson": []byte(`{"auths":{"https://quay.io":{"username":"u","password":"p"}}}`),
		},
	}
	helper := &KubernetesHelper{clientset: fake.NewClientset(secret)}
	got, err := helper.GetSecret(context.Background(), "tenant-a", "oci-secret")
	if err != nil {
		t.Fatalf("GetSecret() err = %v", err)
	}
	if len(got.Data[".dockerconfigjson"]) == 0 {
		t.Fatal("expected dockerconfigjson data")
	}
}
