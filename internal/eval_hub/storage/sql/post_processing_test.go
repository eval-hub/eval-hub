package sql_test

import (
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func postProcessingTestJob(id string, tenant api.Tenant, cfg api.EvaluationJobConfig) *api.EvaluationJobResource {
	return &api.EvaluationJobResource{
		Resource:            api.EvaluationResource{Resource: api.Resource{ID: id, Tenant: tenant, Owner: "owner", CreatedAt: time.Now()}},
		EvaluationJobConfig: cfg,
		Status:              &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending}},
	}
}

func postProcessingTestConfig(sourceID string) api.EvaluationJobConfig {
	return *postprocessing.ToEvaluationJob(&api.StandalonePostProcessingRequest{
		PostProcessingCommon: api.PostProcessingCommon{Name: "post-processing"},
		Operations: api.StandalonePostProcessingOperations{
			ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
				ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
					CalibrationDataRef: []api.CalibrationDataRef{{
						PVC:        &api.PVCTestDataRef{ClaimName: "calibration"},
						DataConfig: api.CalibrationDataConfig{Format: "jsonl", Columns: api.CalibrationDataColumns{Label: "label", Prediction: "prediction"}},
					}},
					SignificanceLevel: 0.05,
				},
				ResultsDataRef: &api.PostProcessingResultsDataRef{EvalJob: &api.EvaluationJobDataRef{ID: sourceID}},
			},
		},
	})
}

func postProcessingCompletion() *api.StatusEvent {
	return &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: postprocessing.ProviderID,
		ID:         postprocessing.BenchmarkID,
		Status:     api.StateCompleted,
		Metrics:    map[string]any{"lower": 0.8, "upper": 0.9},
	}}
}

func TestPostProcessingCompletionAtomic(t *testing.T) {
	testPostProcessingCompletionAtomic(t, drivers[0], getDBName())
}

func testPostProcessingCompletionAtomic(t *testing.T, driver, databaseName string) {
	store, err := getTestStorage(t, driver, databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	for _, reason := range []string{"missing", "another tenant", "unfinished", "self reference"} {
		t.Run(reason, func(t *testing.T) {
			source := postProcessingTestJob(common.GUID(), tenant, api.EvaluationJobConfig{Name: "source"})
			source.Status.State = api.OverallStateCompleted
			job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
			switch reason {
			case "another tenant":
				source.Resource.Tenant = api.Tenant(common.GUID())
				if err := store.WithTenant(source.Resource.Tenant).CreateEvaluationJob(source); err != nil {
					t.Fatal(err)
				}
			case "unfinished":
				source.Status.State = api.OverallStateRunning
				if err := scoped.CreateEvaluationJob(source); err != nil {
					t.Fatal(err)
				}
			case "self reference":
				job.EvaluationJobConfig = postProcessingTestConfig(job.Resource.ID)
			}
			if err := scoped.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			if err := scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion()); err == nil {
				t.Fatal("expected source-link failure")
			}
			stored, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status.State != api.OverallStatePending || len(stored.Status.Benchmarks) != 0 || stored.Results != nil {
				t.Fatalf("completion did not roll back: %+v", stored)
			}
			if reason == "missing" {
				// A failed link does not make the job terminal: completion can be retried.
				if err := scoped.CreateEvaluationJob(source); err != nil {
					t.Fatal(err)
				}
				if err := scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion()); err != nil {
					t.Fatal(err)
				}
				linked, err := scoped.GetEvaluationJob(source.Resource.ID)
				if err != nil || linked.Results == nil || linked.Results.PostProcessingRef == nil || linked.Results.PostProcessingRef.ID != job.Resource.ID {
					t.Fatalf("retry did not link source: %v, %+v", err, linked)
				}
			}
		})
	}
}

func TestPostProcessingConcurrentCompletions(t *testing.T) {
	testPostProcessingConcurrentCompletions(t, drivers[0], getDBName())
}

func testPostProcessingConcurrentCompletions(t *testing.T, driver, databaseName string) {
	store, err := getTestStorage(t, driver, databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	source := postProcessingTestJob(common.GUID(), tenant, api.EvaluationJobConfig{Name: "source"})
	source.Status.State = api.OverallStateCompleted
	if err := scoped.CreateEvaluationJob(source); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 4)
	for i := range ids {
		ids[i] = common.GUID()
		if err := scoped.CreateEvaluationJob(postProcessingTestJob(ids[i], tenant, postProcessingTestConfig(source.Resource.ID))); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errs := make(chan error, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			<-start
			errs <- scoped.UpdateEvaluationJob(id, postProcessingCompletion())
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent completion: %v", err)
		}
	}
	linked, err := scoped.GetEvaluationJob(source.Resource.ID)
	if err != nil || linked.Results == nil || linked.Results.PostProcessingRef == nil {
		t.Fatalf("source has no reference: %v, %+v", err, linked)
	}
	assertCompletedPostProcessing(t, scoped, linked.Results.PostProcessingRef.ID)
	for _, id := range ids {
		assertCompletedPostProcessing(t, scoped, id)
	}
	// A later successful completion deterministically replaces the concurrent result.
	last := common.GUID()
	if err := scoped.CreateEvaluationJob(postProcessingTestJob(last, tenant, postProcessingTestConfig(source.Resource.ID))); err != nil {
		t.Fatal(err)
	}
	if err := scoped.UpdateEvaluationJob(last, postProcessingCompletion()); err != nil {
		t.Fatal(err)
	}
	linked, err = scoped.GetEvaluationJob(source.Resource.ID)
	if err != nil || linked.Results.PostProcessingRef.ID != last {
		t.Fatalf("latest successful completion did not replace reference: %v", err)
	}
}

func assertCompletedPostProcessing(t *testing.T, store abstractions.Storage, id string) {
	t.Helper()
	job, err := store.GetEvaluationJob(id)
	if err != nil || job.Status.State != api.OverallStateCompleted || !postprocessing.IsPostProcessingJob(&job.EvaluationJobConfig) {
		t.Fatalf("expected completed computation %s: %v, %+v", id, err, job)
	}
}
