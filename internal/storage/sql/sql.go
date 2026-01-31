package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	// import the postgres driver - "pgx"
	"github.com/go-viper/mapstructure/v2"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	// These are the only drivers currently supported
	SQLITE_DRIVER   = "sqlite"
	POSTGRES_DRIVER = "pgx"

	// These are the only tables currently supported
	TABLE_EVALUATIONS = "evaluations"
	TABLE_COLLECTIONS = "collections"
)

type SQLStorage struct {
	sqlConfig *SQLDatabaseConfig
	pool      *sql.DB
}

func NewStorage(config map[string]any, logger *slog.Logger) (abstractions.Storage, error) {
	var sqlConfig SQLDatabaseConfig
	err := mapstructure.Decode(config, &sqlConfig)
	if err != nil {
		return nil, err
	}

	// check that the driver is supported
	switch sqlConfig.Driver {
	case SQLITE_DRIVER:
		break
	case POSTGRES_DRIVER:
		break
	default:
		return nil, getUnsupportedDriverError(sqlConfig.Driver)
	}

	logger.Info("Creating SQL storage", "driver", sqlConfig.Driver, "url", sqlConfig.URL)

	pool, err := sql.Open(sqlConfig.Driver, sqlConfig.URL)
	if err != nil {
		return nil, err
	}

	if sqlConfig.ConnMaxLifetime != nil {
		pool.SetConnMaxLifetime(*sqlConfig.ConnMaxLifetime)
	}
	if sqlConfig.MaxIdleConns != nil {
		pool.SetMaxIdleConns(*sqlConfig.MaxIdleConns)
	}
	if sqlConfig.MaxOpenConns != nil {
		pool.SetMaxOpenConns(*sqlConfig.MaxOpenConns)
	}

	storage := &SQLStorage{
		sqlConfig: &sqlConfig,
		pool:      pool,
	}

	// ping the database to verify the DSN provided by the user is valid and the server is accessible
	logger.Info("Pinging SQL storage", "driver", sqlConfig.Driver, "url", sqlConfig.URL)
	err = storage.Ping(1 * time.Second)
	if err != nil {
		return nil, err
	}

	// ensure the schemas are created
	logger.Info("Ensuring schemas are created", "driver", sqlConfig.Driver, "url", sqlConfig.URL)
	if err := storage.ensureSchema(); err != nil {
		return nil, err
	}

	return storage, nil
}

// Ping the database to verify DSN provided by the user is valid and the
// server accessible. If the ping fails exit the program with an error.
func (s *SQLStorage) Ping(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.pool.PingContext(ctx)
}

func (s *SQLStorage) GetDatasourceName() string {
	return s.sqlConfig.Driver
}

func (s *SQLStorage) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.pool.ExecContext(ctx, query, args...)
}

func (s *SQLStorage) ensureSchema() error {
	schemas, err := schemasForDriver(s.sqlConfig.Driver)
	if err != nil {
		return err
	}
	if _, err := s.exec(context.Background(), schemas); err != nil {
		return err
	}

	return nil
}

func (s *SQLStorage) getTenant(_ *executioncontext.ExecutionContext) (api.Tenant, error) {
	return "TODO", nil
}

func (s *SQLStorage) generateID() string {
	return uuid.New().String()
}

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
			return nil, serviceerrors.NewStorageErrorWithCode(404, "evaluation job with id '%s' not found", id)
		}
		ctx.Logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, serviceerrors.NewStorageErrorWithError(err, "failed to get evaluation job")
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationConfig api.EvaluationJobConfig
	err = json.Unmarshal([]byte(entityJSON), &evaluationConfig)
	if err != nil {
		ctx.Logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, serviceerrors.NewStorageErrorWithError(err, "failed to unmarshal evaluation job entity")
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
		return nil, serviceerrors.NewStorageErrorWithError(err, "failed to count evaluation jobs")
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
		return nil, serviceerrors.NewStorageErrorWithError(err, "failed to list evaluation jobs")
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
			return nil, serviceerrors.NewStorageErrorWithError(err, "failed to scan evaluation job row")
		}

		// Unmarshal the entity JSON into EvaluationJobConfig
		var evaluationConfig api.EvaluationJobConfig
		err = json.Unmarshal([]byte(entityJSON), &evaluationConfig)
		if err != nil {
			ctx.Logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, serviceerrors.NewStorageErrorWithError(err, "failed to unmarshal evaluation job entity")
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
		return nil, serviceerrors.NewStorageErrorWithError(err, "error iterating evaluation job rows")
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
	result, err := s.exec(ctx.Ctx, deleteQuery, id)
	if err != nil {
		ctx.Logger.Error("Failed to delete evaluation job", "error", err, "id", id)
		return serviceerrors.NewStorageErrorWithError(err, "failed to delete evaluation job")
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return serviceerrors.NewStorageErrorWithError(err, "failed to get rows affected")
	}

	if rowsAffected == 0 {
		return serviceerrors.NewStorageError("evaluation job with ID %s not found", id)
	}

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
	result, err := s.exec(ctx.Ctx, updateQuery, statusStr, id)
	if err != nil {
		ctx.Logger.Error("Failed to update evaluation job status", "error", err, "id", id, "status", statusStr)
		return serviceerrors.NewStorageErrorWithError(err, "failed to update evaluation job status")
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return serviceerrors.NewStorageErrorWithError(err, "failed to get rows affected")
	}

	if rowsAffected == 0 {
		return serviceerrors.NewStorageError("evaluation job with ID %s not found", id)
	}

	ctx.Logger.Info("Updated evaluation job status", "id", id, "status", statusStr)
	return nil
}

//#######################################################################
// Collection operations
//#######################################################################

func (s *SQLStorage) CreateCollection(ctx *executioncontext.ExecutionContext, collection *api.CollectionResource) error {
	return nil
}

func (s *SQLStorage) GetCollection(ctx *executioncontext.ExecutionContext, id string, summary bool) (*api.CollectionResource, error) {
	return nil, nil
}

func (s *SQLStorage) GetCollections(ctx *executioncontext.ExecutionContext, limit int, offset int) (*api.CollectionResourceList, error) {
	return nil, nil
}

func (s *SQLStorage) UpdateCollection(ctx *executioncontext.ExecutionContext, collection *api.CollectionResource) error {
	return nil
}

func (s *SQLStorage) DeleteCollection(ctx *executioncontext.ExecutionContext, id string) error {
	return nil
}

func (s *SQLStorage) Close() error {
	return s.pool.Close()
}
