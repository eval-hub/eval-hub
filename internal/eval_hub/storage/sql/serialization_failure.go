package sql

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const serializationFailureMaxAttempts = 5

func isSerializationFailure(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "40001" {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 40001") ||
		strings.Contains(msg, "could not serialize access due to read/write dependencies among transactions")
}

func retryOnSerializationFailure(maxAttempts int, run func() error) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = run()
		if lastErr == nil {
			return nil
		}
		if !isSerializationFailure(lastErr) || attempt == maxAttempts {
			return lastErr
		}
	}
	return lastErr
}
