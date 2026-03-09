package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	// import the postgres driver - "pgx"
	_ "github.com/jackc/pgx/v5/stdlib"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
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
	sqlConfig             *shared.SQLDatabaseConfig
	statementsFactory     shared.SQLStatementsFactory
	pool                  *sql.DB
	logger                *slog.Logger
	ctx                   context.Context
	tenant                api.Tenant
	owner                 api.User
	authenticationEnabled bool
}

func NewStorage(config map[string]any, otelEnabled bool, authenticationEnabled bool, logger *slog.Logger) (abstractions.Storage, error) {
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
		return nil, fmt.Errorf("unsupported driver: %s", (sqlConfig.Driver))
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
		statementsFactory, err = sqlite.Setup(logger, pool, &sqlConfig)
		if err != nil {
			return nil, err
		}
	case POSTGRES_DRIVER:
		statementsFactory, err = postgres.Setup(logger, pool, &sqlConfig)
		if err != nil {
			return nil, err
		}
	}

	s := &SQLStorage{
		sqlConfig:             &sqlConfig,
		statementsFactory:     statementsFactory,
		pool:                  pool,
		logger:                logger,
		ctx:                   context.Background(),
		authenticationEnabled: authenticationEnabled,
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

func (s *SQLStorage) getTotalCount(txn *sql.Tx, tableName string, params map[string]any, typeName string) (int, error) {
	countQuery, countArgs := s.statementsFactory.CreateCountEntitiesStatement(s.tenant, tableName, params)

	var totalCount int
	var err error
	if len(countArgs) > 0 {
		err = s.queryRow(txn, countQuery, countArgs...).Scan(&totalCount)
	} else {
		err = s.queryRow(txn, countQuery).Scan(&totalCount)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		s.logger.Error(fmt.Sprintf("Failed to count %s", typeName), "error", err)
		return 0, se.NewServiceError(messages.QueryFailed, "Type", typeName, "Error", err.Error())
	}
	return totalCount, nil
}

func (s *SQLStorage) ensureSchema() error {
	schemas := s.statementsFactory.GetTablesSchema()
	if _, err := s.exec(nil, schemas); err != nil {
		return err
	}

	return nil
}

func (s *SQLStorage) verifyTenant() error {
	if s.authenticationEnabled && s.tenant == "" {
		return se.NewServiceError(messages.Unauthorized, "Error", "Tenant is required")
	}
	return nil
}

func applyPatches(resource string, patches *api.Patch) ([]byte, error) {
	if patches == nil || len(*patches) == 0 {
		return []byte(resource), nil
	}
	patchesJSON, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(patchesJSON)
	if err != nil {
		return nil, err
	}
	return patch.Apply([]byte(resource))
}

func (s *SQLStorage) Close() error {
	return s.pool.Close()
}

func (s *SQLStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:             s.sqlConfig,
		statementsFactory:     s.statementsFactory,
		pool:                  s.pool,
		logger:                logger,
		ctx:                   s.ctx,
		tenant:                s.tenant,
		owner:                 s.owner,
		authenticationEnabled: s.authenticationEnabled,
	}
}

func (s *SQLStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:             s.sqlConfig,
		statementsFactory:     s.statementsFactory,
		pool:                  s.pool,
		logger:                s.logger,
		ctx:                   ctx,
		tenant:                s.tenant,
		owner:                 s.owner,
		authenticationEnabled: s.authenticationEnabled,
	}
}

func (s *SQLStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:             s.sqlConfig,
		statementsFactory:     s.statementsFactory,
		pool:                  s.pool,
		logger:                s.logger,
		ctx:                   s.ctx,
		tenant:                tenant,
		owner:                 s.owner,
		authenticationEnabled: s.authenticationEnabled,
	}
}

func (s *SQLStorage) WithOwner(owner api.User) abstractions.Storage {
	return &SQLStorage{
		sqlConfig:             s.sqlConfig,
		statementsFactory:     s.statementsFactory,
		pool:                  s.pool,
		logger:                s.logger,
		ctx:                   s.ctx,
		tenant:                s.tenant,
		owner:                 owner,
		authenticationEnabled: s.authenticationEnabled,
	}
}
