package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	mcpserver "github.com/eval-hub/eval-hub/internal/evalhub_mcp/server"
	"github.com/eval-hub/eval-hub/internal/logging"
	flag "github.com/spf13/pflag"
)

var (
	Version   string = "0.1.0"
	Build     string
	BuildDate string
	GitHash   string
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	logger, shutdown, err := logging.NewLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return 1
	}
	defer shutdown() //nolint:errcheck

	// Strip optional "serve" subcommand for operator compatibility
	// (e.g. evalhub-mcp serve --transport http-sse).
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}

	fs := flag.NewFlagSet("evalhub-mcp", flag.ContinueOnError)

	transport := fs.String("transport", "stdio", "Transport mode: stdio, http, http-sse, or streamable-http")
	host := fs.String("host", "localhost", "Host to bind HTTP server to")
	port := fs.Int("port", 3001, "Port for HTTP server")
	configPath := fs.String("config", "", "Path to configuration file")
	insecure := fs.Bool("insecure", false, "Disable TLS certificate verification")
	tlsCertFile := fs.String("tls-cert", "", "Path to TLS certificate file")
	tlsKeyFile := fs.String("tls-key", "", "Path to TLS private key file")
	version := fs.Bool("version", false, "Print version information and exit")

	if err := fs.Parse(args); err != nil {
		logger.Error("failed to parse flags", "error", err)
		return 1
	}

	if *version {
		printVersion()
		return 0
	}

	flags := &config.Flags{
		ConfigPath: *configPath,
	}
	if fs.Changed("transport") {
		flags.Transport = transport
	}
	if fs.Changed("host") {
		flags.Host = host
	}
	if fs.Changed("port") {
		flags.Port = port
	}
	if fs.Changed("insecure") {
		flags.Insecure = insecure
	}
	if fs.Changed("tls-cert") {
		flags.TLSCertFile = tlsCertFile
	}
	if fs.Changed("tls-key") {
		flags.TLSKeyFile = tlsKeyFile
	}

	cfg, err := config.Load(flags, logger)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		return 1
	}

	if err := config.Validate(cfg); err != nil {
		logger.Error("invalid configuration", "error", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		cancel()
	}()

	info := &mcpserver.ServerInfo{
		Version:   Version,
		Build:     Build,
		BuildDate: BuildDate,
		GitHash:   GitHash,
	}

	if err := mcpserver.Run(ctx, cfg, info, logger); err != nil {
		logger.Error("MCP Server error", "error", err)
		return 1
	}

	return 0
}

func printVersion() {
	fmt.Printf("evalhub-mcp version %s", Version)
	if Build != "" {
		fmt.Printf(" (build: %s)", Build)
	}
	if GitHash != "" {
		fmt.Printf(" (commit: %s)", GitHash)
	}
	if BuildDate != "" {
		fmt.Printf(" (built: %s)", BuildDate)
	}
	fmt.Println()
}
