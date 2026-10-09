package k8s

import (
	"context"
	"encoding/json"
	"github.com/eval-hub/eval-hub/pkg/api"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

// Profiles arrive as unstructured Kubernetes JSON. Malformed field types must
// produce errors rather than panics, and parsing must not mutate the resource.
func FuzzParseHardwareProfileResources(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"spec":null}`, `{"spec":{"identifiers":"bad"}}`,
		`{"spec":{"identifiers":[null,{}, {"identifier":"nvidia.com/gpu","defaultCount":"invalid"}]}}`,
		`{"spec":{"identifiers":[{"identifier":"cpu","defaultCount":4,"maxCount":"8"}]}}`,
		`{"spec":{"scheduling":{"type":"Node","node":{"tolerations":[{"key":"gpu","tolerationSeconds":60}],"nodeSelector":{"gpu":"true"}}}}}`,
		`{"spec":{"scheduling":{"type":"Queue","kueue":{"localQueueName":" gpu ","priorityClass":"None"}}}}`,
		`{"spec":{"scheduling":{"type":"Node","node":{"tolerations":false}}}}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var object map[string]any
		if json.Unmarshal([]byte(input), &object) != nil || object == nil {
			return
		}
		profile := &unstructured.Unstructured{Object: object}
		original := profile.DeepCopy()
		parsed, err := parseHardwareProfileResources(profile)
		if err == nil && parsed == nil {
			t.Fatal("successful parse returned nil")
		}
		if !reflect.DeepEqual(profile.Object, original.Object) {
			t.Fatal("parsing mutated the resource")
		}
	})
}

// Queue names and disabled annotations must never let a profile backed only by
// another tenant's queue appear in the caller's discovery response.
func FuzzHardwareProfileTenantVisibility(f *testing.F) {
	f.Setenv(hardwareProfilesNamespaceEnv, "platform")
	f.Add("gpu", "gpu", "false", true)
	f.Add("gpu", "other", "false", true)
	f.Add(" gpu ", "gpu", " TrUe ", true)
	f.Add("", "", "false", true)
	f.Add("gpu", "gpu", "false", false)
	f.Fuzz(func(t *testing.T, profileQueue, tenantQueue, disabled string, hasTenantQueue bool) {
		p := testHardwareProfileUnstructured("platform", "profile")
		p.SetAnnotations(map[string]string{hardwareProfileDisabledAnnotation: disabled})
		p.Object["spec"] = map[string]any{"scheduling": map[string]any{"type": "Queue", "kueue": map[string]any{"localQueueName": profileQueue}}}
		objects := []k8sruntime.Object{p, testLocalQueue("other-tenant", strings.TrimSpace(profileQueue), nil)}
		if hasTenantQueue {
			objects = append(objects, testLocalQueue("tenant-a", tenantQueue, nil))
		}
		client := fake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{
			hardwareProfileGVR: "HardwareProfileList", localQueueGVR: "LocalQueueList",
		}, objects...)
		r := &K8sRuntime{helper: &KubernetesHelper{dynamicClient: client}}
		got, err := r.ListHardwareProfiles(context.Background(), "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		queue := strings.TrimSpace(profileQueue)
		visible := !strings.EqualFold(strings.TrimSpace(disabled), "true") && queue != "" && hasTenantQueue && queue == tenantQueue
		want := 0
		if visible {
			want = 1
		}
		if len(got) != want {
			t.Fatalf("profiles = %#v, want %d; profile queue %q, tenant queue %q, disabled %q", got, want, profileQueue, tenantQueue, disabled)
		}
		if want == 1 && (got[0].Name != "profile" || got[0].QueueName != queue || got[0].SchedulingType != api.HardwareProfileSchedulingQueue || got[0].QueueAvailability == nil || got[0].QueueAvailability.Status != api.QueueAvailabilityUnknown || got[0].Identifiers == nil) {
			t.Fatalf("unexpected profile: %#v", got)
		}
	})
}
