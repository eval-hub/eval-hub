package sql

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsSerializationFailure(t *testing.T) {
	t.Parallel()

	pgErr := &pgconn.PgError{Code: "40001", Message: "could not serialize access due to read/write dependencies among transactions"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "pg 40001", err: pgErr, want: true},
		{name: "wrapped pg 40001", err: fmt.Errorf("commit failed: %w", pgErr), want: true},
		{name: "other pg code", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "generic error", err: errors.New("connection reset"), want: false},
		{
			name: "service error with sqlstate text",
			err: serviceerrors.NewServiceError(
				messages.DatabaseOperationFailed,
				"Type", "commit transaction update evaluation job",
				"ResourceId", "job-1",
				"Error", "ERROR: could not serialize access due to read/write dependencies among transactions (SQLSTATE 40001)",
			),
			want: true,
		},
		{
			name: "service error unrelated",
			err: serviceerrors.NewServiceError(
				messages.DatabaseOperationFailed,
				"Type", "commit transaction update evaluation job",
				"ResourceId", "job-1",
				"Error", "connection refused",
			),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSerializationFailure(tt.err); got != tt.want {
				t.Fatalf("isSerializationFailure() = %v, want %v (err=%v)", got, tt.want, tt.err)
			}
		})
	}
}

func TestRetryOnSerializationFailure(t *testing.T) {
	t.Parallel()

	serializationErr := &pgconn.PgError{Code: "40001", Message: "could not serialize access"}
	otherErr := errors.New("permanent failure")

	t.Run("succeeds first attempt", func(t *testing.T) {
		attempts := 0
		err := retryOnSerializationFailure(3, func() error {
			attempts++
			return nil
		})
		if err != nil {
			t.Fatalf("retryOnSerializationFailure() = %v, want nil", err)
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
	})

	t.Run("retries serialization failure then succeeds", func(t *testing.T) {
		attempts := 0
		err := retryOnSerializationFailure(3, func() error {
			attempts++
			if attempts < 3 {
				return serializationErr
			}
			return nil
		})
		if err != nil {
			t.Fatalf("retryOnSerializationFailure() = %v, want nil", err)
		}
		if attempts != 3 {
			t.Fatalf("attempts = %d, want 3", attempts)
		}
	})

	t.Run("stops after max attempts", func(t *testing.T) {
		attempts := 0
		err := retryOnSerializationFailure(3, func() error {
			attempts++
			return serializationErr
		})
		if !errors.Is(err, serializationErr) {
			t.Fatalf("retryOnSerializationFailure() = %v, want serialization error", err)
		}
		if attempts != 3 {
			t.Fatalf("attempts = %d, want 3", attempts)
		}
	})

	t.Run("does not retry other errors", func(t *testing.T) {
		attempts := 0
		err := retryOnSerializationFailure(3, func() error {
			attempts++
			return otherErr
		})
		if !errors.Is(err, otherErr) {
			t.Fatalf("retryOnSerializationFailure() = %v, want other error", err)
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
	})
}
