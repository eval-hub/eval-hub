package storage

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql"
)

// NewStorage creates a new storage instance based on the configuration.
// It currently uses the SQL storage implementation.
func NewStorage(databaseConfig *map[string]any, logger *slog.Logger) (abstractions.Storage, error) {
	if databaseConfig == nil {
		return nil, serviceerrors.NewServiceError(messages.ConfigurationFailed, "Error", "database configuration")
	}
	return sql.NewStorage(*databaseConfig, logger)
}
