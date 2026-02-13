package sql

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-viper/mapstructure/v2"
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
	logger    *slog.Logger
	ctx       context.Context
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

	logger = logger.With("driver", sqlConfig.getDriverName(), "url", sqlConfig.getConnectionURL())

	logger.Info("Creating SQL storage")

	if sqlConfig.Driver == POSTGRES_DRIVER {
		if err := ensurePostgresDatabaseExists(context.Background(), logger, sqlConfig.URL); err != nil {
			return nil, err
		}
	}

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

	s := &SQLStorage{
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

func (s *SQLStorage) getTenant() (api.Tenant, error) {
	return "TODO", nil
}

func (s *SQLStorage) Close() error {
	return s.pool.Close()
}

func (s *SQLStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &SQLStorage{
		sqlConfig: s.sqlConfig,
		pool:      s.pool,
		logger:    logger,
		ctx:       s.ctx,
	}
}

func (s *SQLStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &SQLStorage{
		sqlConfig: s.sqlConfig,
		pool:      s.pool,
		logger:    s.logger,
		ctx:       ctx,
	}
}

func ensurePostgresDatabaseExists(ctx context.Context, logger *slog.Logger, connURL string) error {
	if !strings.Contains(connURL, "://") {
		logger.Warn("Postgres URL is not in URL form; skipping auto-create")
		return nil
	}

	parsed, err := url.Parse(connURL)
	if err != nil {
		return fmt.Errorf("parse postgres url: %w", err)
	}

	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" || dbName == "postgres" {
		return nil
	}

	adminURL := *parsed
	adminURL.Path = "/postgres"

	adminDB, err := sql.Open(POSTGRES_DRIVER, adminURL.String())
	if err != nil {
		return fmt.Errorf("open postgres admin connection: %w", err)
	}
	defer adminDB.Close()

	if err := adminDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres admin database: %w", err)
	}

	var exists bool
	row := adminDB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName)
	if err := row.Scan(&exists); err != nil {
		return fmt.Errorf("check postgres database existence: %w", err)
	}
	if exists {
		return nil
	}

	logger.Info("Postgres database does not exist; creating", "database", dbName)

	owner := ""
	password := ""
	if parsed.User != nil {
		owner = parsed.User.Username()
		if pass, ok := parsed.User.Password(); ok {
			password = pass
		}
	}

	if err := ensurePostgresRoleExists(ctx, logger, adminDB, owner, password); err != nil {
		return err
	}

	var createSQL string
	if owner != "" {
		createSQL = fmt.Sprintf("CREATE DATABASE %s OWNER %s", quoteIdentifier(POSTGRES_DRIVER, dbName), quoteIdentifier(POSTGRES_DRIVER, owner))
	} else {
		createSQL = fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(POSTGRES_DRIVER, dbName))
	}

	if _, err := adminDB.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create postgres database: %w", err)
	}

	return nil
}

func ensurePostgresRoleExists(ctx context.Context, logger *slog.Logger, adminDB *sql.DB, owner string, password string) error {
	if owner == "" {
		return nil
	}

	var exists bool
	row := adminDB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", owner)
	if err := row.Scan(&exists); err != nil {
		return fmt.Errorf("check postgres role existence: %w", err)
	}
	if exists {
		return nil
	}

	if password == "" {
		logger.Warn("Postgres role does not exist and no password provided; skipping role creation", "role", owner)
		return nil
	}

	logger.Info("Postgres role does not exist; creating", "role", owner)
	createSQL := fmt.Sprintf(
		"CREATE USER %s WITH PASSWORD %s",
		quoteIdentifier(POSTGRES_DRIVER, owner),
		quoteLiteral(password),
	)
	if _, err := adminDB.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create postgres role: %w", err)
	}

	return nil
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
