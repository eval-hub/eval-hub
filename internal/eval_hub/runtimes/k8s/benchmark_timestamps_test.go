package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestBenchmarkCreationRecordsKubernetesTimestamp(t *testing.T) {
	for _, scenario := range []string{"success", "lost response", "create failure", "storage failure"} {
		t.Run(scenario, func(t *testing.T) {
			created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			clientset := fake.NewClientset()
			clientset.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, kruntime.Object, error) {
				job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
				job.CreationTimestamp = metav1.NewTime(created)
				if scenario == "create failure" {
					return true, nil, errors.New("create rejected")
				}
				if scenario == "lost response" {
					job.UID = "persisted-job"
					if err := clientset.Tracker().Create(batchv1.SchemeGroupVersion.WithResource("jobs"), job.DeepCopy(), job.Namespace); err != nil {
						return true, nil, err
					}
					return true, nil, errors.New("create response lost")
				}
				return false, nil, nil
			})
			rt := newNetworkPolicyTestRuntime(clientset)
			evaluation := sampleEvaluation("provider-1")
			// Repeated IDs still have independent creation timestamps by index.
			evaluation.Benchmarks = append(evaluation.Benchmarks, evaluation.Benchmarks[0])
			calls := 0
			storageErr := errors.New("creation metadata storage unavailable")
			storage := &fakeStorage{providerConfigs: sampleProviders("provider-1"),
				creationFn: func(id string, index int, timestamp time.Time) error {
					calls++
					if id != evaluation.Resource.ID || index != 1 || !timestamp.Equal(created) {
						t.Errorf("unexpected creation metadata: %s / %d / %v", id, index, timestamp)
					}
					if scenario == "storage failure" {
						return storageErr
					}
					return nil
				}}
			err := rt.createBenchmarkResources(context.Background(), rt.logger, evaluation, &evaluation.Benchmarks[1], 1, storage)
			if scenario == "create failure" {
				if err == nil || calls != 0 {
					t.Fatalf("failed creation recorded metadata: calls=%d err=%v", calls, err)
				}
				return
			}
			if calls != 1 {
				t.Fatalf("creation metadata calls = %d, want 1", calls)
			}
			if scenario == "storage failure" {
				if !errors.Is(err, storageErr) {
					t.Fatalf("creation storage failure was lost: %v", err)
				}
				policies, listErr := clientset.NetworkingV1().NetworkPolicies("default").List(context.Background(), metav1.ListOptions{})
				if listErr != nil || len(policies.Items) != 1 || policies.Items[0].OwnerReferences[0].Kind != "Job" {
					t.Fatalf("creation storage failure skipped resource ownership: %+v, %v", policies, listErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
