package features

import (
	"context"
	"fmt"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// fvtK8sClient talks to the cluster selected by KUBECONFIG (pipeline oc login),
// not the Jenkins agent's in-cluster API credentials.
type fvtK8sClient struct {
	clientset kubernetes.Interface
}

func newFVTK8sClient() (*fvtK8sClient, error) {
	config, err := loadFVTKubeConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}
	return &fvtK8sClient{clientset: clientset}, nil
}

// loadFVTKubeConfig prefers kubeconfig (KUBECONFIG or ~/.kube/config) over in-cluster
// credentials so FVT running on a Jenkins agent reaches the test cluster API.
func loadFVTKubeConfig() (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path := os.Getenv("KUBECONFIG"); path != "" {
		rules.ExplicitPath = path
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err == nil {
		return config, nil
	}

	config, icErr := rest.InClusterConfig()
	if icErr != nil {
		return nil, fmt.Errorf("kubeconfig unavailable (%v) and in-cluster config unavailable (%v)", err, icErr)
	}
	return config, nil
}

func (c *fvtK8sClient) listJobs(ctx context.Context, namespace, labelSelector string) ([]batchv1.Job, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	list, err := c.clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}
