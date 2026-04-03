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
	"strings"
	"syscall"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/config"
	sidecarServer "github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/server"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/termination"
	"github.com/eval-hub/eval-hub/internal/logging"
)

var (
	// Version can be set during the compilation
	Version string = "0.0.1"
	// Build is set during the compilation
	Build string
	// BuildDate is set during the compilation
	BuildDate string
)

func sidecarConfigPath() string {
	p := flag.String("sidecarconfig", "", "Path to sidecar_config.json (default: "+config.DefaultSidecarConfigPath+")")
	flag.Parse()
	if strings.TrimSpace(*p) != "" {
		return strings.TrimSpace(*p)
	}
	return config.DefaultSidecarConfigPath
}

func main() {
	cfgPath := sidecarConfigPath()

	logger, logShutdown, err := logging.NewLogger()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(terminationFilePath(), err, "Failed to create service logger", logging.FallbackLogger())
	}
	svcConfig, err := config.LoadSidecarRuntimeConfig(cfgPath, Version, Build, BuildDate)
	if err != nil {
		startUpFailed(terminationFilePath(), err, "Failed to load sidecar config", logger)
	}

	srv, err := sidecarServer.NewSidecarServer(logger, svcConfig)
	if err != nil {
		startUpFailed(terminationFilePath(), err, "Failed to create sidecar server", logger)
	}

	version, build, buildDate := "", "", ""
	if svcConfig.Service != nil {
		version, build, buildDate = svcConfig.Service.Version, svcConfig.Service.Build, svcConfig.Service.BuildDate
	}
	logger.Info("Server starting",
		"server_port", srv.GetPort(),
		"sidecar_config", cfgPath,
		"version", version,
		"build", build,
		"build_date", buildDate,
		"mlflow_tracking", svcConfig.MLFlow != nil && svcConfig.MLFlow.TrackingURI != "",
	)

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil {
			// we do this as no point trying to continue
			if errors.Is(err, &sidecarServer.ServerClosedError{}) {
				logger.Info("Server closed gracefully")
				return
			}
			startUpFailed(terminationFilePath(), err, "Server failed to start", logger)
		}
	}()

	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()
	adapterTerminated := termination.WatchDefaultAdapterTermination(watchCtx, logger)

	// Wait for interrupt signal or adapter-created termination file (shared emptyDir) to shut down gracefully
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-quit:
		logger.Info("Received shutdown signal", "signal", sig.String())
	case <-adapterTerminated:
		logger.Info("Shutting down after adapter termination signal",
			"watch_dir", termination.AdapterTerminationWatchDir,
			"file", termination.AdapterTerminationSignalFile,
		)
	}
	watchCancel()

	// Create a context with timeout for graceful shutdown
	waitForShutdown := 30 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), waitForShutdown)
	defer cancel()

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

func terminationFilePath() string {
	return config.SidecarTerminationFilePath
}

func startUpFailed(terminationFile string, err error, msg string, logger *slog.Logger) {
	termErr := server.SetTerminationMessage(terminationFile, fmt.Sprintf("%s: %s", msg, err.Error()), logger)
	if termErr != nil {
		logger.Error("Failed to set termination message", "message", msg, "error", termErr.Error())
		log.Println(termErr.Error())
	}
	log.Fatal(err)
}
