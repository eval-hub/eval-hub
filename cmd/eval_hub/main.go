package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/internal/validation"
)

var (
	// Version can be set during the compilation
	Version string = "0.0.1"
	// Build is set during the compilation
	Build string
	// BuildDate is set during the compilation
	BuildDate string
)

func main() {
	// TODO write fatal errors to the error file and close down the server

	// Create logger once for all requests
	logger, logShutdown, err := logging.NewLogger()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(nil, err, "Failed to create service logger", logging.FallbackLogger())
	}

	serviceConfig, err := config.LoadConfig(logger, Version, Build, BuildDate)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(nil, err, "Failed to create service config", logger)
	}

	// set up the validator
	validate, err := validation.NewValidator()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create validator", logger)
	}
	// serviceConfig.Validator = validator

	// set up the storage
	storage, err := storage.NewStorage(serviceConfig, logger)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create storage", logger)
	}
	// serviceConfig.Storage = storage

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create provider configs", logger)
	}

	srv, err := server.NewServer(logger, serviceConfig, providerConfigs, storage, validate)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create server", logger)
	}

	// log the start up details
	logger.Info("Server starting",
		"server_port", srv.GetPort(),
		"version", serviceConfig.Service.Version,
		"build", serviceConfig.Service.Build,
		"build_date", serviceConfig.Service.BuildDate,
		"storage", storage.GetDatasourceName(),
		"validator", validate != nil,
	)

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			// we do this as no point trying to continue
			startUpFailed(serviceConfig, err, "Server failed to start", logger)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// shutdown the storage
	if err := storage.Close(); err != nil {
		logger.Error("Failed to close storage", "error", err.Error(), "storage", storage.GetDatasourceName())
	}

	// Create a context with timeout for graceful shutdown
	waitForShutdown := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), waitForShutdown)
	defer cancel()

	// shutdown the logger
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err.Error(), "timeout", waitForShutdown)
		_ = logShutdown() // ignore the error
	} else {
		logger.Info("Server shutdown gracefully")
		_ = logShutdown() // ignore the error
	}
}

func startUpFailed(conf *config.Config, err error, msg string, logger *slog.Logger) {
	termErr := server.SetTerminationMessage(server.GetTerminationFile(conf, logger), fmt.Sprintf("%s: %s", msg, err.Error()), logger)
	if termErr != nil {
		logger.Error("Failed to set termination message", "message", msg, "error", termErr.Error())
		log.Println(termErr.Error())
	}
	log.Fatal(err)
}
