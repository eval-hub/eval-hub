package sql

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// RecordBenchmarkCreation is independent of the status event path: a fast runtime
// may report running/completed before its resource creation metadata is persisted.
func (s *sqlStorage) RecordBenchmarkCreation(id string, benchmarkIndex int, createdAt time.Time) error {
	if createdAt.IsZero() {
		return fmt.Errorf("benchmark creation timestamp is required")
	}
	return s.withTransaction("record benchmark creation", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		var collection *api.CollectionResource
		if job.Collection != nil && job.Collection.ID != "" {
			collection, err = s.getCollectionTransactional(txn, job.Collection.ID)
			if err != nil {
				return err
			}
		}
		benchmarks, err := handlers.GetJobBenchmarks(job, collection)
		if err != nil {
			return err
		}
		if benchmarkIndex < 0 || benchmarkIndex >= len(benchmarks) {
			return se.NewServiceError(messages.ResourceNotFound, "Type", "benchmark", "ResourceId", fmt.Sprint(benchmarkIndex), "Error", "Invalid benchmark index")
		}
		if job.Status == nil {
			job.Status = &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending}}
		}
		index := -1
		for i := range job.Status.Benchmarks {
			if job.Status.Benchmarks[i].BenchmarkIndex == benchmarkIndex {
				index = i
				break
			}
		}
		if index == -1 {
			benchmark := api.BenchmarkStatus{
				ID: benchmarks[benchmarkIndex].ID, ProviderID: benchmarks[benchmarkIndex].ProviderID,
				BenchmarkIndex: benchmarkIndex, Status: api.StatePending,
			}
			// Cancellation/startup failure may win the race before this benchmark
			// has a status entry. Recording creation must not resurrect it as pending.
			if job.Status.State == api.OverallStateCancelled || job.Status.State == api.OverallStateFailed {
				benchmark.Status = api.State(job.Status.State)
				benchmark.ErrorMessage = job.Status.Message
			}
			job.Status.Benchmarks = append(job.Status.Benchmarks, benchmark)
			index = len(job.Status.Benchmarks) - 1
		}
		benchmark := &job.Status.Benchmarks[index]
		if benchmark.CreatedAt != "" {
			return nil
		}
		benchmark.CreatedAt = api.DateTimeToString(createdAt.UTC())
		stampBenchmarkTimestamps(benchmark, time.Now())
		entity := &EvaluationJobEntity{Config: &job.EvaluationJobConfig, Status: job.Status, Results: job.Results}
		return s.updateEvaluationJobTxn(txn, id, job.Status.State, entity)
	})
}
