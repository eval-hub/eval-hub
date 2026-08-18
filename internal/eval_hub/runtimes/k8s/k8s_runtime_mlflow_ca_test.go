package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestK8sRuntimeResolveMLFlowCABundleConfigMap(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	t.Run("empty when cfg incomplete", func(t *testing.T) {
		r := &K8sRuntime{helper: &KubernetesHelper{clientset: fake.NewSimpleClientset()}}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, nil, logger); got != "" {
			t.Fatalf("nil cfg: got %q", got)
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, &jobConfig{mlflowTrackingURI: "https://mlflow"}, logger); got != "" {
			t.Fatalf("missing instance: got %q", got)
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, &jobConfig{evalHubInstanceName: "my-evalhub"}, logger); got != "" {
			t.Fatalf("missing tracking URI: got %q", got)
		}
	})

	t.Run("empty when ConfigMap not found", func(t *testing.T) {
		r := &K8sRuntime{helper: &KubernetesHelper{clientset: fake.NewSimpleClientset()}}
		cfg := &jobConfig{
			evalHubInstanceName: "my-evalhub",
			mlflowTrackingURI:   "https://mlflow.example",
			namespace:           "team-a",
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, cfg, logger); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("returns ConfigMap name when present", func(t *testing.T) {
		cmName := mlflowCABundleConfigMapName("my-evalhub")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: "team-a"},
			Data:       map[string]string{mlflowCABundleFile: "PEM"},
		}
		r := &K8sRuntime{helper: &KubernetesHelper{clientset: fake.NewSimpleClientset(cm)}}
		cfg := &jobConfig{
			evalHubInstanceName: "my-evalhub",
			mlflowTrackingURI:   "https://mlflow.example",
			namespace:           "team-a",
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, cfg, logger); got != cmName {
			t.Fatalf("got %q, want %q", got, cmName)
		}
	})

	t.Run("empty when bundle key missing", func(t *testing.T) {
		cmName := mlflowCABundleConfigMapName("my-evalhub")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: "team-a"},
			Data:       map[string]string{"other.crt": "PEM"},
		}
		r := &K8sRuntime{helper: &KubernetesHelper{clientset: fake.NewSimpleClientset(cm)}}
		cfg := &jobConfig{
			evalHubInstanceName: "my-evalhub",
			mlflowTrackingURI:   "https://mlflow.example",
			namespace:           "team-a",
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, cfg, logger); got != "" {
			t.Fatalf("got %q, want empty when %s missing", got, mlflowCABundleFile)
		}
	})

	t.Run("empty when bundle key empty", func(t *testing.T) {
		cmName := mlflowCABundleConfigMapName("my-evalhub")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: "team-a"},
			Data:       map[string]string{mlflowCABundleFile: "   "},
		}
		r := &K8sRuntime{helper: &KubernetesHelper{clientset: fake.NewSimpleClientset(cm)}}
		cfg := &jobConfig{
			evalHubInstanceName: "my-evalhub",
			mlflowTrackingURI:   "https://mlflow.example",
			namespace:           "team-a",
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, cfg, logger); got != "" {
			t.Fatalf("got %q, want empty when %s is blank", got, mlflowCABundleFile)
		}
	})

	t.Run("empty on non-NotFound Get error", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		clientset.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, kruntime.Object, error) {
			return true, nil, fmt.Errorf("apiserver unavailable")
		})
		r := &K8sRuntime{helper: &KubernetesHelper{clientset: clientset}}
		cfg := &jobConfig{
			evalHubInstanceName: "my-evalhub",
			mlflowTrackingURI:   "https://mlflow.example",
			namespace:           "team-a",
		}
		if got := r.resolveMLFlowCABundleConfigMap(ctx, cfg, logger); got != "" {
			t.Fatalf("got %q, want empty on Get error", got)
		}
	})
}
