package sql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type EvaluationJobEntity struct {
	Config  *api.EvaluationJobConfig  `json:"config" validate:"required"`
	Status  *api.EvaluationJobStatus  `json:"status,omitempty"`
	Results *api.EvaluationJobResults `json:"results,omitempty"`
}

// #######################################################################
// Evaluation job operations
// #######################################################################
func (s *sqlStorage) CreateEvaluationJob(evaluation *api.EvaluationJobResource) error {
	return s.withTransaction("create evaluation job", evaluation.Resource.ID, func(txn *sql.Tx) error {
		return s.createEvaluationJobTransactional(txn, evaluation)
	})
}

// CreateEvaluationJobAndUpdateCollection atomically persists an evaluation job and
// applies server-managed updates to the collection referenced by the job.
func (s *sqlStorage) CreateEvaluationJobAndUpdateCollection(evaluation *api.EvaluationJobResource) error {
	if evaluation == nil || evaluation.Collection == nil || evaluation.Collection.ID == "" {
		return se.NewServiceError(messages.RequestValidationFailed, "Error", "collection ID is required")
	}

	collectionID := evaluation.Collection.ID
	return s.withTransaction("create evaluation job and update collection", evaluation.Resource.ID, func(txn *sql.Tx) error {
		collection, err := s.getCollectionTransactionalForUpdate(txn, collectionID)
		if err != nil {
			return se.WithRollback(err)
		}
		if err = s.createEvaluationJobTransactional(txn, evaluation); err != nil {
			return err
		}
		if collection.Resource.IsSystemResource() {
			return nil
		}
		if collection.Status == nil {
			collection.Status = &api.CollectionStatus{}
		}
		collection.Status.RunCount++
		return s.updateCollectionTransactional(txn, collection.Resource.ID, collection)
	})
}

func (s *sqlStorage) createEvaluationJobTransactional(txn *sql.Tx, evaluation *api.EvaluationJobResource) error {
	evaluationJSON, err := s.createEvaluationJobEntity(evaluation)
	if err != nil {
		return se.WithRollback(err)
	}
	workloadType := workloads.TypeForJob(&evaluation.EvaluationJobConfig)
	addEntityStatement, args := s.statementsFactory.CreateEvaluationAddEntityStatement(evaluation, string(evaluationJSON), workloadType)
	_, err = s.exec(txn, addEntityStatement, args...)
	if err != nil {
		return se.WithRollback(err)
	}
	s.logger.Info("Created evaluation job", "id", evaluation.Resource.ID, "addEntityStatement", addEntityStatement)
	return nil
}

func (s *sqlStorage) createEvaluationJobEntity(evaluation *api.EvaluationJobResource) ([]byte, error) {
	evaluationEntity := &EvaluationJobEntity{
		Config:  &evaluation.EvaluationJobConfig,
		Status:  evaluation.Status,
		Results: evaluation.Results,
	}
	evaluationJSON, err := json.Marshal(evaluationEntity)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return evaluationJSON, nil
}

func (s *sqlStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	return s.getEvaluationJobTransactional(nil, id)
}

func (s *sqlStorage) getEvaluationJobTransactional(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	return s.scanEvaluationJobTransactional(txn, id, false, workloads.TypeFromContext(s.ctx))
}

func (s *sqlStorage) getEvaluationJobTransactionalForUpdate(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	return s.scanEvaluationJobTransactional(txn, id, true, workloads.TypeFromContext(s.ctx))
}

func (s *sqlStorage) getEvaluationJobTransactionalForUpdateWithType(txn *sql.Tx, id string, workloadType workloads.Type) (*api.EvaluationJobResource, error) {
	return s.scanEvaluationJobTransactional(txn, id, true, workloadType)
}

func (s *sqlStorage) scanEvaluationJobTransactional(txn *sql.Tx, id string, forUpdate bool, workloadType workloads.Type) (*api.EvaluationJobResource, error) {
	query := shared.EntityQuery{
		Resource:     api.Resource{ID: id, Tenant: s.tenant},
		WorkloadType: workloadType,
	}
	var selectQuery string
	var selectArgs, queryArgs []any
	if forUpdate {
		selectQuery, selectArgs, queryArgs = s.statementsFactory.CreateEvaluationGetEntityForUpdateStatement(&query)
	} else {
		selectQuery, selectArgs, queryArgs = s.statementsFactory.CreateEvaluationGetEntityStatement(&query)
	}

	err := s.queryRow(txn, selectQuery, selectArgs...).Scan(queryArgs...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		s.logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	var evaluationJobEntity EvaluationJobEntity
	err = json.Unmarshal([]byte(query.EntityJSON), &evaluationJobEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, se.WithRollback(se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error()))
	}

	status := ""
	job, err := constructEvaluationResource(s.logger, &query, status, &evaluationJobEntity)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return job, nil
}

func (s *sqlStorage) GetEvaluationJobs(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	var txn *sql.Tx
	return listEntities[api.EvaluationJobResource](s, txn, shared.TableEvaluations, filter)
}

// DeleteEvaluationJob deletes the evaluation job identified by id.
func (s *sqlStorage) DeleteEvaluationJob(id string) error {
	return s.withTransaction("delete evaluation job", id, func(txn *sql.Tx) error {
		if _, err := s.getEvaluationJobTransactionalForUpdate(txn, id); err != nil {
			return err
		}

		deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TableEvaluations, id)
		result, err := s.exec(txn, deleteQuery, args...)
		if err != nil {
			s.logger.Error("Failed to delete evaluation job", "error", err, "id", id)
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}
		return s.evaluationJobDeleteResultError(id, result)
	})
}

// ProtectCollectionGenerationJobForCleanup cancels an active generation job, if
// necessary, and installs a bounded cleanup lease in one transaction. The lease
// protects an in-flight runtime cleanup from the retention sweep without changing
// the job's first-terminal expires_at deadline.
func (s *sqlStorage) ProtectCollectionGenerationJobForCleanup(id string, message *api.MessageInfo) (*api.EvaluationJobResource, error) {
	api.WithMessageOrigin(message, api.MessageOriginServer)
	var protected *api.EvaluationJobResource
	err := s.withTransaction("protect collection-generation job for cleanup", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdateWithType(txn, id, workloads.CollectionGeneration)
		if err != nil {
			return err
		}
		if job.Status == nil {
			return fmt.Errorf("collection-generation job %q has no status", id)
		}

		if !job.Status.State.IsTerminalState() {
			benchmarks := job.Status.Benchmarks
			now := time.Now()
			for i := range benchmarks {
				if !api.IsBenchmarkTerminalState(benchmarks[i].Status) {
					benchmarks[i].Status = api.StateCancelled
					benchmarks[i].ErrorMessage = message
					setBenchmarkCompletionTime(&benchmarks[i], now)
				}
			}
			entity := EvaluationJobEntity{
				Config: &job.EvaluationJobConfig,
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{
						State: api.OverallStateCancelled, Message: message,
					},
					Benchmarks:                   benchmarks,
					CollectionGenerationProgress: job.Status.CollectionGenerationProgress,
				},
				Results: job.Results,
			}
			if err := s.updateEvaluationJobTxn(txn, id, api.OverallStateCancelled, &entity); err != nil {
				return err
			}
			job.Status = entity.Status
		}

		protectedUntil := time.Now().UTC().Add(generation.CleanupProtectionLease)
		query, args := s.statementsFactory.CreateSetCollectionGenerationCleanupProtectionStatement(s.tenant, id, protectedUntil)
		result, err := s.exec(txn, query, args...)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}
		if rows == 0 {
			return se.WithRollback(se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id))
		}
		protected = job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return protected, nil
}

// DeleteCleanedCollectionGenerationJob finalizes a DELETE that has already
// cleaned runtime resources. The row may have expired or been removed by then.
func (s *sqlStorage) DeleteCleanedCollectionGenerationJob(id string) error {
	query, args := s.statementsFactory.CreateDeleteCleanedCollectionGenerationJobStatement(s.tenant, id)
	_, err := s.exec(nil, query, args...)
	return err
}

// DeleteExpiredCollectionGenerationJobs removes a bounded batch of terminal generation
// records whose server-assigned retention deadline has passed. The operation is global
// across tenants by design and is safe for concurrent service replicas.
func (s *sqlStorage) DeleteExpiredCollectionGenerationJobs(now time.Time, limit int) (int, error) {
	if limit < 1 {
		return 0, fmt.Errorf("cleanup batch limit must be positive")
	}
	query, args := s.statementsFactory.CreateDeleteExpiredGenerationJobsStatement(now.UTC(), limit)
	result, err := s.pool.ExecContext(s.ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete expired collection-generation jobs: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted collection-generation jobs: %w", err)
	}
	return int(count), nil
}

func (s *sqlStorage) evaluationJobDeleteResultError(id string, result sql.Result) error {
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		s.logger.Error("Failed to determine rows affected", "error", rowsErr, "id", id)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", rowsErr.Error()))
	}
	if rows == 0 {
		s.logger.Debug("Evaluation job not found", "id", id)
		return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
	}
	s.logger.Info("Deleted evaluation job", "id", id)
	return nil
}
