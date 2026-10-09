package sql_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
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
	for _, reason := range []string{"unfinished", "self reference"} {
		t.Run(reason, func(t *testing.T) {
			source := postProcessingTestJob(common.GUID(), tenant, api.EvaluationJobConfig{Name: "source"})
			source.Status.State = api.OverallStateCompleted
			job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
			switch reason {
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
				t.Fatal("expected source validation failure")
			}
			stored, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status.State != api.OverallStatePending || len(stored.Status.Benchmarks) != 0 || stored.Results != nil {
				t.Fatalf("completion did not roll back: %+v", stored)
			}
		})
	}
}

func TestPostProcessingCompletionWithUnavailableSource(t *testing.T) {
	for _, test := range []struct {
		name          string
		anotherTenant bool
	}{
		{name: "missing"},
		{name: "another tenant", anotherTenant: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := getTestStorage(t, drivers[0], getDBName())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })

			tenant := api.Tenant(common.GUID())
			scoped := store.WithTenant(tenant).WithOwner("owner")
			sourceTenant := tenant
			if test.anotherTenant {
				sourceTenant = api.Tenant(common.GUID())
			}
			sourceStore := store.WithTenant(sourceTenant).WithOwner("owner")
			source := postProcessingTestJob(common.GUID(), sourceTenant, api.EvaluationJobConfig{Name: "source"})
			source.Status.State = api.OverallStateCompleted
			if test.anotherTenant {
				if err := sourceStore.CreateEvaluationJob(source); err != nil {
					t.Fatal(err)
				}
			}

			job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
			if err := scoped.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			if err := scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion()); err != nil {
				t.Fatalf("complete post-processing job with %s source: %v", test.name, err)
			}

			completed, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if completed.Status.State != api.OverallStateCompleted {
				t.Fatalf("state = %s, want completed", completed.Status.State)
			}

			if test.anotherTenant {
				storedSource, err := sourceStore.GetEvaluationJob(source.Resource.ID)
				if err != nil {
					t.Fatal(err)
				}
				if storedSource.Results != nil {
					t.Fatalf("cross-tenant source was changed: %+v", storedSource.Results)
				}
			}
		})
	}
}

func TestPostProcessingCompletionReturnsSourceReadError(t *testing.T) {
	databaseName := getDBName()
	store, err := getTestStorage(t, drivers[0], databaseName)
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
	job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}

	corruptDB, err := sql.Open("sqlite", getDBInMemoryURL(databaseName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = corruptDB.Close() })
	if _, err := corruptDB.Exec(`UPDATE evaluations SET entity = '{' WHERE id = ?`, source.Resource.ID); err != nil {
		t.Fatalf("corrupt source entity: %v", err)
	}

	err = scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion())
	if err == nil || !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("UpdateEvaluationJob() error = %v, want source JSON decode error", err)
	}
	storedJob, err := scoped.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedJob.Status.State != api.OverallStatePending || storedJob.Results != nil {
		t.Fatalf("completion was not rolled back after source read error: %+v", storedJob)
	}
}

func TestPostProcessingConcurrentCompletions(t *testing.T) {
	testPostProcessingConcurrentCompletions(t, drivers[0], getDBName())
}

func TestPostProcessingCompletionWithoutEvaluationSource(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	tests := []struct {
		name   string
		config api.EvaluationJobConfig
	}{
		{
			name: "external results",
			config: *postprocessing.ToEvaluationJob(&api.StandalonePostProcessingRequest{
				Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
					ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
						CalibrationDataRef: []api.CalibrationDataRef{{
							PVC:        &api.PVCTestDataRef{ClaimName: "calibration"},
							DataConfig: api.CalibrationDataConfig{Format: "jsonl", Columns: api.CalibrationDataColumns{Label: "label", Prediction: "prediction"}},
						}},
						SignificanceLevel: 0.05,
					},
					ResultsDataRef: &api.PostProcessingResultsDataRef{PVC: &api.PVCTestDataRef{ClaimName: "results"}},
					PrimaryScore:   &api.PrimaryScore{Metric: "accuracy"},
				}},
			}),
		},
		{
			name: "missing results reference",
			config: api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{
				ProviderID: postprocessing.ProviderID,
				Ref:        api.Ref{ID: postprocessing.BenchmarkID},
				Parameters: map[string]any{"operations": map[string]any{"confidence_interval": map[string]any{}}},
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := postProcessingTestJob(common.GUID(), tenant, test.config)
			if err := scoped.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			if err := scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion()); err != nil {
				t.Fatalf("complete post-processing job: %v", err)
			}
			stored, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status.State != api.OverallStateCompleted {
				t.Fatalf("state = %s, want completed", stored.Status.State)
			}
		})
	}
}

func TestPostProcessingCompletionRejectsMissingOperations(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	job := postProcessingTestJob(common.GUID(), tenant, api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{
		ProviderID: postprocessing.ProviderID,
		Ref:        api.Ref{ID: postprocessing.BenchmarkID},
	}}})
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	if err := scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion()); err == nil {
		t.Fatal("expected completion to fail when the stored computation has no operations")
	}
	stored, err := scoped.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status.State != api.OverallStatePending {
		t.Fatalf("state = %s, want pending after rollback", stored.Status.State)
	}
}

func TestPostProcessingCompletionDoesNotUpdateSourceEvaluation(t *testing.T) {
	databaseName := getDBName()
	store, err := getTestStorage(t, drivers[0], databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	source := postProcessingTestJob("source-for-trigger", tenant, api.EvaluationJobConfig{Name: "source"})
	source.Status.State = api.OverallStateCompleted
	source.Results = &api.EvaluationJobResults{
		Benchmarks:          []api.BenchmarkResult{{ID: "accuracy", Metrics: map[string]any{"accuracy": 0.9}}},
		MLFlowExperimentURL: "https://mlflow.example/experiment/1",
	}
	if err := scoped.CreateEvaluationJob(source); err != nil {
		t.Fatal(err)
	}
	job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	triggerDB, err := sql.Open("sqlite", getDBInMemoryURL(databaseName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = triggerDB.Close() })
	_, err = triggerDB.Exec(`CREATE TRIGGER reject_source_link BEFORE UPDATE ON evaluations
WHEN OLD.id = 'source-for-trigger' BEGIN SELECT RAISE(FAIL, 'reject post-processing source update'); END;`)
	if err != nil {
		t.Fatalf("create source-update trigger: %v", err)
	}
	if err := scoped.UpdateEvaluationJob(job.Resource.ID, postProcessingCompletion()); err != nil {
		t.Fatalf("complete post-processing job: %v", err)
	}
	stored, err := scoped.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status.State != api.OverallStateCompleted {
		t.Fatalf("state = %s, want completed", stored.Status.State)
	}
	storedSource, err := scoped.GetEvaluationJob(source.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSource.Results == nil || len(storedSource.Results.Benchmarks) != 1 || storedSource.Results.MLFlowExperimentURL != source.Results.MLFlowExperimentURL {
		t.Fatalf("source results changed when post-processing completed: %+v", storedSource.Results)
	}
}

func TestDeletePostProcessingDoesNotUpdateSourceEvaluation(t *testing.T) {
	databaseName := getDBName()
	store, err := getTestStorage(t, drivers[0], databaseName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	source := postProcessingTestJob("source-delete-no-update", tenant, api.EvaluationJobConfig{Name: "source"})
	source.Status.State = api.OverallStateCompleted
	source.Results = &api.EvaluationJobResults{
		Benchmarks:          []api.BenchmarkResult{{ID: "accuracy", Metrics: map[string]any{"accuracy": 0.9}}},
		MLFlowExperimentURL: "https://mlflow.example/experiment/1",
	}
	if err := scoped.CreateEvaluationJob(source); err != nil {
		t.Fatal(err)
	}
	job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}

	triggerDB, err := sql.Open("sqlite", getDBInMemoryURL(databaseName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = triggerDB.Close() })
	_, err = triggerDB.Exec(fmt.Sprintf(`CREATE TRIGGER reject_source_unlink BEFORE UPDATE ON evaluations
WHEN OLD.id = '%s' BEGIN SELECT RAISE(FAIL, 'source update is not expected'); END;`, source.Resource.ID))
	if err != nil {
		t.Fatalf("create source-update trigger: %v", err)
	}

	postProcessingStorage := scoped.WithContext(context.Background()).WithWorkloadType(workloads.PostProcessing)
	if err := postProcessingStorage.DeleteEvaluationJob(job.Resource.ID); err != nil {
		t.Fatalf("delete post-processing job: %v", err)
	}
	storedSource, err := scoped.GetEvaluationJob(source.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSource.Status.State != api.OverallStateCompleted || storedSource.Results == nil ||
		len(storedSource.Results.Benchmarks) != 1 ||
		storedSource.Results.MLFlowExperimentURL != source.Results.MLFlowExperimentURL {
		t.Fatalf("deleting post-processing changed the source evaluation: %+v", storedSource)
	}
	if _, err := scoped.GetEvaluationJob(job.Resource.ID); err == nil {
		t.Fatal("deleted post-processing job is still stored")
	}
}

func TestDeleteEvaluationJobDoesNotParsePostProcessingOperations(t *testing.T) {
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tenant := api.Tenant(common.GUID())
	scoped := store.WithTenant(tenant).WithOwner("owner")
	malformed := postProcessingTestJob(common.GUID(), tenant, api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{
		ProviderID: postprocessing.ProviderID,
		Ref:        api.Ref{ID: postprocessing.BenchmarkID},
		Parameters: map[string]any{"operations": map[string]any{"confidence_interval": "invalid"}},
	}}})
	if err := scoped.CreateEvaluationJob(malformed); err != nil {
		t.Fatal(err)
	}
	if err := scoped.WithContext(context.Background()).WithWorkloadType(workloads.PostProcessing).DeleteEvaluationJob(malformed.Resource.ID); err != nil {
		t.Fatalf("delete malformed post-processing job: %v", err)
	}
	if _, err := scoped.GetEvaluationJob(malformed.Resource.ID); err == nil {
		t.Fatal("malformed post-processing job is still stored")
	}
}

func TestDeletePostProcessingDoesNotReadSourceEvaluation(t *testing.T) {
	databaseName := getDBName()
	store, err := getTestStorage(t, drivers[0], databaseName)
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
	job := postProcessingTestJob(common.GUID(), tenant, postProcessingTestConfig(source.Resource.ID))
	if err := scoped.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}

	corruptDB, err := sql.Open("sqlite", getDBInMemoryURL(databaseName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = corruptDB.Close() })
	if _, err := corruptDB.Exec(`UPDATE evaluations SET entity = '{' WHERE id = ?`, source.Resource.ID); err != nil {
		t.Fatalf("corrupt source entity: %v", err)
	}

	postProcessingStorage := scoped.WithContext(context.Background()).WithWorkloadType(workloads.PostProcessing)
	if err := postProcessingStorage.DeleteEvaluationJob(job.Resource.ID); err != nil {
		t.Fatalf("delete post-processing job with unreadable source: %v", err)
	}
	if _, err := scoped.GetEvaluationJob(job.Resource.ID); err == nil {
		t.Fatal("deleted post-processing job is still stored")
	}
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
	storedSource, err := scoped.GetEvaluationJob(source.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSource.Results != nil {
		t.Fatalf("post-processing completion changed source results: %+v", storedSource.Results)
	}
	for _, id := range ids {
		assertCompletedPostProcessing(t, scoped, id)
	}
	// A later successful completion must also leave the source unchanged.
	last := common.GUID()
	if err := scoped.CreateEvaluationJob(postProcessingTestJob(last, tenant, postProcessingTestConfig(source.Resource.ID))); err != nil {
		t.Fatal(err)
	}
	if err := scoped.UpdateEvaluationJob(last, postProcessingCompletion()); err != nil {
		t.Fatal(err)
	}
	storedSource, err = scoped.GetEvaluationJob(source.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSource.Results != nil {
		t.Fatalf("later completion changed source results: %+v", storedSource.Results)
	}
}

func assertCompletedPostProcessing(t *testing.T, store abstractions.Storage, id string) {
	t.Helper()
	job, err := store.GetEvaluationJob(id)
	if err != nil || job.Status.State != api.OverallStateCompleted || !postprocessing.IsPostProcessingJob(&job.EvaluationJobConfig) {
		t.Fatalf("expected completed computation %s: %v, %+v", id, err, job)
	}
}
