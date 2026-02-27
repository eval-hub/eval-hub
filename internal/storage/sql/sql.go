package sql

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	// import the postgres driver - "pgx"
	_ "github.com/jackc/pgx/v5/stdlib"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/storage/sql/postgres"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/internal/storage/sql/sqlite"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-viper/mapstructure/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
)

const (
	// These are the only drivers currently supported
	SQLITE_DRIVER   = "sqlite"
	POSTGRES_DRIVER = "pgx"
)

type SQLStorage struct {
	sqlConfig         *shared.SQLDatabaseConfig
	statementsFactory shared.SQLStatementsFactory
	pool              *sql.DB
	logger            *slog.Logger
	ctx               context.Context
	tenant            api.Tenant
}

func NewStorage(config map[string]any, otelEnabled bool, logger *slog.Logger) (abstractions.Storage, error) {
	var sqlConfig shared.SQLDatabaseConfig
	merr := mapstructure.Decode(config, &sqlConfig)
	if merr != nil {
		return nil, merr
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

	databaseName := sqlConfig.GetDatabaseName()
	logger = logger.With("driver", sqlConfig.GetDriverName(), "database", databaseName)

	logger.Info("Creating SQL storage")

	var pool *sql.DB
	var err error
	if otelEnabled {
		var attrs []attribute.KeyValue
		switch sqlConfig.Driver {
		case SQLITE_DRIVER:
			attrs = append(attrs, semconv.DBSystemSqlite)
		case POSTGRES_DRIVER:
			attrs = append(attrs, semconv.DBSystemPostgreSQL)
		}
		if databaseName != "" {
			attrs = append(attrs, semconv.DBNameKey.String(databaseName))
		}
		pool, err = otelsql.Open(sqlConfig.Driver, sqlConfig.URL, otelsql.WithAttributes(attrs...))
	} else {
		pool, err = sql.Open(sqlConfig.Driver, sqlConfig.URL)
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

	if sqlConfig.ConnMaxLifetime != nil {
		pool.SetConnMaxLifetime(*sqlConfig.ConnMaxLifetime)
	}
	if sqlConfig.MaxIdleConns != nil {
		pool.SetMaxIdleConns(*sqlConfig.MaxIdleConns)
	}
	if sqlConfig.MaxOpenConns != nil {
		pool.SetMaxOpenConns(*sqlConfig.MaxOpenConns)
	}

	var statementsFactory shared.SQLStatementsFactory
	switch sqlConfig.Driver {
	case SQLITE_DRIVER:
		statementsFactory, err = sqlite.Setup(pool, &sqlConfig)
		if err != nil {
			return nil, err
		}
	case POSTGRES_DRIVER:
		statementsFactory, err = postgres.Setup(pool, &sqlConfig)
		if err != nil {
			return nil, err
		}
	}

	s := &SQLStorage{
		sqlConfig:         &sqlConfig,
		statementsFactory: statementsFactory,
		pool:              pool,
		logger:            logger,
		ctx:               context.Background(),
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
func (s *SQLStorage) Ping(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.pool.PingContext(ctx)
}

func (s *SQLStorage) exec(txn *sql.Tx, query string, args ...any) (sql.Result, error) {
	if txn != nil {
		return txn.ExecContext(s.ctx, query, args...)
	} else {
		return s.pool.ExecContext(s.ctx, query, args...)
	}
}

func (s *SQLStorage) query(txn *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	if txn != nil {
		return txn.QueryContext(s.ctx, query, args...)
	} else {
		return s.pool.QueryContext(s.ctx, query, args...)
	}
}

func (s *SQLStorage) queryRow(txn *sql.Tx, query string, args ...any) *sql.Row {
	if txn != nil {
		return txn.QueryRowContext(s.ctx, query, args...)
	} else {
		return s.pool.QueryRowContext(s.ctx, query, args...)
	}
}

func (s *SQLStorage) ensureSchema() error {
	schemas, err := schemasForDriver(s.sqlConfig.Driver)
	if err != nil {
		return err
	}
	if _, err := s.exec(nil, schemas); err != nil {
		return err
	}

	return nil
}

func (s *SQLStorage) Close() error {
	return s.pool.Close()
}

func (s *SQLStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            logger,
		ctx:               s.ctx,
		tenant:            s.tenant,
	}
}

func (s *SQLStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            s.logger,
		ctx:               ctx,
		tenant:            s.tenant,
	}
}

func (s *SQLStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            s.logger,
		ctx:               s.ctx,
		tenant:            tenant,
	}
}
