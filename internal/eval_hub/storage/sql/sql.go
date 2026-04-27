package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	// import the postgres driver - "pgx"
	_ "github.com/jackc/pgx/v5/stdlib"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/postgres"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/sqlite"
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

type sqlStorage struct {
	sqlConfig         *shared.SQLDatabaseConfig
	statementsFactory shared.SQLStatementsFactory
	pool              *sql.DB
	logger            *slog.Logger
	ctx               context.Context
	tenant            api.Tenant
	owner             api.User
	maxArgLength      int
	isolationLevel    sql.IsolationLevel
}

func NewStorage(
	config map[string]any,
	systemCollections map[string]api.CollectionResource,
	systemProviders map[string]api.ProviderResource,
	otelEnabled bool,
	logger *slog.Logger,
) (abstractions.Storage, error) {
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

	logger = logger.With("driver", sqlConfig.GetDriverName())
	databaseName := sqlConfig.GetDatabaseName()
	if databaseName != "" {
		logger = logger.With("database", databaseName)
	}

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

	isolationLevel, err := getIsolationLevel(&sqlConfig, logger)
	if err != nil {
		return nil, err
	}

	s := &sqlStorage{
		sqlConfig:         &sqlConfig,
		statementsFactory: statementsFactory,
		pool:              pool,
		logger:            logger,
		ctx:               context.Background(),
		maxArgLength:      512,
		isolationLevel:    isolationLevel,
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

	// load any system resources
	if err := s.LoadSystemResources(systemCollections, systemProviders); err != nil {
		return nil, err
	}

	success = true
	return s, nil
}

func getIsolationLevel(config *shared.SQLDatabaseConfig, logger *slog.Logger) (sql.IsolationLevel, error) {
	// for testing purposes only
	isolationLevel := strings.TrimSpace(os.Getenv("DEBUG_SQL_ISOLATION_LEVEL"))
	if isolationLevel != "" {
		levels := []sql.IsolationLevel{
			sql.LevelDefault,
			sql.LevelReadUncommitted,
			sql.LevelReadCommitted,
			sql.LevelWriteCommitted,
			sql.LevelRepeatableRead,
			sql.LevelSnapshot,
			sql.LevelSerializable,
			sql.LevelLinearizable,
		}
		for _, level := range levels {
			if strings.EqualFold(isolationLevel, level.String()) {
				return level, nil
			}
		}
		logger.Error("Invalid isolation level", "isolation_level", isolationLevel)
		return sql.LevelDefault, fmt.Errorf("invalid isolation level: %s", isolationLevel)
	}

	switch config.Driver {
	case SQLITE_DRIVER:
		return sql.LevelDefault, nil
	case POSTGRES_DRIVER:
		return sql.LevelSerializable, nil
	default:
		return sql.LevelDefault, nil
	}
}

// Ping the database to verify DSN provided by the user is valid and the
// server accessible. If the ping fails exit the program with an error.
func (s *sqlStorage) Ping(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.pool.PingContext(ctx)
}

func (s *sqlStorage) safeArg(arg any) any {
	if arg == nil {
		return nil
	}
	if v, ok := arg.(string); ok && len(v) > s.maxArgLength {
		return v[:s.maxArgLength] + "..."
	}
	return arg
}

func (s *sqlStorage) safeArgs(args []any) []any {
	newArgs := make([]any, len(args))
	for i, arg := range args {
		newArg := s.safeArg(arg)
		if newArg != nil {
			newArgs[i] = newArg
		}
	}
	return newArgs
}

func (s *sqlStorage) exec(txn *sql.Tx, query string, args ...any) (sql.Result, error) {
	s.logger.Debug("Executing exec", "transaction", txn != nil, "query", query, "args", s.safeArgs(args))

	if txn != nil {
		return txn.ExecContext(s.ctx, query, args...)
	} else {
		return s.pool.ExecContext(s.ctx, query, args...)
	}
}

func (s *sqlStorage) query(txn *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	s.logger.Debug("Executing query", "transaction", txn != nil, "query", query, "args", s.safeArgs(args))

	if txn != nil {
		return txn.QueryContext(s.ctx, query, args...)
	} else {
		return s.pool.QueryContext(s.ctx, query, args...)
	}
}

func (s *sqlStorage) queryRow(txn *sql.Tx, query string, args ...any) *sql.Row {
	s.logger.Debug("Executing row query", "transaction", txn != nil, "query", query, "args", s.safeArgs(args))

	if txn != nil {
		return txn.QueryRowContext(s.ctx, query, args...)
	} else {
		return s.pool.QueryRowContext(s.ctx, query, args...)
	}
}

func (s *sqlStorage) getTotalCount(txn *sql.Tx, tenant api.Tenant, tableName string, params map[string]any, typeName string) (int, error) {
	countQuery, countArgs := s.statementsFactory.CreateCountEntitiesStatement(tenant, tableName, params)

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

func (s *sqlStorage) ensureSchema() error {
	schemas := s.statementsFactory.GetTablesSchema()
	if _, err := s.exec(nil, schemas); err != nil {
		return err
	}

	return nil
}

// isVisibleResource checks if a resource is visible to the current tenant.
// A system resource is always visible, a user resource is visible if the tenant_id matches.
func (s *sqlStorage) isVisibleResource(resource *api.Resource) bool {
	// now check that the tenant_id matches and owner matches the system owner
	if !resource.IsSystemResource() {
		// this is a user resource so check the tenant_id matches
		if resource.Tenant.String() != s.tenant.String() {
			s.logger.Debug("Tenant mismatch for resource", "id", resource.ID, "resource_tenant", resource.Tenant, "resource", s.prettyPrint(resource))
			return false
		}
	}
	return true
}

func (s *sqlStorage) prettyPrint(v any) string {
	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		jsonBytes = []byte(fmt.Sprintf("%v", v))
	}
	return string(jsonBytes[:min(len(jsonBytes), s.maxArgLength)])
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

func (s *sqlStorage) Close() error {
	return s.pool.Close()
}

func (s *sqlStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &sqlStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            logger,
		ctx:               s.ctx,
		tenant:            s.tenant,
		owner:             s.owner,
		maxArgLength:      s.maxArgLength,
		isolationLevel:    s.isolationLevel,
	}
}

func (s *sqlStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &sqlStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            s.logger,
		ctx:               ctx,
		tenant:            s.tenant,
		owner:             s.owner,
		maxArgLength:      s.maxArgLength,
		isolationLevel:    s.isolationLevel,
	}
}

func (s *sqlStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &sqlStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            s.logger,
		ctx:               s.ctx,
		tenant:            tenant,
		owner:             s.owner,
		maxArgLength:      s.maxArgLength,
		isolationLevel:    s.isolationLevel,
	}
}

func (s *sqlStorage) WithOwner(owner api.User) abstractions.Storage {
	return &sqlStorage{
		sqlConfig:         s.sqlConfig,
		statementsFactory: s.statementsFactory,
		pool:              s.pool,
		logger:            s.logger,
		ctx:               s.ctx,
		tenant:            s.tenant,
		owner:             owner,
		maxArgLength:      s.maxArgLength,
		isolationLevel:    s.isolationLevel,
	}
}
