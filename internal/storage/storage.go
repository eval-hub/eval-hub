package storage

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql"
)

// NewStorage creates a new storage instance based on the configuration.
// It currently uses the SQL storage implementation.
func NewStorage(serviceConfig *config.Config, logger *slog.Logger) (abstractions.Storage, error) {
	if serviceConfig.Database == nil {
		return nil, serviceerrors.NewStorageError("database configuration is required")
	}
	return sql.NewStorage(*serviceConfig.Database, logger)
}
