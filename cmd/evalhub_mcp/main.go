package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	mcpserver "github.com/eval-hub/eval-hub/internal/evalhub_mcp/server"
	flag "github.com/spf13/pflag"
)

var (
	Version   string = "0.1.0"
	Build     string
	BuildDate string
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("evalhub-mcp", flag.ContinueOnError)

	transport := fs.String("transport", "stdio", "Transport mode: stdio or http")
	host := fs.String("host", "localhost", "Host to bind HTTP server to")
	port := fs.Int("port", 3001, "Port for HTTP server")
	configPath := fs.String("config", "", "Path to configuration file")
	insecure := fs.Bool("insecure", false, "Disable TLS certificate verification")
	version := fs.Bool("version", false, "Print version information and exit")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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

	cfg, err := config.Load(flags)
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		return 1
	}

	if err := config.Validate(cfg); err != nil {
		log.Printf("Invalid configuration: %v", err)
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

	if err := mcpserver.Run(ctx, cfg, Version); err != nil {
		log.Printf("Server error: %v", err)
		return 1
	}

	return 0
}

func printVersion() {
	fmt.Printf("evalhub-mcp version %s", Version)
	if Build != "" {
		fmt.Printf(" (build: %s)", Build)
	}
	if BuildDate != "" {
		fmt.Printf(" (built: %s)", BuildDate)
	}
	fmt.Println()
}
