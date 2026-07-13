package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/internal/eval_hub/mlflow"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes"
	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/otel"
)

var (
	// Version can be set during the compilation
	Version string = "0.4.4"
	// Build is set during the compilation
	Build string
	// BuildDate is set during the compilation
	BuildDate string
	// GitHash is set during the compilation
	GitHash string
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

	startUpFailed := func(conf *config.Config, err error, msg string, logger *slog.Logger) {
		server.HandleStartupFailure(conf, args.LocalMode, err, msg, logger)
		log.Fatal(err)
	}

	logger, logShutdown, err := logging.NewLogger()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(nil, err, "Failed to create service logger", logging.FallbackLogger())
	}

	serviceConfig, err := config.LoadConfig(logger, Version, Build, BuildDate, GitHash, args.ConfigDir)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(nil, err, "Failed to create service config", logger)
	}

	serviceConfig.Service.LocalMode = args.LocalMode

	// set up the validator
	validate, err := validation.NewValidator()
	if err != nil {
		startUpFailed(serviceConfig, err, "Failed to create validator", logger)
	}

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, validate, args.ConfigDir)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create provider configs", logger)
	}

	// set up the collection configs
	collectionConfigs, err := config.LoadCollectionConfigs(logger, validate, args.ConfigDir)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create collection configs", logger)
	}

	// setup OTEL before storage so DB metrics can register against the MeterProvider
	var otelShutdown func(context.Context) error
	if serviceConfig.IsOTELEnabled() {
		shutdown, err := otel.SetupOTEL(context.Background(), serviceConfig.OTEL, logger, serviceConfig.IsPrometheusEnabled())
		if err != nil {
			startUpFailed(serviceConfig, err, "Failed to setup OTEL", logger)
		}
		otelShutdown = shutdown
		if serviceConfig.IsOTELLogsEnabled() {
			logger = otel.BridgeSlogToOTEL(logger)
		}
	}
	if serviceConfig.OTEL != nil && serviceConfig.OTEL.EnableJobContainerLogs && !serviceConfig.IsOTELLogsEnabled() {
		logger.Warn("otel.enable_job_container_logs requires otel.enable_logs; container log export is disabled")
	}

	if serviceConfig.IsOTELMetricsEnabled() {
		if err := metrics.Init(); err != nil {
			startUpFailed(serviceConfig, err, "Failed to initialize OTEL metrics", logger)
		}
	}

	// setup mlflow client if there is a tracking URI set
	mlflowClient, mlflowTrackingURI, mlflowServerVersion, err := mlflow.SetupMLFlowClient(serviceConfig, logger)
	if err != nil {
		startUpFailed(serviceConfig, err, "Failed to create MLFlow client", logger)
	}

	// set up the storage
	storage, err := storage.NewStorage(
		serviceConfig.Database,
		collectionConfigs,
		providerConfigs,
		serviceConfig.IsOTELStorageScansEnabled(),
		serviceConfig.IsOTELMetricsEnabled(),
		logger,
	)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create storage", logger)
	}
	// setup runtime
	runtime, err := runtimes.NewRuntime(logger, serviceConfig)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create runtime", logger)
	}
	logger.Info("Runtime created", "runtime", runtime.Name())

	// create the server
	srv, err := server.NewServer(logger,
		serviceConfig,
		storage,
		validate,
		runtime,
		mlflowClient)

	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(serviceConfig, err, "Failed to create API server", logger)
	}

	// Create the metrics server if Prometheus is enabled
	var metricsSrv *server.MetricsServer
	if serviceConfig.IsPrometheusEnabled() {
		metricsSrv = server.NewMetricsServer(logger, serviceConfig.Prometheus)
	}

	// log the start up details
	metricsPort := 0
	if metricsSrv != nil {
		metricsPort = metricsSrv.GetPort()
	}
	logger.Info("API Server starting",
		"server_port", srv.GetPort(),
		"metrics_port", metricsPort,
		"version", serviceConfig.Service.Version,
		"build", serviceConfig.Service.Build,
		"build_date", serviceConfig.Service.BuildDate,
		"git_hash", serviceConfig.Service.GitHash,
		"validator", validate != nil,
		"local", serviceConfig.Service.LocalMode,
		"tls", serviceConfig.Service.TLSEnabled(),
		"mlflow_tracking", mlflowClient != nil,
		"mlflow_tracking_uri", mlflowTrackingURI,
		"mlflow_server_version", mlflowServerVersion,
		"otel", serviceConfig.IsOTELEnabled(),
		"otel_storage_scans", serviceConfig.IsOTELStorageScansEnabled(),
		"prometheus", serviceConfig.IsPrometheusEnabled(),
	)

	// Start config watcher to reload system providers and collections on file changes
	watcherDone, watcherCancel := config.SetupWatcher(logger, validate, storage, args.ConfigDir)

	// Start metrics server in a goroutine
	if metricsSrv != nil {
		go func() {
			if err := metricsSrv.Start(); err != nil {
				logger.Error("Metrics server failed", "error", err.Error())
			}
		}()
	}

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil {
			// we do this as no point trying to continue
			if errors.Is(err, &server.ServerClosedError{}) {
				logger.Info("API Server closed gracefully")
				return
			}
			startUpFailed(serviceConfig, err, "API Server failed to start", logger)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Stop config watcher
	logger.Info("Shutting down API config watcher...")
	watcherCancel()
	<-watcherDone // Wait for Watch() to fully complete

	// Create a context with timeout for graceful shutdown
	waitForShutdown := 30 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), waitForShutdown)
	defer cancel()

	// shutdown the metrics server
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("Metrics server forced to shutdown", "error", err.Error())
		}
	}

	// shutdown the storage
	logger.Info("Shutting down API storage...")
	if err := storage.Close(); err != nil {
		logger.Error("Failed to close API storage", "error", err.Error())
	}

	// shutdown the otel tracing
	if otelShutdown != nil {
		logger.Info("Shutting down API OTEL...")
		if err := otelShutdown(shutdownCtx); err != nil {
			logger.Error("Failed to shutdown API OTEL", "error", err.Error())
		}
	}

	// shutdown the logger
	logger.Info("Shutting down API server...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("API Server forced to shutdown", "error", err.Error(), "timeout", waitForShutdown)
		_ = logShutdown() // ignore the error
	} else {
		logger.Info("API Server shutdown gracefully")
		_ = logShutdown() // ignore the error
	}
}
