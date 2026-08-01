package features

import (
	"context"
	"fmt"
	"os"
	"time"

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
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err == nil {
		return config, nil
	}
	if kubeconfigPath != "" {
		return nil, fmt.Errorf("load kubeconfig from KUBECONFIG %q: %w", kubeconfigPath, err)
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

// getJobNameForEvalJob returns the name of the Kubernetes Job backing the given eval-hub job ID.
func (c *fvtK8sClient) getJobNameForEvalJob(ctx context.Context, namespace, evalJobID string) (string, error) {
	labelSelector := fmt.Sprintf("job_id=%s", evalJobID)
	jobs, err := c.listJobs(ctx, namespace, labelSelector)
	if err != nil {
		return "", fmt.Errorf("list jobs for eval job %s: %w", evalJobID, err)
	}
	if len(jobs) == 0 {
		return "", fmt.Errorf("no Kubernetes Job found for eval job %s in namespace %s", evalJobID, namespace)
	}
	return jobs[0].Name, nil
}

// getJobPhaseLabel returns the value of the trustyai.opendatahub.io/evaluation-phase label
// on the Kubernetes Job backing the given eval-hub job ID, or "" if the label is absent.
func (c *fvtK8sClient) getJobPhaseLabel(ctx context.Context, namespace, evalJobID string) (string, error) {
	labelSelector := fmt.Sprintf("job_id=%s", evalJobID)
	jobs, err := c.listJobs(ctx, namespace, labelSelector)
	if err != nil {
		return "", fmt.Errorf("list jobs for eval job %s: %w", evalJobID, err)
	}
	if len(jobs) == 0 {
		return "", fmt.Errorf("no Kubernetes Job found for eval job %s in namespace %s", evalJobID, namespace)
	}
	return jobs[0].Labels["trustyai.opendatahub.io/evaluation-phase"], nil
}

// waitForEventOnJob polls the Kubernetes Events API until an event with the given reason is found
// on the Job named jobName (involvedObject.kind=Job). It returns on the first match or when ctx
// is cancelled.
func (c *fvtK8sClient) waitForEventOnJob(ctx context.Context, namespace, jobName, reason string) error {
	if namespace == "" || jobName == "" || reason == "" {
		return fmt.Errorf("namespace, jobName and reason are required")
	}
	fieldSelector := fmt.Sprintf(
		"involvedObject.name=%s,involvedObject.kind=Job,involvedObject.namespace=%s,reason=%s",
		jobName, namespace, reason,
	)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Kubernetes Event reason=%s on Job %s/%s", reason, namespace, jobName)
		case <-ticker.C:
			list, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
				FieldSelector: fieldSelector,
			})
			if err != nil {
				continue
			}
			if len(list.Items) > 0 {
				return nil
			}
		}
	}
}
