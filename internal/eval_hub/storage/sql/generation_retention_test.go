package sql_test

import (
	"context"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type generationRetentionCleaner interface {
	DeleteExpiredCollectionGenerationJobs(time.Time, int) (int, error)
}

func generationJobForRetention(t *testing.T, id string, tenant api.Tenant, owner api.User, retention time.Duration) *api.EvaluationJobResource {
	t.Helper()
	job, err := generation.NewJob(id, tenant, owner, api.CollectionGenerationRequest{
		EvaluationGoal: "Evaluate safe tool use",
		Model:          api.ModelRef{URL: "https://model.example/v1", Name: "generator"},
	}, "evalhub:worker", loop.DefaultPresets()[loop.EffortStandard], "", retention, time.Now().UTC())
	if err != nil {
		t.Fatalf("create generation job fixture: %v", err)
	}
	return job
}

func failGenerationJob(t *testing.T, store abstractions.Storage, id string) {
	t.Helper()
	if err := store.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
		Status: api.StateFailed, AdditionalInfo: map[string]any{"collection_generation_error": map[string]any{"code": "model_failed"}},
	}}); err != nil {
		t.Fatalf("mark generation job %q failed: %v", id, err)
	}
}

func TestCollectionGenerationRetentionIsBoundedAndWorkloadScoped(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	owner := api.User("retention-test")
	scoped := store.WithTenant(tenant).WithOwner(owner)
	generationStore := scoped.WithWorkloadType(workloads.CollectionGeneration)

	const terminalCount = 3
	terminalIDs := make([]string, 0, terminalCount)
	for range terminalCount {
		id := common.GUID()
		job := generationJobForRetention(t, id, tenant, owner, time.Second)
		if err := scoped.CreateEvaluationJob(job); err != nil {
			t.Fatalf("create terminal generation job: %v", err)
		}
		if err := generationStore.WithContext(context.Background()).UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
			Status: api.StateRunning, CollectionGenerationProgress: &api.CollectionGenerationProgressEvent{Sequence: 1, Phase: "guardrails"},
		}}); err != nil {
			t.Fatalf("mark generation job running: %v", err)
		}
		failGenerationJob(t, generationStore.WithContext(context.Background()), id)
		terminalIDs = append(terminalIDs, id)
	}

	active := generationJobForRetention(t, common.GUID(), tenant, owner, time.Second)
	if err := scoped.CreateEvaluationJob(active); err != nil {
		t.Fatalf("create active generation job: %v", err)
	}
	ordinary := &api.EvaluationJobResource{
		Resource:            api.EvaluationResource{Resource: api.Resource{ID: common.GUID(), Tenant: tenant, Owner: owner}},
		EvaluationJobConfig: api.EvaluationJobConfig{Name: "ordinary evaluation"},
		Status:              &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateFailed}},
	}
	if err := scoped.CreateEvaluationJob(ordinary); err != nil {
		t.Fatalf("create ordinary evaluation job: %v", err)
	}

	cleaner, ok := store.(generationRetentionCleaner)
	if !ok {
		t.Fatal("SQL storage does not expose generation retention cleanup")
	}
	future := time.Now().UTC().Add(24 * time.Hour)
	deleted, err := cleaner.DeleteExpiredCollectionGenerationJobs(future, 2)
	if err != nil || deleted != 2 {
		t.Fatalf("first bounded cleanup = (%d, %v), want (2, nil)", deleted, err)
	}
	deleted, err = cleaner.DeleteExpiredCollectionGenerationJobs(future, 2)
	if err != nil || deleted != 1 {
		t.Fatalf("second bounded cleanup = (%d, %v), want (1, nil)", deleted, err)
	}
	for _, id := range terminalIDs {
		if _, err := generationStore.GetEvaluationJob(id); err == nil {
			t.Errorf("expired terminal generation job %q remains visible", id)
		}
	}
	if _, err := generationStore.GetEvaluationJob(active.Resource.ID); err != nil {
		t.Fatalf("active generation job was removed: %v", err)
	}
	if _, err := scoped.WithWorkloadType(workloads.Evaluation).GetEvaluationJob(ordinary.Resource.ID); err != nil {
		t.Fatalf("ordinary evaluation was removed by generation cleanup: %v", err)
	}
}

func TestExpiredGenerationIsHiddenBeforeCleanup(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	owner := api.User("expiry-test")
	scoped := store.WithTenant(tenant).WithOwner(owner)
	job := generationJobForRetention(t, common.GUID(), tenant, owner, time.Nanosecond)
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	generationStore := scoped.WithWorkloadType(workloads.CollectionGeneration)
	if err := generationStore.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
		Status: api.StateFailed, AdditionalInfo: map[string]any{"collection_generation_error": map[string]any{"code": "timeout"}},
	}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := generationStore.GetEvaluationJob(job.Resource.ID); err == nil {
		t.Fatal("expired generation was visible before physical cleanup")
	}
}
