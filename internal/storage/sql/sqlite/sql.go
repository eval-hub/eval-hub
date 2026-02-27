package sqlite

import (
	"context"

	db "database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/storage/sql"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-viper/mapstructure/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
)

const (
	TABLE_EVALUATIONS = "evaluations"
	TABLE_COLLECTIONS = "collections"
	TABLE_PROVIDERS   = "providers"
)

type SQLiteStorage struct {
	sqlConfig *sql.SQLDatabaseConfig
	pool      *db.DB
	logger    *slog.Logger
	ctx       context.Context
	tenant    api.Tenant
}

func NewStorage(config map[string]any, otelEnabled bool, logger *slog.Logger) (abstractions.Storage, error) {
	var sqlConfig sql.SQLDatabaseConfig
	merr := mapstructure.Decode(config, &sqlConfig)
	if merr != nil {
		return nil, merr
	}

	databaseName := sqlConfig.GetDatabaseName()
	logger = logger.With("driver", sqlConfig.GetDriverName(), "database", databaseName)

	logger.Info("Creating SQLite storage")

	var pool *db.DB
	var err error
	if otelEnabled {
		var attrs []attribute.KeyValue
		attrs = append(attrs, semconv.DBSystemSqlite)
		if databaseName != "" {
			attrs = append(attrs, semconv.DBNameKey.String(databaseName))
		}
		pool, err = otelsql.Open(sqlConfig.Driver, sqlConfig.URL, otelsql.WithAttributes(attrs...))
	} else {
		pool, err = db.Open(sqlConfig.Driver, sqlConfig.URL)
	}
	if err != nil {
		return nil, err
	}

	success := false
	defer func() {
		if !success {
			pool.Close()
		}
	}()

	// SQLite only supports one writer at a time; serializing access through
	// a single connection eliminates lock contention and deadlocks.
	pool.SetMaxOpenConns(1)
	if _, err := pool.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}
	// Enable WAL mode for file-based databases (in-memory databases don't
	// support WAL and always return journal_mode="memory").
	if !strings.Contains(sqlConfig.URL, "mode=memory") {
		var mode string
		if err := pool.QueryRow("PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
			return nil, fmt.Errorf("failed to set journal_mode: %w", err)
		}
		if mode != "wal" {
			return nil, fmt.Errorf("failed to enable WAL mode: database returned journal_mode=%q", mode)
		}
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

	s := &SQLiteStorage{
		sqlConfig: &sqlConfig,
		pool:      pool,
		logger:    logger,
		ctx:       context.Background(),
	}

	// ping the database to verify the DSN provided by the user is valid and the server is accessible
	logger.Info("Pinging SQL storage")
	err = s.Ping(1 * time.Second)
	if err != nil {
		return nil, err
	}

	// ensure the schemas are created
	logger.Info("Ensuring schemas are created")
	if err := s.ensureSchema(); err != nil {
		return nil, err
	}

	success = true
	return s, nil
}

// Ping the database to verify DSN provided by the user is valid and the
// server accessible. If the ping fails exit the program with an error.
func (s *SQLiteStorage) Ping(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.pool.PingContext(ctx)
}

func (s *SQLiteStorage) ensureSchema() error {
	schemas := SCHEMA

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	if _, err := e.Exec(schemas); err != nil {
		return err
	}

	return nil
}

func (s *SQLiteStorage) Close() error {
	return s.pool.Close()
}

func (s *SQLiteStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &SQLiteStorage{
		sqlConfig: s.sqlConfig,
		pool:      s.pool,
		logger:    logger,
		ctx:       s.ctx,
		tenant:    s.tenant,
	}
}

func (s *SQLiteStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &SQLiteStorage{
		sqlConfig: s.sqlConfig,
		pool:      s.pool,
		logger:    s.logger,
		ctx:       ctx,
		tenant:    s.tenant,
	}
}

func (s *SQLiteStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &SQLiteStorage{
		sqlConfig: s.sqlConfig,
		pool:      s.pool,
		logger:    s.logger,
		ctx:       s.ctx,
		tenant:    tenant,
	}
}
