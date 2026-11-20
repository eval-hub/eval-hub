package sql_test

import (
	"context"
	"sync"
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

type generationExpiryResetter interface {
	ClearCollectionGenerationExpiry(string) error
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
	testCollectionGenerationRetentionIsBoundedAndWorkloadScoped(t, drivers[0], getDBName())
}

func testCollectionGenerationRetentionIsBoundedAndWorkloadScoped(t *testing.T, driver, databaseName string) {
	t.Helper()
	store, err := getTestStorage(t, driver, databaseName)
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

	const concurrentCount = 4
	for range concurrentCount {
		id := common.GUID()
		job := generationJobForRetention(t, id, tenant, owner, time.Second)
		if err := scoped.CreateEvaluationJob(job); err != nil {
			t.Fatalf("create concurrent-cleanup job: %v", err)
		}
		failGenerationJob(t, generationStore, id)
	}
	var wait sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			count, err := cleaner.DeleteExpiredCollectionGenerationJobs(future, 10)
			counts <- count
			errs <- err
		}()
	}
	wait.Wait()
	close(counts)
	close(errs)
	deletedConcurrent := 0
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent cleanup: %v", err)
		}
	}
	for count := range counts {
		deletedConcurrent += count
	}
	if deletedConcurrent != concurrentCount {
		t.Errorf("concurrent cleanup deleted %d records, want %d", deletedConcurrent, concurrentCount)
	}
}

func TestExpiredGenerationIsHiddenBeforeCleanup(t *testing.T) {
	testExpiredGenerationIsHiddenBeforeCleanup(t, drivers[0], getDBName())
}

func testExpiredGenerationIsHiddenBeforeCleanup(t *testing.T, driver, databaseName string) {
	t.Helper()
	store, err := getTestStorage(t, driver, databaseName)
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
	if _, err := generationStore.WithContext(context.Background()).GetEvaluationJob(job.Resource.ID); err == nil {
		t.Fatal("expired generation was visible before physical cleanup")
	}
	if _, err := scoped.WithContext(context.Background()).WithWorkloadType(workloads.CollectionGeneration).GetEvaluationJob(job.Resource.ID); err == nil {
		t.Fatal("expired generation was visible when WithContext preceded WithWorkloadType")
	}
}

func TestGenerationRuntimeCleanupFailureCanBeRetried(t *testing.T) {
	testGenerationRuntimeCleanupFailureCanBeRetried(t, drivers[0], getDBName())
}

func testGenerationRuntimeCleanupFailureCanBeRetried(t *testing.T, driver, databaseName string) {
	t.Helper()
	store, err := getTestStorage(t, driver, databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	owner := api.User("cleanup-retry")
	scoped := store.WithTenant(tenant).WithOwner(owner)
	job := generationJobForRetention(t, common.GUID(), tenant, owner, time.Second)
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	generationStore := scoped.WithWorkloadType(workloads.CollectionGeneration)
	failGenerationJob(t, generationStore, job.Resource.ID)
	resetter, ok := generationStore.(generationExpiryResetter)
	if !ok {
		t.Fatal("SQL storage does not support preserving records for cleanup retry")
	}
	if err := resetter.ClearCollectionGenerationExpiry(job.Resource.ID); err != nil {
		t.Fatalf("clear expiry after failed runtime cleanup: %v", err)
	}
	cleaner, ok := store.(generationRetentionCleaner)
	if !ok {
		t.Fatal("SQL storage does not expose generation retention cleanup")
	}
	deleted, err := cleaner.DeleteExpiredCollectionGenerationJobs(time.Now().UTC().Add(24*time.Hour), 10)
	if err != nil || deleted != 0 {
		t.Fatalf("cleanup removed a record with retryable runtime resources: deleted=%d err=%v", deleted, err)
	}
	if _, err := generationStore.GetEvaluationJob(job.Resource.ID); err != nil {
		t.Fatalf("cleanup retry record is no longer available: %v", err)
	}
}
