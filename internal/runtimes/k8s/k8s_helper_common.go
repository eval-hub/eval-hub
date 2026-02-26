package k8s

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubernetesHelperInterface defines operations for creating and managing Kubernetes resources
// (ConfigMaps, Jobs). *KubernetesHelper implements it; MockKubernetesHelper is for local/testing.
type KubernetesHelperInterface interface {
	CreateConfigMap(ctx context.Context, namespace, name string, data map[string]string, opts *CreateConfigMapOptions) (*corev1.ConfigMap, error)
	CreateJob(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error)
	DeleteJob(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error
	DeleteConfigMap(ctx context.Context, namespace, name string) error
	SetConfigMapOwner(ctx context.Context, namespace, name string, owner metav1.OwnerReference) error
	ListJobs(ctx context.Context, namespace, labelSelector string) ([]batchv1.Job, error)
	ListConfigMaps(ctx context.Context, namespace, labelSelector string) ([]corev1.ConfigMap, error)
}

// mockTemplateLabel is stripped by the real helper before creating jobs in the cluster.
const mockTemplateLabel = "__MOCK_TEMPLATE"

// CreateConfigMapOptions holds optional metadata for CreateConfigMap.
type CreateConfigMapOptions struct {
	Labels      map[string]string
	Annotations map[string]string
}
