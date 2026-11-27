package sql_test

import (
	"errors"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
	"sigs.k8s.io/yaml"
)

func TestGenerationRejectsInvalidEventsWithoutChangingState(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant, owner := api.Tenant(common.GUID()), api.User("event-owner")
	scoped := store.WithTenant(tenant).WithOwner(owner).WithWorkloadType(workloads.CollectionGeneration)
	id := createRunningGenerationJob(t, scoped, tenant, owner)
	for _, tc := range []struct {
		name   string
		change func(*api.BenchmarkStatusEvent)
		code   *messages.MessageCode
	}{
		{"wrong benchmark", func(e *api.BenchmarkStatusEvent) { e.ID = "another-benchmark" }, messages.ResourceNotFound},
		{"evaluation metrics", func(e *api.BenchmarkStatusEvent) { e.Metrics = map[string]any{"score": 1} }, messages.RequestValidationFailed},
		{"client supplied timestamp", func(e *api.BenchmarkStatusEvent) { e.StartedAt = "2026-11-23T17:00:00Z" }, messages.RequestValidationFailed},
		{"progress combined with completion", func(e *api.BenchmarkStatusEvent) {
			e.Status = api.StateCompleted
			e.CollectionGenerationProgress = &api.CollectionGenerationProgressEvent{Sequence: 2, Phase: "validation"}
		}, messages.RequestValidationFailed},
		{"stale progress", func(e *api.BenchmarkStatusEvent) {
			e.CollectionGenerationProgress = &api.CollectionGenerationProgressEvent{Sequence: 1, Phase: "finalization"}
		}, messages.RequestValidationFailed},
		{"running event with results", func(e *api.BenchmarkStatusEvent) {
			e.AdditionalInfo = map[string]any{"collection_generation_result": "unexpected"}
		}, messages.RequestValidationFailed},
		{"unsupported state", func(e *api.BenchmarkStatusEvent) { e.Status = api.StateCancelled }, messages.RequestValidationFailed},
		{"unstructured failure", func(e *api.BenchmarkStatusEvent) { e.Status = api.StateFailed }, messages.RequestValidationFailed},
		{"unrecognized infrastructure failure", func(e *api.BenchmarkStatusEvent) {
			e.Status = api.StateFailed
			e.ErrorMessage = &api.MessageInfo{MessageCode: "unrecognized", Message: "unstructured failure"}
		}, messages.RequestValidationFailed},
		{"invalid typed failure with an infrastructure message", func(e *api.BenchmarkStatusEvent) {
			e.Status = api.StateFailed
			e.ErrorMessage = &api.MessageInfo{MessageCode: "RUNTIME_FAILURE", Message: "pod failure"}
			e.AdditionalInfo = map[string]any{"collection_generation_error": api.CollectionGenerationError{Code: "guardrails_rejected"}}
		}, messages.RequestValidationFailed},
		{"guardrail rejection without category", func(e *api.BenchmarkStatusEvent) {
			e.Status = api.StateFailed
			e.AdditionalInfo = map[string]any{"collection_generation_error": api.CollectionGenerationError{Code: "guardrails_rejected"}}
		}, messages.RequestValidationFailed},
		{"non-guardrail failure with category", func(e *api.BenchmarkStatusEvent) {
			e.Status = api.StateFailed
			e.AdditionalInfo = map[string]any{"collection_generation_error": api.CollectionGenerationError{Code: "model_failed", Category: "topic_relevance"}}
		}, messages.RequestValidationFailed},
		{"completion with error", func(e *api.BenchmarkStatusEvent) {
			e.Status = api.StateCompleted
			e.ErrorMessage = &api.MessageInfo{Message: "incompatible with completion"}
		}, messages.RequestValidationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := api.BenchmarkStatusEvent{ProviderID: generation.ProviderID, ID: generation.BenchmarkID, Status: api.StateRunning}
			tc.change(&event)
			err := scoped.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &event})
			var serviceErr abstractions.ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.MessageCode() != tc.code {
				t.Fatalf("UpdateEvaluationJob() = %v, want %v", err, tc.code)
			}
			job, err := scoped.GetEvaluationJob(id)
			if err != nil || job.Status.State != api.OverallStateRunning ||
				job.Status.CollectionGenerationProgress.Sequence != 1 || job.Status.CollectionGenerationProgress.Phase != "catalog_discovery" {
				t.Fatalf("rejected event changed the job: %+v, %v", job, err)
			}
		})
	}
	failure := &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, Status: api.StateFailed,
		AdditionalInfo: map[string]any{"collection_generation_error": api.CollectionGenerationError{
			Code: "guardrails_rejected", Category: "topic_relevance", Message: "upstream detector details",
		}},
	}
	if err := scoped.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: failure}); err != nil {
		t.Fatalf("persist guardrail rejection: %v", err)
	}
	job, err := scoped.GetEvaluationJob(id)
	if err != nil {
		t.Fatalf("read failed job: %v", err)
	}
	resource, err := generation.ResourceFromJob(job)
	if err != nil {
		t.Fatalf("map failed job: %v", err)
	}
	got := resource.Status.Error
	if got == nil || got.Code != "guardrails_rejected" || got.Category != "topic_relevance" || got.Message != "The request was rejected by generation safety checks." {
		t.Fatalf("mapped guardrail rejection = %+v", got)
	}
	event := api.BenchmarkStatusEvent{ProviderID: generation.ProviderID, ID: generation.BenchmarkID, Status: api.StateRunning}
	err = scoped.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &event})
	var serviceErr abstractions.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.MessageCode() != messages.JobCanNotBeUpdated {
		t.Fatalf("late event after terminal failure = %v", err)
	}
}

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

func createCatalogProvider(t *testing.T, store abstractions.Storage, tenant api.Tenant, owner api.User) string {
	t.Helper()
	id := common.GUID()
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: id, Tenant: tenant, Owner: owner},
		ProviderConfig: api.ProviderConfig{
			Name: "Provider A", Title: "Provider A",
			Benchmarks: []api.BenchmarkResource{{ID: "bench-a", Name: "Benchmark A"}},
		},
	}
	if err := store.CreateProvider(provider); err != nil {
		t.Fatalf("create caller-visible provider: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteProvider(id); err != nil {
			t.Errorf("delete caller-visible provider: %v", err)
		}
	})
	return id
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
	removeGenerationFixture(t, store, id)
	if err := store.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
		Status: api.StateRunning,
	}}); err != nil {
		t.Fatalf("mark generation job running: %v", err)
	}
	if err := store.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
		Status: api.StateRunning, CollectionGenerationProgress: &api.CollectionGenerationProgressEvent{Sequence: 1, Phase: "catalog_discovery"},
	}}); err != nil {
		t.Fatalf("persist generation progress: %v", err)
	}
	return id
}

func TestCollectionGenerationCompletionRequiresCallerVisibleCatalog(t *testing.T) {
	testCollectionGenerationCompletionRequiresCallerVisibleCatalog(t, drivers[0], getDBName())
}

func TestGenerationInfrastructureFailuresBecomeTerminal(t *testing.T) {
	testGenerationInfrastructureFailuresBecomeTerminal(t, drivers[0], getDBName())
}

func testGenerationInfrastructureFailuresBecomeTerminal(t *testing.T, driver, databaseName string) {
	t.Helper()
	store, err := getTestStorage(t, driver, databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant, owner := api.Tenant(common.GUID()), api.User("event-owner")
	scoped := store.WithTenant(tenant).WithOwner(owner).WithWorkloadType(workloads.CollectionGeneration)
	for _, tc := range []struct {
		code           string
		running        bool
		additionalInfo map[string]any
	}{
		{code: "RUNTIME_FAILURE", running: true},
		{code: "queue_error", additionalInfo: map[string]any{}},
		{code: "gpu_unavailable", additionalInfo: map[string]any{"reason": "private queue details"}},
	} {
		t.Run(tc.code, func(t *testing.T) {
			var id string
			if tc.running {
				id = createRunningGenerationJob(t, scoped, tenant, owner)
			} else {
				id = common.GUID()
				if err := scoped.CreateEvaluationJob(generationJobForRetention(t, id, tenant, owner, 24*time.Hour)); err != nil {
					t.Fatal(err)
				}
				removeGenerationFixture(t, scoped, id)
			}
			event := &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ProviderID: generation.ProviderID, ID: generation.BenchmarkID, Status: api.StateFailed,
				ErrorMessage:   &api.MessageInfo{MessageCode: tc.code, Message: "private infrastructure details"},
				AdditionalInfo: tc.additionalInfo,
			}}
			if err := scoped.UpdateEvaluationJob(id, event); err != nil {
				t.Fatalf("persist infrastructure failure: %v", err)
			}
			job, err := scoped.GetEvaluationJob(id)
			if err != nil {
				t.Fatal(err)
			}
			resource, err := generation.ResourceFromJob(job)
			if err != nil {
				t.Fatal(err)
			}
			failure := resource.Status.Error
			if resource.Status.State != api.OverallStateFailed || failure == nil || failure.Code != "workload_failed" ||
				failure.Category != "" || resource.Status.Progress != nil || resource.Result != nil {
				t.Fatalf("incorrect generation status: %+v", resource.Status)
			}
			if job.Status.Message.Message != "The collection-generation worker failed." ||
				job.Status.Benchmarks[0].ErrorMessage.Message != job.Status.Message.Message ||
				failure.Message != job.Status.Message.Message {
				t.Fatalf("infrastructure diagnostic was not sanitized: %+v", job.Status)
			}
			if info := job.Results.Benchmarks[0].AdditionalInfo; len(info) != 1 || info["collection_generation_error"] == nil {
				t.Fatalf("unexpected persisted infrastructure metadata: %v", info)
			}
			cleaner := store.(generationRetentionCleaner)
			removed, err := cleaner.DeleteExpiredCollectionGenerationJobs(time.Now().UTC().Add(25*time.Hour), 100)
			if err != nil || removed == 0 {
				t.Fatalf("no terminal retention expiry after infrastructure failure: deleted %d, err %v", removed, err)
			}
			if _, err := scoped.GetEvaluationJob(id); err == nil {
				t.Fatal("expired infrastructure failure was not removed")
			}
		})
	}
}

func testCollectionGenerationCompletionRequiresCallerVisibleCatalog(t *testing.T, driver, databaseName string) {
	t.Helper()
	store, err := getTestStorage(t, driver, databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	owner := api.User("catalog-owner")
	scoped := store.WithTenant(tenant).WithOwner(owner).WithWorkloadType(workloads.CollectionGeneration)
	providerID := createCatalogProvider(t, scoped, tenant, owner)

	validID := createRunningGenerationJob(t, scoped, tenant, owner)
	if err := scoped.UpdateEvaluationJob(validID, proposalEvent(t, providerID, "bench-a", nil)); err != nil {
		t.Fatalf("complete proposal using visible catalog benchmark: %v", err)
	}
	completed, err := scoped.WithWorkloadType(workloads.CollectionGeneration).GetEvaluationJob(validID)
	if err != nil || completed.Status.State != api.OverallStateCompleted || len(completed.Results.Benchmarks) != 1 ||
		completed.Status.CollectionGenerationProgress == nil || completed.Status.CollectionGenerationProgress.Phase != "catalog_discovery" {
		t.Fatalf("completed proposal = (%+v, %v)", completed, err)
	}

	for _, tc := range []struct {
		name     string
		provider string
		id       string
		rejected []api.CollectionGenerationBenchmarkReason
	}{
		{name: "provider outside tenant catalog", provider: "not-visible", id: "bench-a"},
		{name: "benchmark outside provider catalog", provider: providerID, id: "not-in-provider"},
		{name: "rejected benchmark outside provider catalog", provider: providerID, id: "bench-a", rejected: []api.CollectionGenerationBenchmarkReason{{
			ProviderID: providerID, ID: "not-in-provider", Reason: "It does not match the evaluation goal.",
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := createRunningGenerationJob(t, scoped, tenant, owner)
			if err := scoped.UpdateEvaluationJob(id, proposalEvent(t, tc.provider, tc.id, tc.rejected)); err != nil {
				t.Fatalf("invalid completed proposal was not durably converted to a failure: %v", err)
			}
			failed, err := scoped.WithWorkloadType(workloads.CollectionGeneration).GetEvaluationJob(id)
			if err != nil || failed.Status.State != api.OverallStateFailed || len(failed.Results.Benchmarks) != 1 {
				t.Fatalf("invalid proposal did not persist a terminal failure: job=%+v err=%v", failed, err)
			}
			failure, err := generation.FailureFromAdditionalInfo(failed.Results.Benchmarks[0].AdditionalInfo)
			if err != nil || failure.Code != "validation_failed" {
				t.Fatalf("persisted failure = %+v, %v", failure, err)
			}
		})
	}
}

func TestCollectionGenerationSQLPostgres(t *testing.T) {
	image := usePostgresImage()
	databaseName := getDBName()
	user, err := getPostgresUser()
	if err != nil {
		t.Skipf("Failed to get Postgres user: %v", err)
	}
	if err := startPostgres(t, databaseName, user, image); err != nil {
		t.Skipf("Skipping postgres tests: %v", err)
	}
	t.Cleanup(func() { stopPostgres(t, databaseName, user, image) })
	t.Run("status-results-catalog", func(t *testing.T) {
		testCollectionGenerationCompletionRequiresCallerVisibleCatalog(t, drivers[1], databaseName)
	})
	t.Run("infrastructure-failure", func(t *testing.T) {
		testGenerationInfrastructureFailuresBecomeTerminal(t, drivers[1], databaseName)
	})
	t.Run("retention-expiry-and-concurrency", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			test func(*testing.T, string, string)
		}{
			{"bounded", testCollectionGenerationRetentionIsBoundedAndWorkloadScoped},
			{"expiry", testExpiredGenerationIsHiddenBeforeCleanup},
			{"retry", testGenerationRuntimeCleanupFailureCanBeRetried},
			{"concurrent-cleanup", testGenerationRetentionCannotDeleteRecordDuringRuntimeCleanup},
			{"same-tenant-delete", testGenerationDeleteProtectionIsTenantScopedAndPreservesExpiry},
			{"finalize-after-expiry", testSuccessfulDeleteFinalizesAfterExpiry},
		} {
			t.Run(tc.name, func(t *testing.T) { tc.test(t, drivers[1], databaseName) })
		}
	})
}
