package storage

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/postgres"
	"github.com/eval-hub/eval-hub/internal/storage/sql/sqlite"
)

// NewStorage creates a new storage instance based on the configuration.
// It currently uses the SQL storage implementation.
func NewStorage(config *config.Config, otelEnabled bool, logger *slog.Logger) (abstractions.Storage, error) {
	if config.Database == nil {
		return nil, serviceerrors.NewServiceError(messages.ConfigurationFailed, "Error", "database configuration")
	}

	if config.Service.LocalMode {
		return sqlite.NewStorage(*config.Database, otelEnabled, logger)
	}
	return postgres.NewStorage(*config.Database, otelEnabled, logger)

}
