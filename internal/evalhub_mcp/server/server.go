package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServerInfo struct {
	Version   string
	Build     string
	BuildDate string
}

func (s *ServerInfo) VersionString() string {
	if s.Build != "" {
		return s.Version + "+" + s.Build
	}
	return s.Version
}

// New creates a configured MCP server with capabilities advertised for tools,
// resources, and prompts. The returned server is ready to be connected to a
// transport via Run, or used directly with in-memory transports for testing.
func New(info *ServerInfo, logger *slog.Logger, serverOption *ServerOption) *mcp.Server {
	version := "unknown"
	if info != nil {
		version = info.VersionString()
	}
	serverOpts := &mcp.ServerOptions{
		Logger: logger,
		Capabilities: &mcp.ServerCapabilities{
			Logging:   &mcp.LoggingCapabilities{},
			Tools:     &mcp.ToolCapabilities{ListChanged: true},
			Resources: &mcp.ResourceCapabilities{ListChanged: true},
			Prompts:   &mcp.PromptCapabilities{ListChanged: true},
		},
	}
	if serverOption != nil {
		serverOption.apply(serverOpts)
	}
	return mcp.NewServer(
		&mcp.Implementation{
			Name:    "evalhub-mcp",
			Version: version,
		},
		serverOpts,
	)
}

// ServerOption configures the MCP server options.
type ServerOption struct {
	applyFn func(*mcp.ServerOptions)
}

func (o *ServerOption) apply(opts *mcp.ServerOptions) {
	if o.applyFn != nil {
		o.applyFn(opts)
	}
}

// NewEvalHubClient creates an EvalHub API client from the MCP server configuration.
// Returns nil when no BaseURL is configured.
func NewEvalHubClient(cfg *config.Config, logger *slog.Logger) *evalhubclient.Client {
	if cfg.BaseURL == "" {
		return nil
	}
	client := evalhubclient.NewClient(cfg.BaseURL).WithLogger(logger)
	if cfg.Token != "" {
		client = client.WithToken(cfg.Token)
	}
	if cfg.Tenant != "" {
		client = client.WithTenant(cfg.Tenant)
	}
	if cfg.Insecure {
		client = client.WithInsecureSkipVerify()
	}
	return client
}

// RegisterHandlers wires tool, resource, and prompt handlers into the MCP
// server. The server version resource is always registered. The EvalHub client
// is captured by handler closures so that every handler has access to the API
// without global state.
func RegisterHandlers(srv *mcp.Server, client *evalhubclient.Client, info *ServerInfo, logger *slog.Logger) {
	registerVersionResource(srv, info, logger)
	if client != nil {
		registerResources(srv, client, logger)
	}
}

// CompletionHandlerOption returns a ServerOption that installs a completion handler
// backed by the given data source. Returns nil when ds is nil.
func CompletionHandlerOption(ds EvalHubDiscovery, logger *slog.Logger) *ServerOption {
	if ds == nil {
		return nil
	}
	cp := newCompletionProvider(ds, logger)
	return &ServerOption{applyFn: func(opts *mcp.ServerOptions) {
		opts.CompletionHandler = cp.handle
	}}
}

func Run(ctx context.Context, cfg *config.Config, info *ServerInfo, logger *slog.Logger) error {
	client := NewEvalHubClient(cfg, logger)
	srv := New(info, logger, CompletionHandlerOption(client, logger))
	RegisterHandlers(srv, client, info, logger)

	version := "unknown"
	if info != nil {
		version = info.VersionString()
	}
	logger.Info("starting evalhub-mcp server",
		"version", version,
		"transport", cfg.Transport,
	)

	switch cfg.Transport {
	case "stdio":
		return runStdio(ctx, srv)
	case "http":
		return runHTTP(ctx, srv, cfg, logger)
	default:
		return fmt.Errorf("unsupported transport: %s", cfg.Transport)
	}
}

func runStdio(ctx context.Context, srv *mcp.Server) error {
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func runHTTP(ctx context.Context, srv *mcp.Server, cfg *config.Config, logger *slog.Logger) error {
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		nil,
	)

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("MCP HTTP Server listening", "addr", addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("MCP HTTP Server error: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("MCP HTTP Server graceful shutdown failed", "error", shutdownErr)
			if closeErr := httpServer.Close(); closeErr != nil {
				return errors.Join(shutdownErr, closeErr)
			}
			return shutdownErr
		}
		logger.Info("MCP HTTP Server stopped gracefully")
		return nil
	}
}
