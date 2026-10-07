package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestBuildJobNetworkPolicyDeniesIngressForOneExecution(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "evaluation-1",
		resourceGUID:   "6ecb6a09-1e18-4c13-899b-7fed91920fdd",
		namespace:      "tenant-a",
		providerID:     "provider-1",
		benchmarkID:    "arc-easy",
		benchmarkIndex: 0,
	}

	policy, err := buildJobNetworkPolicy(cfg)
	if err != nil {
		t.Fatalf("buildJobNetworkPolicy: %v", err)
	}
	if policy.Namespace != cfg.namespace {
		t.Fatalf("namespace = %q, want %q", policy.Namespace, cfg.namespace)
	}
	if policy.Name == "" || len(policy.Name) > maxK8sNameLength {
		t.Fatalf("invalid policy name %q", policy.Name)
	}
	if got := policy.Spec.PodSelector.MatchLabels[labelExecutionIDKey]; got != cfg.resourceGUID {
		t.Fatalf("execution selector = %q, want %q", got, cfg.resourceGUID)
	}
	if got := policy.Spec.PodSelector.MatchLabels[labelAppKey]; got != labelAppValue {
		t.Fatalf("app selector = %q, want %q", got, labelAppValue)
	}
	if got := policy.Spec.PodSelector.MatchLabels[labelComponentKey]; got != labelComponentValue {
		t.Fatalf("component selector = %q, want %q", got, labelComponentValue)
	}
	if len(policy.Spec.PodSelector.MatchLabels) != 3 {
		t.Fatalf("selector includes unexpected labels: %#v", policy.Spec.PodSelector.MatchLabels)
	}
	if len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("policy types = %#v, want ingress only", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Ingress) != 0 {
		t.Fatalf("expected no ingress allow rules, got %#v", policy.Spec.Ingress)
	}
	if len(policy.Spec.Egress) != 0 {
		t.Fatalf("this policy must not author egress rules, got %#v", policy.Spec.Egress)
	}
	if got := policy.Annotations[annotationJobNetworkPolicyEgressKey]; got != annotationJobNetworkPolicyEgressValue {
		t.Fatalf("egress annotation = %q, want %q", got, annotationJobNetworkPolicyEgressValue)
	}
	if got := jobLabels(cfg)[labelExecutionIDKey]; got != cfg.resourceGUID {
		t.Fatalf("Job Pod execution label = %q, want %q", got, cfg.resourceGUID)
	}
}

func TestBuildJobNetworkPolicyNamesAreUniquePerExecution(t *testing.T) {
	first, err := buildJobNetworkPolicy(&jobConfig{jobID: "same-job", resourceGUID: "guid-one", namespace: "tenant-a"})
	if err != nil {
		t.Fatalf("build first policy: %v", err)
	}
	second, err := buildJobNetworkPolicy(&jobConfig{jobID: "same-job", resourceGUID: "guid-two", namespace: "tenant-a"})
	if err != nil {
		t.Fatalf("build second policy: %v", err)
	}
	if first.Name == second.Name {
		t.Fatalf("policy names collide: %q", first.Name)
	}
	if first.Spec.PodSelector.MatchLabels[labelExecutionIDKey] == second.Spec.PodSelector.MatchLabels[labelExecutionIDKey] {
		t.Fatal("execution selectors collide")
	}
}

func TestBuildJobNetworkPolicyRequiresNamespaceAndExecutionIdentity(t *testing.T) {
	tests := []struct {
		name string
		cfg  *jobConfig
	}{
		{name: "nil config"},
		{name: "missing namespace", cfg: &jobConfig{resourceGUID: "guid"}},
		{name: "missing identity", cfg: &jobConfig{namespace: "tenant-a"}},
		{name: "invalid identity", cfg: &jobConfig{namespace: "tenant-a", resourceGUID: "---"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildJobNetworkPolicy(tt.cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCreateBenchmarkResourcesCreatesNetworkPolicyBeforeJob(t *testing.T) {
	clientset := fake.NewClientset()
	evaluation := sampleEvaluation("provider-1")
	runtime := newNetworkPolicyTestRuntime(clientset)
	storage := &fakeStorage{providerConfigs: sampleProviders("provider-1")}

	if err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage); err != nil {
		t.Fatalf("createBenchmarkResources: %v", err)
	}

	policyCreate, jobCreate := -1, -1
	for i, action := range clientset.Actions() {
		if action.GetVerb() != "create" {
			continue
		}
		switch action.GetResource().Resource {
		case "networkpolicies":
			if policyCreate == -1 {
				policyCreate = i
			}
		case "jobs":
			if jobCreate == -1 {
				jobCreate = i
			}
		}
	}
	if policyCreate < 0 || jobCreate < 0 || policyCreate >= jobCreate {
		t.Fatalf("NetworkPolicy must be created before Job; action indexes policy=%d job=%d", policyCreate, jobCreate)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	policies, err := clientset.NetworkingV1().NetworkPolicies(jobs[0].Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list NetworkPolicies: %v", err)
	}
	if len(policies.Items) != 1 {
		t.Fatalf("got %d policies, want 1", len(policies.Items))
	}
	policy := &policies.Items[0]
	executionID := jobs[0].Spec.Template.Labels[labelExecutionIDKey]
	if executionID == "" || policy.Spec.PodSelector.MatchLabels[labelExecutionIDKey] != executionID {
		t.Fatalf("policy selector %q does not match Job Pod identity %q", policy.Spec.PodSelector.MatchLabels[labelExecutionIDKey], executionID)
	}
	if len(policy.OwnerReferences) != 1 || policy.OwnerReferences[0].Kind != "Job" || policy.OwnerReferences[0].Name != jobs[0].Name {
		t.Fatalf("NetworkPolicy owner references = %#v, want Job %q", policy.OwnerReferences, jobs[0].Name)
	}
}

func TestCreateBenchmarkResourcesKeepsPolicyWhenJobCreateResponseIsAmbiguous(t *testing.T) {
	clientset := fake.NewClientset()
	clientset.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, kruntime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID = "job-uid"
		if err := clientset.Tracker().Create(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, job, job.Namespace); err != nil {
			return true, nil, err
		}
		return true, nil, fmt.Errorf("response lost after create")
	})
	evaluation := sampleEvaluation("provider-1")
	runtime := newNetworkPolicyTestRuntime(clientset)
	storage := &fakeStorage{providerConfigs: sampleProviders("provider-1")}

	if err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage); err != nil {
		t.Fatalf("createBenchmarkResources: %v", err)
	}
	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want matching persisted Job", len(jobs))
	}
	policies, err := clientset.NetworkingV1().NetworkPolicies("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list NetworkPolicies: %v", err)
	}
	if len(policies.Items) != 1 || len(policies.Items[0].OwnerReferences) != 1 || policies.Items[0].OwnerReferences[0].UID != jobs[0].UID {
		t.Fatalf("NetworkPolicy owner references = %#v, want persisted Job UID %q", policies.Items, jobs[0].UID)
	}
}

func TestCreateBenchmarkResourcesDoesNotCreateJobWhenNetworkPolicyCreationFails(t *testing.T) {
	clientset := fake.NewClientset()
	clientset.PrependReactor("create", "networkpolicies", func(ktesting.Action) (bool, kruntime.Object, error) {
		return true, nil, fmt.Errorf("network policy create denied")
	})
	evaluation := sampleEvaluation("provider-1")
	runtime := newNetworkPolicyTestRuntime(clientset)
	storage := &fakeStorage{providerConfigs: sampleProviders("provider-1")}

	if err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage); err == nil {
		t.Fatal("expected NetworkPolicy creation error")
	}
	jobs, err := clientset.BatchV1().Jobs("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("created %d Jobs after NetworkPolicy creation failed", len(jobs.Items))
	}
	configMaps, err := clientset.CoreV1().ConfigMaps("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ConfigMaps: %v", err)
	}
	if len(configMaps.Items) != 0 {
		t.Fatalf("left %d ConfigMaps after NetworkPolicy creation failed", len(configMaps.Items))
	}
}

func TestNetworkPolicyHelperValidatesAndSetsJobOwner(t *testing.T) {
	helper := &KubernetesHelper{clientset: fake.NewClientset()}
	if _, err := helper.CreateNetworkPolicy(context.Background(), nil); err == nil {
		t.Fatal("expected CreateNetworkPolicy to reject nil")
	}
	if err := helper.SetNetworkPolicyOwner(context.Background(), "", "policy", metav1.OwnerReference{}); err == nil {
		t.Fatal("expected SetNetworkPolicyOwner to require namespace")
	}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "job-policy", Namespace: "tenant-a"}}
	if _, err := helper.CreateNetworkPolicy(context.Background(), policy); err != nil {
		t.Fatalf("CreateNetworkPolicy: %v", err)
	}
	owner := metav1.OwnerReference{APIVersion: "batch/v1", Kind: "Job", Name: "eval-job", UID: "job-uid"}
	if err := helper.SetNetworkPolicyOwner(context.Background(), "tenant-a", policy.Name, owner); err != nil {
		t.Fatalf("SetNetworkPolicyOwner: %v", err)
	}
	updated, err := helper.GetNetworkPolicy(context.Background(), "tenant-a", policy.Name)
	if err != nil {
		t.Fatalf("GetNetworkPolicy: %v", err)
	}
	if len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].Name != owner.Name || updated.OwnerReferences[0].UID != owner.UID {
		t.Fatalf("owner references = %#v, want %#v", updated.OwnerReferences, owner)
	}
}

func newNetworkPolicyTestRuntime(clientset *fake.Clientset) *K8sRuntime {
	return &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
		},
	}
}

func TestNetworkPolicyOwnerRetriesConflictWithFreshPolicy(t *testing.T) {
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "default", ResourceVersion: "1"}}
	clientset := fake.NewClientset(policy)
	updates := 0
	clientset.PrependReactor("update", "networkpolicies", func(action ktesting.Action) (bool, kruntime.Object, error) {
		updates++
		if updates == 1 {
			newPolicy := policy.DeepCopy()
			newPolicy.ResourceVersion = "2"
			if err := clientset.Tracker().Update(networkingv1.SchemeGroupVersion.WithResource("networkpolicies"), newPolicy, policy.Namespace); err != nil {
				t.Fatal(err)
			}
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "networkpolicies"}, policy.Name, fmt.Errorf("concurrent update"))
		}
		if got := action.(ktesting.UpdateAction).GetObject().(*networkingv1.NetworkPolicy).ResourceVersion; got != "2" {
			t.Fatalf("retried with resource version %q, want 2", got)
		}
		return false, nil, nil
	})
	helper := &KubernetesHelper{clientset: clientset}
	owner := metav1.OwnerReference{APIVersion: "batch/v1", Kind: "Job", Name: "job", UID: "job-uid"}
	if err := helper.SetNetworkPolicyOwner(context.Background(), policy.Namespace, policy.Name, owner); err != nil {
		t.Fatal(err)
	}
	if updates != 2 {
		t.Fatalf("update attempts = %d, want 2", updates)
	}
	updated, err := helper.GetNetworkPolicy(context.Background(), policy.Namespace, policy.Name)
	if err != nil || len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].UID != owner.UID {
		t.Fatalf("owner transfer failed: policy=%#v err=%v", updated, err)
	}
}

func TestNetworkPolicyOwnerFailureRollsBackAndReportsFailedStatus(t *testing.T) {
	for _, kind := range []string{"not-found", "forbidden", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			evaluation := sampleEvaluation("provider-1")
			evaluation.Model.Auth = &api.ModelAuth{SecretRef: "model-auth"}
			clientset := fake.NewClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "default"},
				Data:       map[string][]byte{"api-key": []byte("test-key")},
			})
			resource := schema.GroupResource{Resource: "networkpolicies"}
			var ownerErr error
			switch kind {
			case "not-found":
				ownerErr = apierrors.NewNotFound(resource, "policy")
			case "forbidden":
				ownerErr = apierrors.NewForbidden(resource, "policy", fmt.Errorf("denied"))
			case "conflict":
				ownerErr = apierrors.NewConflict(resource, "policy", fmt.Errorf("concurrent update"))
			}
			clientset.PrependReactor("update", "networkpolicies", func(ktesting.Action) (bool, kruntime.Object, error) {
				return true, nil, ownerErr
			})
			runtime := newNetworkPolicyTestRuntime(clientset)
			runtime.ctx = context.Background()
			storage := &fakeStorage{providerConfigs: sampleProviders("provider-1"), runStatusChan: make(chan *api.StatusEvent, 1)}
			if err := runtime.RunEvaluationJob(evaluation, evaluation.Benchmarks, storage); err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-storage.runStatusChan:
				if event.BenchmarkStatusEvent.Status != api.StateFailed {
					t.Fatalf("status = %v, want failed", event.BenchmarkStatusEvent.Status)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no failure status event")
			}
			if jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID); len(jobs) != 0 {
				t.Fatalf("left %d Jobs after rollback", len(jobs))
			}
			cms, err := clientset.CoreV1().ConfigMaps("default").List(context.Background(), metav1.ListOptions{})
			if err != nil || len(cms.Items) != 0 {
				t.Fatalf("ConfigMaps after rollback: %#v, err=%v", cms, err)
			}
			secrets, err := clientset.CoreV1().Secrets("default").List(context.Background(), metav1.ListOptions{})
			if err != nil || len(secrets.Items) != 1 || secrets.Items[0].Name != "model-auth" {
				t.Fatalf("expected only source credential Secret after rollback: %#v, err=%v", secrets, err)
			}
			for _, action := range clientset.Actions() {
				if action.GetVerb() == "delete" && action.GetResource().Resource == "jobs" {
					options := action.(ktesting.DeleteAction).GetDeleteOptions()
					if options.PropagationPolicy == nil || *options.PropagationPolicy != metav1.DeletePropagationForeground {
						t.Fatal("rollback must use foreground deletion")
					}
				}
			}
		})
	}
}

func TestNetworkPolicyOwnerFailureRetainsCoverageWhenRollbackFails(t *testing.T) {
	for _, failure := range []string{"delete", "confirm-deletion"} {
		t.Run(failure, func(t *testing.T) {
			clientset := fake.NewClientset()
			ownerErr := fmt.Errorf("owner update denied")
			cleanupErr := fmt.Errorf("rollback unavailable")
			clientset.PrependReactor("update", "networkpolicies", func(ktesting.Action) (bool, kruntime.Object, error) {
				return true, nil, ownerErr
			})
			if failure == "delete" {
				clientset.PrependReactor("delete", "jobs", func(ktesting.Action) (bool, kruntime.Object, error) {
					return true, nil, cleanupErr
				})
			} else {
				// Accept the deletion request but leave the Job present, as the API does
				// while foreground garbage collection is still terminating Pods.
				clientset.PrependReactor("delete", "jobs", func(ktesting.Action) (bool, kruntime.Object, error) {
					return true, nil, nil
				})
				clientset.PrependReactor("get", "jobs", func(ktesting.Action) (bool, kruntime.Object, error) {
					return true, nil, cleanupErr
				})
			}
			evaluation := sampleEvaluation("provider-1")
			runtime := newNetworkPolicyTestRuntime(clientset)
			storage := &fakeStorage{providerConfigs: sampleProviders("provider-1")}
			err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
			if !errors.Is(err, ownerErr) || !errors.Is(err, cleanupErr) {
				t.Fatalf("error = %v, want owner and cleanup failures", err)
			}
			for _, action := range clientset.Actions() {
				if action.GetVerb() == "delete" && (action.GetResource().Resource == "configmaps" || action.GetResource().Resource == "networkpolicies") {
					t.Fatalf("removed policy coverage during incomplete rollback: %#v", action)
				}
			}
		})
	}
}
