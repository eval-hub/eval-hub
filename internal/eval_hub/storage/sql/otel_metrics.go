package sql

import (
	"context"
	"database/sql"

	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// dbClientMeterScope is the OTEL instrumentation scope for the semconv
// db.client.connection.* pool metrics registered alongside otelsql's
// legacy go.sql.* instruments (see registerDBClientSemconvMetrics).
const dbClientMeterScope = "github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql"

// defaultMaxIdleConns mirrors database/sql's documented default (2) used
// when SQLDatabaseConfig.MaxIdleConns is not set.
const defaultMaxIdleConns = 2

// dbSystemName maps a supported driver name to its semconv v1.39.0
// db.system.name attribute value.
func dbSystemName(driver string) attribute.KeyValue {
	switch driver {
	case SQLiteDriver:
		return semconv.DBSystemNameSQLite
	case PostgresDriver:
		return semconv.DBSystemNamePostgreSQL
	default:
		return semconv.DBSystemNameOtherSQL
	}
}

// effectiveMaxIdleConns returns the configured max idle connections, or
// database/sql's default (2) when unset, matching the pool's actual
// behavior (see database/sql.DB.SetMaxIdleConns docs).
func effectiveMaxIdleConns(sqlConfig *shared.SQLDatabaseConfig) int {
	if sqlConfig.MaxIdleConns != nil {
		return *sqlConfig.MaxIdleConns
	}
	return defaultMaxIdleConns
}

// registerDBClientSemconvMetrics registers OTEL semconv v1.39.0
// "db.client.connection.*" pool metrics (db.client.connection.count,
// db.client.connection.max, db.client.connection.idle.max) sourced from
// sql.DB.Stats(), in addition to otelsql's legacy go.sql.* instruments.
//
// These are additive: they do not replace or rename the existing
// otelsql-reported go.sql.* metrics, which remain for backward
// compatibility with existing dashboards/alerts.
func registerDBClientSemconvMetrics(pool *sql.DB, sqlConfig *shared.SQLDatabaseConfig, poolName string) error {
	meter := otel.Meter(dbClientMeterScope)

	connCount, err := meter.Int64ObservableUpDownCounter(
		"db.client.connection.count",
		metric.WithDescription("The number of connections that are currently in state described by the state attribute."),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return err
	}

	connMax, err := meter.Int64ObservableUpDownCounter(
		"db.client.connection.max",
		metric.WithDescription("The maximum number of open connections allowed."),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return err
	}

	connIdleMax, err := meter.Int64ObservableUpDownCounter(
		"db.client.connection.idle.max",
		metric.WithDescription("The maximum number of idle open connections allowed."),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return err
	}

	baseAttrs := []attribute.KeyValue{
		dbSystemName(sqlConfig.Driver),
		semconv.DBClientConnectionPoolName(poolName),
	}
	if databaseName := sqlConfig.GetDatabaseName(); databaseName != "" {
		baseAttrs = append(baseAttrs, semconv.DBNamespace(databaseName))
	}
	// Snapshot at registration time; database/sql does not expose the
	// configured max-idle as a live stat, so if SetMaxIdleConns is called
	// after storage init this value will be stale.
	idleMax := int64(effectiveMaxIdleConns(sqlConfig))

	usedAttrs := append(append([]attribute.KeyValue{}, baseAttrs...), semconv.DBClientConnectionStateUsed)
	idleAttrs := append(append([]attribute.KeyValue{}, baseAttrs...), semconv.DBClientConnectionStateIdle)

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		stats := pool.Stats()

		o.ObserveInt64(connCount, int64(stats.InUse), metric.WithAttributes(usedAttrs...))
		o.ObserveInt64(connCount, int64(stats.Idle), metric.WithAttributes(idleAttrs...))
		o.ObserveInt64(connMax, int64(stats.MaxOpenConnections), metric.WithAttributes(baseAttrs...))
		o.ObserveInt64(connIdleMax, idleMax, metric.WithAttributes(baseAttrs...))
		return nil
	}, connCount, connMax, connIdleMax)
	return err
}
