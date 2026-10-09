package k8s

import (
	"context"
	"errors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func testLocalQueue(namespace, name string, activeCondition map[string]any) *unstructured.Unstructured {
	queue := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": localQueueGVR.Group + "/" + localQueueGVR.Version,
		"kind":       "LocalQueue",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}}
	if activeCondition != nil {
		conditions := []any{activeCondition}
		if err := unstructured.SetNestedSlice(queue.Object, conditions, "status", "conditions"); err != nil {
			panic(err)
		}
	}
	return queue
}

func TestListLocalQueues(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleDynamicClient(k8sruntime.NewScheme(),
		testLocalQueue("tenant-a", "z-inactive", map[string]any{
			"type": "Active", "status": "False", "reason": "ClusterQueueIsInactive", "message": "queue is paused",
		}),
		testLocalQueue("tenant-a", "a-active", map[string]any{
			"type": "Active", "status": "True", "reason": "Ready", "message": "Can admit new workloads",
		}),
		testLocalQueue("tenant-a", "m-unknown", nil),
		testLocalQueue("tenant-b", "other-tenant", map[string]any{"type": "Active", "status": "True"}),
	)
	helper := &KubernetesHelper{dynamicClient: client}

	queues, err := helper.ListLocalQueues(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("ListLocalQueues returned error: %v", err)
	}
	if len(queues) != 3 {
		t.Fatalf("got %d queues, want 3: %#v", len(queues), queues)
	}
	if queues[0].Name != "a-active" || !queues[0].Active || queues[0].Reason != "Ready" || queues[0].Message != "Can admit new workloads" {
		t.Fatalf("active queue = %#v", queues[0])
	}
	if queues[1].Name != "m-unknown" || queues[1].Active || queues[1].Reason != "" || queues[1].Message != "" {
		t.Fatalf("queue without Active condition = %#v", queues[1])
	}
	if queues[2].Name != "z-inactive" || queues[2].Active || queues[2].Reason != "ClusterQueueIsInactive" || queues[2].Message != "queue is paused" {
		t.Fatalf("inactive queue = %#v", queues[2])
	}
}

func TestListLocalQueuesErrors(t *testing.T) {
	t.Parallel()

	t.Run("requires a namespace", func(t *testing.T) {
		helper := &KubernetesHelper{dynamicClient: fake.NewSimpleDynamicClient(k8sruntime.NewScheme())}
		if _, err := helper.ListLocalQueues(context.Background(), ""); err == nil {
			t.Fatal("expected namespace validation error")
		}
	})

	t.Run("requires a dynamic client", func(t *testing.T) {
		if _, err := (&KubernetesHelper{}).ListLocalQueues(context.Background(), "tenant-a"); err == nil {
			t.Fatal("expected dynamic client validation error")
		}
	})

	t.Run("returns Kubernetes list errors", func(t *testing.T) {
		client := fake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{
			localQueueGVR: "LocalQueueList",
		})
		client.PrependReactor("list", "localqueues", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("forbidden")
		})
		helper := &KubernetesHelper{dynamicClient: client}
		if _, err := helper.ListLocalQueues(context.Background(), "tenant-a"); err == nil {
			t.Fatal("expected Kubernetes list error")
		}
	})
}

func TestListLocalQueuesAvailabilityStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		condition map[string]any
		want      api.QueueAvailabilityStatus
	}{
		{name: "active", condition: map[string]any{"type": "Active", "status": "True"}, want: api.QueueAvailabilityActive},
		{name: "inactive", condition: map[string]any{"type": "Active", "status": "False"}, want: api.QueueAvailabilityInactive},
		{name: "unknown", condition: map[string]any{"type": "Active", "status": "Unknown"}, want: api.QueueAvailabilityUnknown},
		{name: "missing condition", want: api.QueueAvailabilityUnknown},
		{name: "missing status", condition: map[string]any{"type": "Active"}, want: api.QueueAvailabilityUnknown},
		{name: "invalid status", condition: map[string]any{"type": "Active", "status": "invalid"}, want: api.QueueAvailabilityUnknown},
		{name: "unrelated condition", condition: map[string]any{"type": "Ready", "status": "True"}, want: api.QueueAvailabilityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(k8sruntime.NewScheme(), testLocalQueue("tenant-a", "gpu", tc.condition))
			got, err := (&KubernetesHelper{dynamicClient: client}).ListLocalQueues(context.Background(), "tenant-a")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("queues = %#v, want one", got)
			}
			if got[0].Status != tc.want || got[0].Active != (tc.want == api.QueueAvailabilityActive) {
				t.Fatalf("queue = %#v, want status %s", got[0], tc.want)
			}
		})
	}
}
