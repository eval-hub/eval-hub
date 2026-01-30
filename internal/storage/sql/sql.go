package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	// import the postgres driver - "pgx"
	"github.com/go-viper/mapstructure/v2"
	_ "github.com/jackc/pgx/v5/stdlib"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
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
	result, err := s.exec(executionContext.Ctx, addEntityStatement, tenant, api.StatePending, string(evaluationJSON))
	if err != nil {
		return nil, err
	}
	evaluationID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.Resource{
			ID:        strconv.FormatInt(evaluationID, 10),
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

func (s *SQLStorage) getEvaluationJobID(id string) (int64, error) {
	// Parse the ID string to int64
	if evaluationId, err := strconv.ParseInt(id, 10, 64); err != nil {
		return 0, fmt.Errorf("Invalid evaluation job ID: %w", err)
	} else {
		return evaluationId, nil
	}
}

func (s *SQLStorage) GetEvaluationJob(ctx *executioncontext.ExecutionContext, id string) (*api.EvaluationJobResource, error) {
	evaluationID, err := s.getEvaluationJobID(id)
	if err != nil {
		return nil, err
	}

	// Build the SELECT query
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}

	// Query the database
	var dbID int64
	var createdAt, updatedAt time.Time
	var statusStr string
	var entityJSON string

	err = s.pool.QueryRowContext(ctx.Ctx, selectQuery, evaluationID).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &entityJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("evaluation job with ID %s not found", id)
		}
		ctx.Logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, fmt.Errorf("failed to get evaluation job: %w", err)
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationConfig api.EvaluationJobConfig
	err = json.Unmarshal([]byte(entityJSON), &evaluationConfig)
	if err != nil {
		ctx.Logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, fmt.Errorf("failed to unmarshal evaluation job entity: %w", err)
	}

	// Parse status from database
	status := api.State(statusStr)

	// Construct the EvaluationJobResource
	// Note: Results and Benchmarks are initialized with defaults since they're not stored in the entity column
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.Resource{
			ID:        strconv.FormatInt(dbID, 10),
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
		return nil, fmt.Errorf("failed to count evaluation jobs: %w", err)
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
		return nil, fmt.Errorf("failed to list evaluation jobs: %w", err)
	}
	defer rows.Close()

	// Process rows
	var items []api.EvaluationJobResource
	for rows.Next() {
		var dbID int64
		var createdAt, updatedAt time.Time
		var statusStr string
		var entityJSON string

		err = rows.Scan(&dbID, &createdAt, &updatedAt, &statusStr, &entityJSON)
		if err != nil {
			ctx.Logger.Error("Failed to scan evaluation job row", "error", err)
			return nil, fmt.Errorf("failed to scan evaluation job row: %w", err)
		}

		// Unmarshal the entity JSON into EvaluationJobConfig
		var evaluationConfig api.EvaluationJobConfig
		err = json.Unmarshal([]byte(entityJSON), &evaluationConfig)
		if err != nil {
			ctx.Logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, fmt.Errorf("failed to unmarshal evaluation job entity: %w", err)
		}

		// Parse status from database
		status := api.State(statusStr)

		// Construct the EvaluationJobResource
		// Note: Results and Benchmarks are initialized with defaults since they're not stored in the entity column
		resource := api.EvaluationJobResource{
			Resource: api.Resource{
				ID:        strconv.FormatInt(dbID, 10),
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
		return nil, fmt.Errorf("error iterating evaluation job rows: %w", err)
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

	evaluationID, err := s.getEvaluationJobID(id)
	if err != nil {
		return err
	}

	// Build the DELETE query
	deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return err
	}

	// Execute the DELETE query
	result, err := s.exec(ctx.Ctx, deleteQuery, evaluationID)
	if err != nil {
		ctx.Logger.Error("Failed to delete evaluation job", "error", err, "id", id)
		return fmt.Errorf("failed to delete evaluation job: %w", err)
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("evaluation job with ID %s not found", id)
	}

	ctx.Logger.Info("Deleted evaluation job", "id", id, "hardDelete", hardDelete)
	return nil
}

func (s *SQLStorage) UpdateEvaluationJobStatus(ctx *executioncontext.ExecutionContext, id string, status *api.EvaluationJobStatus) error {
	evaluationID, err := s.getEvaluationJobID(id)
	if err != nil {
		return err
	}

	// Build the UPDATE query
	updateQuery, err := createUpdateStatusStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return err
	}

	// TODO: For now this only handles the status update

	// Execute the UPDATE query
	statusStr := string(status.EvaluationJobState.State)
	result, err := s.exec(ctx.Ctx, updateQuery, statusStr, evaluationID)
	if err != nil {
		ctx.Logger.Error("Failed to update evaluation job status", "error", err, "id", id, "status", statusStr)
		return fmt.Errorf("failed to update evaluation job status: %w", err)
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("evaluation job with ID %s not found", id)
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
