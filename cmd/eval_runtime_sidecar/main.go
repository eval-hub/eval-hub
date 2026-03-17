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

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	sidecarServer "github.com/eval-hub/eval-hub/cmd/eval_runtime_sidecar/server"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/otel"
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
}

func args() Args {
	configDir := ""
	dir := flag.String("configdir", configDir, "Directory to search for configuration files.")
	flag.Parse()
	configDir = *dir
	if configDir == "" {
		configDir = os.Getenv("EVAL_HUB_CONFIG_DIR")
	}

	return Args{
		ConfigDir: configDir,
	}
}

func main() {
	args := args()

	logger, logShutdown, err := logging.NewLogger()
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(terminationFilePath(nil, logger), err, "Failed to create service logger", logging.FallbackLogger())
	}
	defaultConfigDir := "/etc/evalhub/config"
	if args.ConfigDir == "" {
		args.ConfigDir = defaultConfigDir
	}
	config, err := config.LoadConfig(logger, Version, Build, BuildDate, args.ConfigDir)
	if err != nil {
		// we do this as no point trying to continue
		startUpFailed(terminationFilePath(nil, logger), err, "Failed to create service config", logger)
	}

	// setup OTEL
	var otelShutdown func(context.Context) error
	if config.IsOTELEnabled() {
		// TODO CHECK TO SEE WHY WE HAVE TO PASS IN A CONTEXT HERE
		shutdown, err := otel.SetupOTEL(context.Background(), config.OTEL, logger)
		if err != nil {
			// we do this as no point trying to continue
			startUpFailed(terminationFilePath(config, logger), err, "Failed to setup OTEL", logger)
		}
		otelShutdown = shutdown
	}

	// create the server
	srv, err := sidecarServer.NewSidecarServer(logger, config)
	if err != nil {
		startUpFailed(terminationFilePath(config, logger), err, "Failed to create sidecar server", logger)
	}

	// log the start up details
	version, build, buildDate := "", "", ""
	if config.Service != nil {
		version, build, buildDate = config.Service.Version, config.Service.Build, config.Service.BuildDate
	}
	logger.Info("Server starting",
		"server_port", srv.GetPort(),
		"version", version,
		"build", build,
		"build_date", buildDate,
		"mlflow_tracking", config.MLFlow != nil && config.MLFlow.TrackingURI != "",
		"otel", config.IsOTELEnabled(),
		"prometheus", config.IsPrometheusEnabled(),
	)

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil {
			// we do this as no point trying to continue
			if errors.Is(err, &server.ServerClosedError{}) {
				logger.Info("Server closed gracefully")
				return
			}
			startUpFailed(terminationFilePath(config, logger), err, "Server failed to start", logger)
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

func terminationFilePath(cfg *config.Config, logger *slog.Logger) string {
	if cfg != nil && cfg.Service != nil && strings.TrimSpace(cfg.Service.TerminationFile) != "" {
		return strings.TrimSpace(cfg.Service.TerminationFile)
	}
	if tf := os.Getenv(constants.EnvVarTerminationFile); tf != "" {
		logger.Info("Termination file set from environment variable", "env", constants.EnvVarTerminationFile, "file", tf)
		return tf
	}
	return "/opt/evalhub/work/termination-log"
}

func startUpFailed(terminationFile string, err error, msg string, logger *slog.Logger) {
	termErr := server.SetTerminationMessage(terminationFile, fmt.Sprintf("%s: %s", msg, err.Error()), logger)
	if termErr != nil {
		logger.Error("Failed to set termination message", "message", msg, "error", termErr.Error())
		log.Println(termErr.Error())
	}
	log.Fatal(err)
}
