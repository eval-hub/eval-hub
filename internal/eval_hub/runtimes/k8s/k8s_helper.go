package k8s

// Helper wrapper around the Kubernetes clientset.
import (
	"context"
	"fmt"
	"io"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesHelper wraps the Kubernetes client-go client and exposes methods to interact with the cluster.
// Keeping this abstraction in one place allows all call sites to stay unchanged if we switch
// to a different underlying Kubernetes client implementation.
type KubernetesHelper struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
}

var hardwareProfileGVR = schema.GroupVersionResource{
	Group:    hardwareProfileAPIGroup,
	Version:  hardwareProfileAPIVersion,
	Resource: hardwareProfileResource,
}

func loadKubernetesConfig() (*rest.Config, error) {
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
	return config, nil
}

// NewKubernetesHelper builds a Kubernetes client (in-cluster config, then default kubeconfig)
// and returns a KubernetesHelper.
func NewKubernetesHelper() (*KubernetesHelper, error) {
	config, err := loadKubernetesConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &KubernetesHelper{
		clientset:     clientset,
		dynamicClient: dynamicClient,
	}, nil
}

// NewKubernetesHelperWithClientset returns a helper backed by the given clientset.
// It is intended for unit tests that need deterministic Kubernetes API behavior.
func NewKubernetesHelperWithClientset(clientset kubernetes.Interface) *KubernetesHelper {
	return &KubernetesHelper{clientset: clientset}
}

// GetHardwareProfile fetches a HardwareProfile custom resource by name in the given namespace.
func (h *KubernetesHelper) GetHardwareProfile(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name are required")
	}
	if h.dynamicClient == nil {
		return nil, fmt.Errorf("dynamic kubernetes client is not configured")
	}
	return h.dynamicClient.Resource(hardwareProfileGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// DeleteHardwareProfile deletes a HardwareProfile custom resource by name in the given namespace.
func (h *KubernetesHelper) DeleteHardwareProfile(ctx context.Context, namespace, name string) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	if h.dynamicClient == nil {
		return fmt.Errorf("dynamic kubernetes client is not configured")
	}
	return h.dynamicClient.Resource(hardwareProfileGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
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
			cm.Labels = opts.Labels
		}
		if len(opts.Annotations) > 0 {
			cm.Annotations = opts.Annotations
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

// DeleteJob deletes a Job in the given namespace.
func (h *KubernetesHelper) DeleteJob(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	return h.clientset.BatchV1().Jobs(namespace).Delete(ctx, name, opts)
}

// DeleteConfigMap deletes a ConfigMap in the given namespace.
func (h *KubernetesHelper) DeleteConfigMap(ctx context.Context, namespace, name string) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	return h.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListJobs returns Jobs matching the label selector.
func (h *KubernetesHelper) ListJobs(ctx context.Context, namespace, labelSelector string) ([]batchv1.Job, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	list, err := h.clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ListConfigMaps returns ConfigMaps matching the label selector.
func (h *KubernetesHelper) ListConfigMaps(ctx context.Context, namespace, labelSelector string) ([]corev1.ConfigMap, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	list, err := h.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
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

// GetSecret returns the Secret with the given name in namespace.
func (h *KubernetesHelper) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name are required")
	}
	return h.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
}

// CreateSecret creates a Secret in the given namespace.
func (h *KubernetesHelper) CreateSecret(ctx context.Context, namespace string, secret *corev1.Secret) (*corev1.Secret, error) {
	if secret == nil || namespace == "" || secret.Name == "" {
		return nil, fmt.Errorf("secret, namespace, and name are required")
	}
	secret.Namespace = namespace
	return h.clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
}

// DeleteSecret deletes a Secret in the given namespace.
func (h *KubernetesHelper) DeleteSecret(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	return h.clientset.CoreV1().Secrets(namespace).Delete(ctx, name, opts)
}

// ListSecrets returns Secrets matching the label selector.
func (h *KubernetesHelper) ListSecrets(ctx context.Context, namespace, labelSelector string) ([]corev1.Secret, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	list, err := h.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// SetSecretOwner sets a single owner reference on the Secret.
func (h *KubernetesHelper) SetSecretOwner(ctx context.Context, namespace, name string, owner metav1.OwnerReference) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	secret, err := h.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	secret.OwnerReferences = []metav1.OwnerReference{owner}
	_, err = h.clientset.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

// ListPods returns Pods matching the label selector.
func (h *KubernetesHelper) ListPods(ctx context.Context, namespace, labelSelector string) ([]corev1.Pod, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	list, err := h.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// GetPodLogs returns plain-text logs for a pod container.
func (h *KubernetesHelper) GetPodLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	if namespace == "" || podName == "" {
		return "", fmt.Errorf("namespace and pod name are required")
	}
	if opts == nil {
		opts = &corev1.PodLogOptions{}
	}
	req := h.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CreateConfigMapOptions holds optional metadata for CreateConfigMap.
type CreateConfigMapOptions struct {
	Labels      map[string]string
	Annotations map[string]string
}
