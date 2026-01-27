package sql

import (
	"context"
	"database/sql"
	"encoding/json"
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

	logger.Info("Pinging SQL storage", "driver", sqlConfig.Driver, "url", sqlConfig.URL)
	err = storage.Ping(1 * time.Second)
	if err != nil {
		return nil, err
	}

	if err := storage.checkTables(&storage.sqlConfig.Evaluations); err != nil {
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

func (s *SQLStorage) exec(query string, args ...any) (sql.Result, error) {
	return s.pool.ExecContext(context.Background(), query, args...)
}

func (s *SQLStorage) checkTables(tableConfig *SQLTableConfig) error {

	if err := tableConfig.CheckConfig(); err != nil {
		return err
	}
	if _, err := s.exec(createEvaluationsTable()); err != nil {
		return err
	}

	if _, err := s.exec(createCollectionsTable()); err != nil {
		return err
	}

	return nil
}

// CreateEvaluationJob creates a new evaluation job in the database
// the evaluation job is stored in the evaluations table as a JSON string
// the evaluation job is returned as a EvaluationJobResource
// This should use transactions etc and requires cleaning up
func (s *SQLStorage) CreateEvaluationJob(executionContext *executioncontext.ExecutionContext, evaluation *api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {

	evaluationJSON, err := json.Marshal(evaluation)
	if err != nil {
		return nil, err
	}
	result, err := s.exec(createAddEntityStatement(), s.sqlConfig.Evaluations.TableName, string(evaluationJSON))
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
			Tenant:    "TODO",
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
	return nil, nil
}

func (s *SQLStorage) GetEvaluationJobs(ctx *executioncontext.ExecutionContext, summary bool, limit int, offset int, statusFilter string) (*api.EvaluationJobResourceList, error) {
	return nil, nil
}

func (s *SQLStorage) DeleteEvaluationJob(ctx *executioncontext.ExecutionContext, id string, hardDelete bool) error {
	return nil
}

func (s *SQLStorage) UpdateBenchmarkStatusForJob(ctx *executioncontext.ExecutionContext, id string, status api.BenchmarkStatus) error {
	return nil
}

func (s *SQLStorage) UpdateEvaluationJobStatus(ctx *executioncontext.ExecutionContext, id string, state api.EvaluationJobState) error {
	return nil
}

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
