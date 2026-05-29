package sql

import (
	"errors"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const serializationFailureMaxAttempts = 5

const (
	serializationRetryBaseDelay = 10 * time.Millisecond
	serializationRetryMaxDelay  = 250 * time.Millisecond
)

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

func serializationFailureBackoff(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	delay := serializationRetryBaseDelay << (attempt - 1)
	if delay > serializationRetryMaxDelay {
		delay = serializationRetryMaxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(delay/4 + 1)))
	return delay + jitter
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
		time.Sleep(serializationFailureBackoff(attempt))
	}
	return lastErr
}
