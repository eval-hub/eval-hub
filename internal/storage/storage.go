package storage

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/storage/sql"
)

// NewStorage creates a new storage instance based on the configuration.
// It currently uses the SQL storage implementation.
func NewStorage(serviceConfig *config.Config, logger *slog.Logger) (abstractions.Storage, error) {
	if serviceConfig.Database == nil {
		return nil, &StorageError{Message: "database configuration is required"}
	}
	return sql.NewStorage(*serviceConfig.Database, logger)
}

// StorageError represents an error in storage operations
type StorageError struct {
	Message string
}

func (e *StorageError) Error() string {
	return e.Message
}
