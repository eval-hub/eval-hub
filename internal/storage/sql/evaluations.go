package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	// import the postgres driver - "pgx"

	_ "github.com/jackc/pgx/v5/stdlib"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

//#######################################################################
// Evaluation job operations
//#######################################################################

// CreateEvaluationJob creates a new evaluation job in the database
// the evaluation job is stored in the evaluations table as a JSON string
// the evaluation job is returned as a EvaluationJobResource
// This should use transactions etc and requires cleaning up
func (s *SQLStorage) CreateEvaluationJob(executionContext *executioncontext.ExecutionContext, evaluation *api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
	tenant, err := s.getTenant(executionContext)
	if err != nil {
		return nil, err
	}
	evaluationJSON, err := json.Marshal(evaluation)
	if err != nil {
		return nil, err
	}
	addEntityStatement, err := createAddEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}
	jobID := s.generateID()
	executionContext.Logger.Info("Creating evaluation job", "id", jobID, "tenant", tenant, "status", api.StatePending)
	_, err = s.exec(executionContext.Ctx, addEntityStatement, jobID, tenant, api.StatePending, string(evaluationJSON))
	if err != nil {
		return nil, err
	}
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.Resource{
			ID:        jobID,
			Tenant:    api.Tenant(tenant),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		EvaluationJobConfig: *evaluation,
		Status: api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State:   api.StatePending,
				Message: "Evaluation job created",
			},
			Benchmarks: nil,
		},
		Results: nil,
	}
	return evaluationResource, nil
}

func (s *SQLStorage) GetEvaluationJob(ctx *executioncontext.ExecutionContext, id string) (*api.EvaluationJobResource, error) {
	// Build the SELECT query
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var statusStr string
	var entityJSON string

	err = s.pool.QueryRowContext(ctx.Ctx, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &entityJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		ctx.Logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationConfig api.EvaluationJobConfig
	err = json.Unmarshal([]byte(entityJSON), &evaluationConfig)
	if err != nil {
		ctx.Logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	// Parse status from database
	status := api.State(statusStr)

	// Construct the EvaluationJobResource
	// Note: Results and Benchmarks are initialized with defaults since they're not stored in the entity column
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.Resource{
			ID:        dbID,
			Tenant:    "TODO", // TODO: retrieve tenant from database or context
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		},
		EvaluationJobConfig: evaluationConfig,
		Status: api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State:   status,
				Message: "Evaluation job retrieved",
			},
			Benchmarks: nil, // TODO: retrieve benchmarks status from database
		},
		Results: nil, // TODO: retrieve results from database if needed
	}

	return evaluationResource, nil
}

func (s *SQLStorage) GetEvaluationJobs(ctx *executioncontext.ExecutionContext, limit int, offset int, statusFilter string) (*api.EvaluationJobResourceList, error) {
	// Get total count (with status filter if provided)
	countQuery, countArgs, err := createCountEntitiesStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, statusFilter)
	if err != nil {
		return nil, err
	}

	var totalCount int
	if len(countArgs) > 0 {
		err = s.pool.QueryRowContext(ctx.Ctx, countQuery, countArgs...).Scan(&totalCount)
	} else {
		err = s.pool.QueryRowContext(ctx.Ctx, countQuery).Scan(&totalCount)
	}
	if err != nil {
		ctx.Logger.Error("Failed to count evaluation jobs", "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	// Build the list query with pagination and status filter
	listQuery, listArgs, err := createListEntitiesStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, limit, offset, statusFilter)
	if err != nil {
		return nil, err
	}

	// Query the database
	rows, err := s.pool.QueryContext(ctx.Ctx, listQuery, listArgs...)
	if err != nil {
		ctx.Logger.Error("Failed to list evaluation jobs", "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}
	defer rows.Close()

	// Process rows
	var items []api.EvaluationJobResource
	for rows.Next() {
		var dbID string
		var createdAt, updatedAt time.Time
		var statusStr string
		var entityJSON string

		err = rows.Scan(&dbID, &createdAt, &updatedAt, &statusStr, &entityJSON)
		if err != nil {
			ctx.Logger.Error("Failed to scan evaluation job row", "error", err)
			return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", dbID, "Error", err.Error())
		}

		// Unmarshal the entity JSON into EvaluationJobConfig
		var evaluationConfig api.EvaluationJobConfig
		err = json.Unmarshal([]byte(entityJSON), &evaluationConfig)
		if err != nil {
			ctx.Logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
		}

		// Parse status from database
		status := api.State(statusStr)

		// Construct the EvaluationJobResource
		// Note: Results and Benchmarks are initialized with defaults since they're not stored in the entity column
		resource := api.EvaluationJobResource{
			Resource: api.Resource{
				ID:        dbID,
				Tenant:    "TODO", // TODO: retrieve tenant from database or context
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			EvaluationJobConfig: evaluationConfig,
			Status: api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State:   status,
					Message: "Evaluation job retrieved",
				},
				Benchmarks: nil, // TODO: retrieve benchmarks status from database
			},
		}

		items = append(items, resource)
	}

	if err = rows.Err(); err != nil {
		ctx.Logger.Error("Error iterating evaluation job rows", "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	// Calculate pagination info
	// Note: hrefs are left empty as they should be populated by the handler based on the request URL
	hasNext := offset+limit < totalCount
	var nextHref *api.HRef
	if hasNext {
		nextHref = &api.HRef{Href: ""} // Handler should populate this
	}

	return &api.EvaluationJobResourceList{
		Page: api.Page{
			First:      &api.HRef{Href: ""}, // Handler should populate this
			Next:       nextHref,
			Limit:      limit,
			TotalCount: totalCount,
		},
		Items: items,
	}, nil
}

func (s *SQLStorage) DeleteEvaluationJob(ctx *executioncontext.ExecutionContext, id string, hardDelete bool) error {
	if !hardDelete {
		return s.UpdateEvaluationJobStatus(ctx, id, &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State:   api.StateCancelled,
				Message: "Evaluation job cancelled",
			},
		})
	}

	// Build the DELETE query
	deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return err
	}

	// Execute the DELETE query
	_, err = s.exec(ctx.Ctx, deleteQuery, id)
	if err != nil {
		ctx.Logger.Error("Failed to delete evaluation job", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	/* TODO: remove this code? For now we don't do this because not all drivers support RowsAffected()
	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return NewStorageErrorWithError(err, "failed to get rows affected")
	}

	if rowsAffected == 0 {
		return NewStorageError("evaluation job with ID %s not found", id)
	}
	*/

	ctx.Logger.Info("Deleted evaluation job", "id", id, "hardDelete", hardDelete)
	return nil
}

func (s *SQLStorage) UpdateEvaluationJobStatus(ctx *executioncontext.ExecutionContext, id string, status *api.EvaluationJobStatus) error {
	// Build the UPDATE query
	updateQuery, err := createUpdateStatusStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return err
	}

	// TODO: For now this only handles the status update

	// Execute the UPDATE query
	statusStr := string(status.EvaluationJobState.State)
	_, err = s.exec(ctx.Ctx, updateQuery, statusStr, id)
	if err != nil {
		ctx.Logger.Error("Failed to update evaluation job status", "error", err, "id", id, "status", statusStr)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	/* TODO: remove this code? For now we don't do this because not all drivers support RowsAffected()
	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return NewStorageErrorWithError(err, "failed to get rows affected")
	}

	if rowsAffected == 0 {
		return NewStorageError("evaluation job with ID %s not found", id)
	}
	*/

	ctx.Logger.Info("Updated evaluation job status", "id", id, "status", statusStr)
	return nil
}
