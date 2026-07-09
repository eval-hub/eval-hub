package sql

import (
	"crypto/rand"
	"errors"
	"math/big"
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
	jitter, err := serializationRetryJitter(delay)
	if err != nil {
		return delay
	}
	return delay + jitter
}

func serializationRetryJitter(delay time.Duration) (time.Duration, error) {
	max := int64(delay/4 + 1)
	if max <= 0 {
		return 0, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0, err
	}
	return time.Duration(n.Int64()), nil
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
