package k8s

// Helper wrapper around the Kubernetes clientset.
import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesHelper wraps the Kubernetes client-go client and exposes methods to interact with the cluster.
// Keeping this abstraction in one place allows all call sites to stay unchanged if we switch
// to a different underlying Kubernetes client implementation.
type KubernetesHelper struct {
	clientset kubernetes.Interface
}

// NewKubernetesHelper builds a Kubernetes client (in-cluster config, then default kubeconfig)
// and returns a KubernetesHelper. Call this when LocalMode is false.
func NewKubernetesHelper() (*KubernetesHelper, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules,
			configOverrides,
		).ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &KubernetesHelper{
		clientset: clientset,
	}, nil
}

// CreateConfigMap creates a ConfigMap in the given namespace.
// name is the ConfigMap name; data is the key-value map for ConfigMap.Data.
// opts may be nil; use it to set labels and annotations.
func (h *KubernetesHelper) CreateConfigMap(
	ctx context.Context,
	namespace, name string,
	data map[string]string,
	opts *CreateConfigMapOptions,
) (*corev1.ConfigMap, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name are required")
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: data,
	}
	if opts != nil {
		if len(opts.Labels) > 0 {
			cm.ObjectMeta.Labels = opts.Labels
		}
		if len(opts.Annotations) > 0 {
			cm.ObjectMeta.Annotations = opts.Annotations
		}
	}
	return h.clientset.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
}

// CreateJob creates a Job in the given namespace.
func (h *KubernetesHelper) CreateJob(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	if job == nil || job.Namespace == "" || job.Name == "" {
		return nil, fmt.Errorf("job, namespace, and name are required")
	}
	return h.clientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
}

// DeleteConfigMap deletes a ConfigMap in the given namespace.
func (h *KubernetesHelper) DeleteConfigMap(ctx context.Context, namespace, name string) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	return h.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// SetConfigMapOwner sets a single owner reference on the ConfigMap.
func (h *KubernetesHelper) SetConfigMapOwner(ctx context.Context, namespace, name string, owner metav1.OwnerReference) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	cm, err := h.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	cm.OwnerReferences = []metav1.OwnerReference{owner}
	_, err = h.clientset.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// CreateConfigMapOptions holds optional metadata for CreateConfigMap.
type CreateConfigMapOptions struct {
	Labels      map[string]string
	Annotations map[string]string
}
