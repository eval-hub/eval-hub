package sql

import (
	"database/sql"
	"fmt"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
)

// TransactionFunction runs database work inside a transaction begun by runTransaction.
// It may be invoked more than once when runTransaction retries on serialization
// failure (SQLSTATE 40001); callbacks must be retry-safe—limit work to operations
// against the provided *sql.Tx and avoid irreversible side effects outside the DB.
type TransactionFunction func(*sql.Tx) error

// withTransaction runs fn inside a transaction, retrying the full transaction on
// serialization failure via retryOnSerializationFailure. See TransactionFunction.
func (s *sqlStorage) withTransaction(name string, resourceID string, fn TransactionFunction) error {
	return retryOnSerializationFailure(serializationFailureMaxAttempts, func() error {
		err := s.runTransaction(name, resourceID, fn)
		if err != nil && isSerializationFailure(err) {
			s.logger.Warn(
				"Transaction failed with serialization error; will retry if attempts remain",
				"name", name,
				"resource_id", resourceID,
				"isolation_level", s.isolationLevel.String(),
				"error", err.Error(),
			)
		}
		return err
	})
}

// runTransaction begins a transaction, runs fn, then commits or rolls back.
// Serialization-failure retries are applied by withTransaction, which re-invokes
// runTransaction (and thus fn) until success or retryOnSerializationFailure exhausts attempts.
func (s *sqlStorage) runTransaction(name string, resourceID string, fn TransactionFunction) error {
	txn, err := s.pool.BeginTx(s.ctx, &sql.TxOptions{Isolation: s.isolationLevel})
	if err != nil {
		s.logger.Error("Failed to begin transaction", "name", fmt.Sprintf("begin transaction %s", name), "resource_id", resourceID, "isolation_level", s.isolationLevel.String(), "error", err.Error())
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("begin transaction %s", name), "ResourceId", resourceID, "Error", err.Error())
	}
	servicerError := fn(txn)
	commit := true
	if servicerError != nil {
		if se, ok := servicerError.(abstractions.ServiceError); ok {
			if se.ShouldRollback() {
				commit = false
			}
		} else {
			// This is not a service error, so we rollback the transaction
			// we could decide to fail here if we don't get a service error
			commit = false
		}
	}
	if commit {
		if txnErr := txn.Commit(); txnErr != nil {
			_ = txn.Rollback()
			s.logger.Error("Failed to commit transaction", "name", fmt.Sprintf("commit transaction %s", name), "resource_id", resourceID, "isolation_level", s.isolationLevel.String(), "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("commit transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	} else {
		if txnErr := txn.Rollback(); txnErr != nil {
			s.logger.Error("Failed to rollback transaction", "name", fmt.Sprintf("rollback transaction %s", name), "resource_id", resourceID, "isolation_level", s.isolationLevel.String(), "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("rollback transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	}
	// this is the error from the code function
	return servicerError
}
