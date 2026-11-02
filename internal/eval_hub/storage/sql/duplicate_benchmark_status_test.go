package sql_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// Older service versions accepted mismatched override counts. Seed such a job
// through storage, bypassing POST validation, and process real status updates.
func TestUpdateEvaluationJob_LegacyRepeatedBenchmarkOverrides(t *testing.T) {
	for _, counts := range [][2]int{{3, 2}, {2, 3}} {
		t.Run(fmt.Sprintf("%d rows %d overrides", counts[0], counts[1]), func(t *testing.T) {
			store, err := getTestStorage(t, drivers[0], getDBName())
			if err != nil {
				t.Fatal(err)
			}
			tenant := api.Tenant(common.GUID())
			scoped := store.WithTenant(tenant).WithOwner("legacy-owner")
			now := time.Now()
			collection := &api.CollectionResource{
				Resource:         api.Resource{ID: common.GUID(), Tenant: tenant, Owner: "legacy-owner", CreatedAt: now, UpdatedAt: now},
				CollectionConfig: api.CollectionConfig{Name: "Legacy repeated benchmarks"},
			}
			for range counts[0] {
				collection.Benchmarks = append(collection.Benchmarks, api.CollectionBenchmarkConfig{
					Ref: api.Ref{ID: "inspect/bfcl"}, ProviderID: "inspect",
				})
			}
			if err := scoped.CreateCollection(collection); err != nil {
				t.Fatal(err)
			}
			job := &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{
					ID: common.GUID(), Tenant: tenant, Owner: "legacy-owner", CreatedAt: now, UpdatedAt: now,
				}},
				EvaluationJobConfig: api.EvaluationJobConfig{
					Name: "Pre-upgrade job", Model: &api.ModelRef{URL: "http://model.example.com/v1", Name: "test-model"},
					Collection: &api.CollectionRef{ID: collection.Resource.ID},
				},
				Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning}},
			}
			for i := range counts[1] {
				job.Collection.Benchmarks = append(job.Collection.Benchmarks, api.EvaluationBenchmarkConfig{
					Ref: api.Ref{ID: "inspect/bfcl"}, ProviderID: "inspect", Parameters: map[string]any{"num_examples": i + 1},
				})
			}
			wantConfig, err := json.Marshal(job.EvaluationJobConfig)
			if err != nil {
				t.Fatal(err)
			}
			if err := scoped.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			statusEvent := func(index int, state api.State) *api.StatusEvent {
				return &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "inspect/bfcl", ProviderID: "inspect", BenchmarkIndex: index, Status: state,
				}}
			}
			if err := scoped.UpdateEvaluationJob(job.Resource.ID, statusEvent(0, api.StateRunning)); err != nil {
				t.Fatalf("legacy running callback must remain accepted: %v", err)
			}
			// Compatibility must not disable index validation.
			if err := scoped.UpdateEvaluationJob(job.Resource.ID, statusEvent(counts[0], api.StateCompleted)); err == nil {
				t.Fatal("out-of-range benchmark callback must be rejected")
			}
			// Finish in reverse order to distinguish repeated benchmark instances
			// by index and verify the total is collection rows, not override count.
			for i := counts[0] - 1; i >= 0; i-- {
				if err := scoped.UpdateEvaluationJob(job.Resource.ID, statusEvent(i, api.StateCompleted)); err != nil {
					t.Fatalf("legacy completion callback at index %d: %v", i, err)
				}
				got, err := scoped.GetEvaluationJob(job.Resource.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantState := api.OverallStateRunning
				if i == 0 {
					wantState = api.OverallStateCompleted
				}
				if got.Status.State != wantState {
					t.Fatalf("after index %d: state = %s, want %s", i, got.Status.State, wantState)
				}
				if got.Results == nil || len(got.Results.Benchmarks) != counts[0]-i {
					t.Fatalf("after index %d: expected %d distinct benchmark results, got %+v", i, counts[0]-i, got.Results)
				}
				if len(got.Status.Benchmarks) > counts[0] {
					t.Fatal("rejected callback must not add a benchmark status")
				}
				gotConfig, err := json.Marshal(got.EvaluationJobConfig)
				if err != nil {
					t.Fatal(err)
				}
				if string(gotConfig) != string(wantConfig) {
					t.Fatalf("status updates changed persisted config: got %s, want %s", gotConfig, wantConfig)
				}
			}
		})
	}
}
