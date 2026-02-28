package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/mlflow"
	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/internal/runtimes"
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

type Args struct {
	ConfigDir string
	LocalMode bool
}

func args() Args {
	configDir := ""
	dir := flag.String("configdir", configDir, "Directory to search for configuration files.")
	local := flag.Bool("local", false, "Server operates in local mode or not.")
	flag.Parse()
	configDir = *dir
	if configDir == "" {
		configDir = os.Getenv("EVAL_HUB_CONFIG_DIR")
	}

	return Args{
		ConfigDir: configDir,
		LocalMode: *local,
	}
}

func main() {
	args := args()

	logger, logShutdown, err := logging.NewLogger()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(nil, err, "Failed to create service logger", logging.FallbackLogger())
	}

	serviceConfig, err := config.LoadConfig(logger, Version, Build, BuildDate, args.ConfigDir)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(nil, err, "Failed to create service config", logger)
	}

	serviceConfig.Service.LocalMode = args.LocalMode

	// set up the validator
	validate, err := validation.NewValidator()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create validator", logger)
	}

	// set up the storage
	storage, err := storage.NewStorage(serviceConfig.Database, serviceConfig.IsOTELEnabled(), logger)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create storage", logger)
	}

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, args.ConfigDir)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create provider configs", logger)
	}

	// setup runtime
	runtime, err := runtimes.NewRuntime(logger, serviceConfig, providerConfigs)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create runtime", logger)
	}
	logger.Info("Runtime created", "runtime", runtime.Name())

	mlflowClient, err := mlflow.NewMLFlowClient(serviceConfig, logger)
	if err != nil {
		startUpFailed(serviceConfig, err, "Failed to create MLFlow client", logger)
	}

	// setup OTEL
	var otelShutdown func(context.Context) error
	if serviceConfig.IsOTELEnabled() {
		// TODO CHECK TO SEE WHY WE HAVE TO PASS IN A CONTEXT HERE
		shutdown, err := otel.SetupOTEL(context.Background(), serviceConfig.OTEL, logger)
		if err != nil {
			// we do this as no point trying to continue
			startUpFailed(serviceConfig, err, "Failed to setup OTEL", logger)
		}
		otelShutdown = shutdown
	}

	// create the server
	srv, err := server.NewServer(logger,
		serviceConfig,
		providerConfigs,
		storage,
		validate,
		runtime,
		mlflowClient,
		nil)

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
		"validator", validate != nil,
		"local", serviceConfig.Service.LocalMode,
		"mlflow_tracking", mlflowClient != nil,
		"otel", serviceConfig.IsOTELEnabled(),
		"prometheus", serviceConfig.IsPrometheusEnabled(),
	)

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil {
			// we do this as no point trying to continue
			if errors.Is(err, &server.ServerClosedError{}) {
				logger.Info("Server closed gracefully")
				return
			}
			startUpFailed(serviceConfig, err, "Server failed to start", logger)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Create a context with timeout for graceful shutdown
	waitForShutdown := 30 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), waitForShutdown)
	defer cancel()

	// shutdown the storage
	logger.Info("Shutting down storage...")
	if err := storage.Close(); err != nil {
		logger.Error("Failed to close storage", "error", err.Error())
	}

	// shutdown the otel tracing
	if otelShutdown != nil {
		logger.Info("Shutting down OTEL...")
		if err := otelShutdown(shutdownCtx); err != nil {
			logger.Error("Failed to shutdown OTEL", "error", err.Error())
		}
	}

	// shutdown the logger
	logger.Info("Shutting down server...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
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
