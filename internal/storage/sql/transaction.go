package sql

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
)

type TransactionFunction func(*sql.Tx) error

const transactionMaxRetries = 3

func (s *SQLStorage) withTransaction(name string, resourceID string, fn TransactionFunction) error {
	if s.sqlConfig.Driver == SQLITE_DRIVER {
		return s.withTransactionRetry(name, resourceID, fn)
	}
	return s.executeTransaction(name, resourceID, fn)
}

// NOTE: The SQLITE_DRIVER transaction logic with automatic retry on lock contention
// is currently only applied to --local (SQLite) mode. Production environments should use Postgres.
func (s *SQLStorage) withTransactionRetry(name string, resourceID string, fn TransactionFunction) error {
	var lastErr error
	for attempt := 0; attempt < transactionMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*100) * time.Millisecond)
			s.logger.Warn("Retrying transaction due to SQLite lock contention",
				"name", name, "resource_id", resourceID, "attempt", attempt+1)
		}
		lastErr = s.executeTransaction(name, resourceID, fn)
		if lastErr == nil {
			return nil
		}
		if !isSQLiteBusyError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func (s *SQLStorage) executeTransaction(name string, resourceID string, fn TransactionFunction) error {
	txn, err := s.pool.BeginTx(s.ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin transaction", "name", fmt.Sprintf("begin transaction %s", name), "resource_id", resourceID, "error", err.Error())
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
			s.logger.Error("Failed to commit transaction", "name", fmt.Sprintf("commit transaction %s", name), "resource_id", resourceID, "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("commit transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	} else {
		if txnErr := txn.Rollback(); txnErr != nil {
			s.logger.Error("Failed to rollback transaction", "name", fmt.Sprintf("rollback transaction %s", name), "resource_id", resourceID, "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("rollback transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	}
	// this is the error from the code function
	return servicerError
}

func isSQLiteBusyError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
}
