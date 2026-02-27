package sql

// The code in this file must be unware of the database implementation.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
)

type TransactionFunction func(*sql.Tx) error

func WithTransaction(db *sql.DB, ctx context.Context, logger *slog.Logger, name string, resourceID string, fn TransactionFunction) error {
	txn, err := db.BeginTx(ctx, nil)
	if err != nil {
		logger.Error("Failed to begin transaction", "name", fmt.Sprintf("begin transaction %s", name), "resource_id", resourceID, "error", err.Error())
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
			logger.Error("Failed to commit transaction", "name", fmt.Sprintf("commit transaction %s", name), "resource_id", resourceID, "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("commit transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	} else {
		if txnErr := txn.Rollback(); txnErr != nil {
			logger.Error("Failed to rollback transaction", "name", fmt.Sprintf("rollback transaction %s", name), "resource_id", resourceID, "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("rollback transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	}
	// this is the error from the code function
	return servicerError
}
