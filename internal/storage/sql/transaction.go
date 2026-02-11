package sql

import (
	"database/sql"
	"fmt"

	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
)

type TransactionFunction func(*sql.Tx) (TransactionState, error)

type TransactionState int

const (
	TransactionStateCommit TransactionState = iota
	TransactionStateRollback
)

func (s *SQLStorage) withTransaction(name string, resourceID string, fn TransactionFunction) error {
	txn, err := s.pool.BeginTx(s.ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin transaction", "name", fmt.Sprintf("begin transaction %s", name), "resource_id", resourceID, "error", err.Error())
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("begin transaction %s", name), "ResourceId", resourceID, "Error", err.Error())
	}
	state, servicerError := fn(txn)
	switch state {
	case TransactionStateCommit:
		if txnErr := txn.Commit(); txnErr != nil {
			s.logger.Error("Failed to commit transaction", "name", fmt.Sprintf("commit transaction %s", name), "resource_id", resourceID, "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("commit transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	case TransactionStateRollback:
		if txnErr := txn.Rollback(); txnErr != nil {
			s.logger.Error("Failed to rollback transaction", "name", fmt.Sprintf("rollback transaction %s", name), "resource_id", resourceID, "error", txnErr.Error())
			return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", fmt.Sprintf("rollback transaction %s", name), "ResourceId", resourceID, "Error", txnErr.Error())
		}
	}
	// this is the error from the code function
	return servicerError
}

/*
func (s *SQLStorage) withoutTransaction(_ string, _ string, fn TransactionFunction) error {
	_, servicerError := fn(nil)
	return servicerError
}
*/
