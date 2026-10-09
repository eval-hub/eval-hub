package k8s

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestListHardwareProfilesTenantVisibility(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "platform")
	profile := func(namespace, name, queue string) *unstructured.Unstructured {
		p := testHardwareProfileUnstructured(namespace, name)
		if queue != "" {
			p.Object["spec"].(map[string]any)["scheduling"] = map[string]any{"type": "Queue", "kueue": map[string]any{"localQueueName": queue}}
		}
		return p
	}
	shared := profile("platform", "a-shared", "")
	shared.Object["spec"].(map[string]any)["scheduling"] = map[string]any{"type": "Node", "node": map[string]any{}}
	shared.SetAnnotations(map[string]string{"opendatahub.io/display-name": "Shared CPU", "opendatahub.io/description": "CPU resources"})
	disabled := profile("platform", "disabled", "")
	disabled.SetAnnotations(map[string]string{hardwareProfileDisabledAnnotation: "true"})
	client := fake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{
		hardwareProfileGVR: "HardwareProfileList", localQueueGVR: "LocalQueueList",
	}, shared, disabled,
		profile("platform", "b-tenant-a", "queue-a"),
		profile("platform", "c-tenant-b", "queue-b"),
		profile("other-platform", "wrong-namespace", ""),
		testLocalQueue("tenant-a", "queue-a", map[string]any{"type": "Active", "status": "True", "reason": "Ready", "message": "Can admit workloads"}),
		testLocalQueue("tenant-b", "queue-b", map[string]any{"type": "Active", "status": "False", "reason": "Paused", "message": "Queue paused"}),
	)
	runtime := &K8sRuntime{helper: &KubernetesHelper{dynamicClient: client}}
	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-empty"} {
		t.Run(tenant, func(t *testing.T) {
			got, err := runtime.ListHardwareProfiles(context.Background(), tenant)
			if err != nil {
				t.Fatal(err)
			}
			identifiers := []api.HardwareProfileIdentifier{{Identifier: "cpu", ResourceType: api.HardwareProfileResourceCPU, DefaultCount: "4"}}
			want := []api.HardwareProfileInfo{{SchedulingType: api.HardwareProfileSchedulingNode, Identifiers: identifiers, Name: "a-shared", DisplayName: "Shared CPU", Description: "CPU resources"}}
			if tenant == "tenant-a" {
				want = append(want, api.HardwareProfileInfo{Name: "b-tenant-a", QueueName: "queue-a", SchedulingType: api.HardwareProfileSchedulingQueue, Identifiers: identifiers, QueueAvailability: &api.QueueAvailability{Status: api.QueueAvailabilityActive, Reason: "Ready", Message: "Can admit workloads"}})
			}
			if tenant == "tenant-b" {
				want = append(want, api.HardwareProfileInfo{Name: "c-tenant-b", QueueName: "queue-b", SchedulingType: api.HardwareProfileSchedulingQueue, Identifiers: identifiers, QueueAvailability: &api.QueueAvailability{Status: api.QueueAvailabilityInactive, Reason: "Paused", Message: "Queue paused"}})
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("profiles = %#v, want %#v", got, want)
			}
		})
	}
}

func TestListHardwareProfilesErrors(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "platform")
	client := fake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{hardwareProfileGVR: "HardwareProfileList"})
	runtime := &K8sRuntime{helper: &KubernetesHelper{dynamicClient: client}}
	if _, err := runtime.ListHardwareProfiles(context.Background(), ""); err == nil {
		t.Fatal("expected missing tenant error")
	}
	t.Run("missing platform namespace", func(t *testing.T) {
		t.Setenv(hardwareProfilesNamespaceEnv, "")
		if _, err := runtime.ListHardwareProfiles(context.Background(), "tenant-a"); err == nil {
			t.Fatal("expected missing platform namespace error")
		}
	})
	t.Run("missing client", func(t *testing.T) {
		r := &K8sRuntime{helper: &KubernetesHelper{}}
		if _, err := r.ListHardwareProfiles(context.Background(), "tenant-a"); err == nil {
			t.Fatal("expected missing client error")
		}
	})
	t.Run("list fails", func(t *testing.T) {
		expected := errors.New("forbidden")
		client.PrependReactor("list", "hardwareprofiles", func(k8stesting.Action) (bool, k8sruntime.Object, error) { return true, nil, expected })
		if _, err := runtime.ListHardwareProfiles(context.Background(), "tenant-a"); !errors.Is(err, expected) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestListHardwareProfilesEdgeCases(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "platform")
	for _, tc := range []struct {
		name       string
		spec       map[string]any
		queueError bool
		wantError  bool
	}{
		{name: "malformed profile", spec: map[string]any{"scheduling": "invalid"}, wantError: true},
		{name: "queue without name", spec: map[string]any{"scheduling": map[string]any{"type": "Queue"}}},
		{name: "queue lookup fails", spec: map[string]any{"scheduling": map[string]any{"type": "Queue", "kueue": map[string]any{"localQueueName": "gpu"}}}, queueError: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testHardwareProfileUnstructured("platform", "profile")
			p.Object["spec"] = tc.spec
			client := fake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{
				hardwareProfileGVR: "HardwareProfileList", localQueueGVR: "LocalQueueList",
			}, p)
			expected := errors.New("queue lookup forbidden")
			if tc.queueError {
				client.PrependReactor("list", "localqueues", func(k8stesting.Action) (bool, k8sruntime.Object, error) { return true, nil, expected })
			}
			r := &K8sRuntime{helper: &KubernetesHelper{dynamicClient: client}}
			got, err := r.ListHardwareProfiles(context.Background(), "tenant-a")
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %t", err, tc.wantError)
			}
			if tc.queueError && !errors.Is(err, expected) {
				t.Fatalf("queue error not preserved: %v", err)
			}
			if !tc.wantError && (got == nil || len(got) != 0) {
				t.Fatalf("profiles = %#v, want empty array", got)
			}
		})
	}
	if _, err := (&KubernetesHelper{}).ListHardwareProfiles(context.Background(), ""); err == nil {
		t.Fatal("expected missing namespace error")
	}
}

func TestListHardwareProfilesIdentifiersAndUnknownAvailability(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "platform")
	p := testHardwareProfileUnstructured("platform", "profile")
	p.Object["spec"] = map[string]any{
		"identifiers": []any{
			map[string]any{"identifier": "cpu", "displayName": "CPU", "resourceType": "CPU", "minCount": int64(0), "defaultCount": int64(4), "maxCount": int64(16)},
			map[string]any{"identifier": "memory", "displayName": "Memory", "resourceType": "Memory", "minCount": "1Gi", "defaultCount": "4Gi", "maxCount": "16Gi"},
			map[string]any{"identifier": "nvidia.com/gpu", "displayName": "GPU", "resourceType": "Accelerator", "defaultCount": int64(2)},
			map[string]any{"identifier": "ephemeral-storage", "displayName": "Storage", "defaultCount": "10Gi"},
		},
		"scheduling": map[string]any{"type": "Queue", "kueue": map[string]any{"localQueueName": "gpu", "priorityClass": "high-priority"}},
	}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{
		hardwareProfileGVR: "HardwareProfileList", localQueueGVR: "LocalQueueList",
	}, p, testLocalQueue("tenant-a", "gpu", nil))
	r := &K8sRuntime{helper: &KubernetesHelper{dynamicClient: client}}
	got, err := r.ListHardwareProfiles(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	want := []api.HardwareProfileInfo{{
		Name: "profile", QueueName: "gpu", PriorityClassName: "high-priority", SchedulingType: api.HardwareProfileSchedulingQueue,
		QueueAvailability: &api.QueueAvailability{Status: api.QueueAvailabilityUnknown},
		Identifiers: []api.HardwareProfileIdentifier{
			{Identifier: "cpu", DisplayName: "CPU", ResourceType: api.HardwareProfileResourceCPU, MinCount: "0", DefaultCount: "4", MaxCount: "16"},
			{Identifier: "memory", DisplayName: "Memory", ResourceType: api.HardwareProfileResourceMemory, MinCount: "1Gi", DefaultCount: "4Gi", MaxCount: "16Gi"},
			{Identifier: "nvidia.com/gpu", DisplayName: "GPU", ResourceType: api.HardwareProfileResourceAccelerator, DefaultCount: "2"},
			{Identifier: "ephemeral-storage", DisplayName: "Storage", DefaultCount: "10Gi"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profiles = %#v, want %#v", got, want)
	}
}
