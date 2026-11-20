package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
	"sigs.k8s.io/yaml"
)

func proposalEvent(t *testing.T, selectedProvider, selectedID string, rejected []api.CollectionGenerationBenchmarkReason) *api.StatusEvent {
	t.Helper()
	collection := api.CollectionConfig{
		Name:    "Generated collection",
		Domains: []string{"safety"},
		Benchmarks: []api.CollectionBenchmarkConfig{{
			Ref: api.Ref{ID: selectedID}, ProviderID: selectedProvider,
		}},
	}
	serialized, err := yaml.Marshal(collection)
	if err != nil {
		t.Fatalf("marshal proposal collection: %v", err)
	}
	result := api.CollectionGenerationResult{
		Collection: collection,
		YAML:       string(serialized),
		Rationale:  "The selected benchmark provides evidence relevant to the goal.",
		Explanation: api.CollectionGenerationExplanation{SelectedBenchmarks: []api.CollectionGenerationBenchmarkReason{{
			ProviderID: selectedProvider, ID: selectedID, Reason: "It measures the requested behavior.",
		}}, RejectedBenchmarks: rejected},
	}
	return &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
		Status: api.StateCompleted, AdditionalInfo: map[string]any{"collection_generation_result": result},
	}}
}

func createCatalogProvider(t *testing.T, store abstractions.Storage, tenant api.Tenant, owner api.User) {
	t.Helper()
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-a", Tenant: tenant, Owner: owner},
		ProviderConfig: api.ProviderConfig{
			Name: "Provider A", Title: "Provider A",
			Benchmarks: []api.BenchmarkResource{{ID: "bench-a", Name: "Benchmark A"}},
		},
	}
	if err := store.CreateProvider(provider); err != nil {
		t.Fatalf("create caller-visible provider: %v", err)
	}
}

func createRunningGenerationJob(t *testing.T, store abstractions.Storage, tenant api.Tenant, owner api.User) string {
	t.Helper()
	id := common.GUID()
	job, err := generation.NewJob(id, tenant, owner, api.CollectionGenerationRequest{
		EvaluationGoal: "Assess safe tool use",
		Model:          api.ModelRef{URL: "https://model.example/v1", Name: "generator"},
	}, "evalhub:worker", loop.DefaultPresets()[loop.EffortStandard], "", 24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("create generation job: %v", err)
	}
	if err := store.CreateEvaluationJob(job); err != nil {
		t.Fatalf("persist generation job: %v", err)
	}
	if err := store.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
		Status: api.StateRunning,
	}}); err != nil {
		t.Fatalf("mark generation job running: %v", err)
	}
	return id
}

func TestCollectionGenerationCompletionRequiresCallerVisibleCatalog(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	owner := api.User("catalog-owner")
	scoped := store.WithTenant(tenant).WithOwner(owner).WithWorkloadType(workloads.CollectionGeneration)
	createCatalogProvider(t, scoped, tenant, owner)

	validID := createRunningGenerationJob(t, scoped, tenant, owner)
	if err := scoped.UpdateEvaluationJob(validID, proposalEvent(t, "provider-a", "bench-a", nil)); err != nil {
		t.Fatalf("complete proposal using visible catalog benchmark: %v", err)
	}
	completed, err := scoped.WithWorkloadType(workloads.CollectionGeneration).GetEvaluationJob(validID)
	if err != nil || completed.Status.State != api.OverallStateCompleted || len(completed.Results.Benchmarks) != 1 {
		t.Fatalf("completed proposal = (%+v, %v)", completed, err)
	}

	for _, tc := range []struct {
		name     string
		provider string
		id       string
		rejected []api.CollectionGenerationBenchmarkReason
	}{
		{name: "provider outside tenant catalog", provider: "not-visible", id: "bench-a"},
		{name: "benchmark outside provider catalog", provider: "provider-a", id: "not-in-provider"},
		{name: "rejected benchmark outside provider catalog", provider: "provider-a", id: "bench-a", rejected: []api.CollectionGenerationBenchmarkReason{{
			ProviderID: "provider-a", ID: "not-in-provider", Reason: "It does not match the evaluation goal.",
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := createRunningGenerationJob(t, scoped, tenant, owner)
			if err := scoped.UpdateEvaluationJob(id, proposalEvent(t, tc.provider, tc.id, tc.rejected)); err == nil {
				t.Fatal("proposal referencing a non-visible catalog item was accepted")
			}
			stillRunning, err := scoped.WithWorkloadType(workloads.CollectionGeneration).GetEvaluationJob(id)
			if err != nil || stillRunning.Status.State != api.OverallStateRunning {
				t.Fatalf("rejected result changed job state: job=%+v err=%v", stillRunning, err)
			}
		})
	}
}
