package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// These should be shared with the eval-hub server
const (
	TRANSACTION_ID_HEADER = "X-Global-Transaction-Id"
	USER_HEADER           = "X-User"
	TENANT_HEADER         = "X-Tenant"
)

type ServerInfo struct {
	Version   string
	Build     string
	BuildDate string
	GitHash   string
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
	client := evalhubclient.NewClient(cfg.BaseURL).WithListPageLimit(cfg.ListPageLimit).WithLogger(logger)
	if cfg.Token != "" {
		client = client.WithToken(cfg.Token)
	}
	if cfg.Tenant != "" {
		client = client.WithTenant(cfg.Tenant)
	}
	if cfg.Insecure {
		client = client.WithInsecureSkipVerify()
	}
	if cfg.CACertPath != "" {
		pemData, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			logger.Error("failed to read CA cert file", "path", cfg.CACertPath, "error", err)
		} else {
			withCA, err := client.WithCACert(pemData)
			if err != nil {
				logger.Error("failed to configure CA cert", "path", cfg.CACertPath, "error", err)
			} else {
				client = withCA
			}
		}
	}
	logger.Info("EvalHub client created", "baseURL", cfg.BaseURL, "tenant", cfg.Tenant, "insecure", cfg.Insecure, "caCertPath", cfg.CACertPath)
	return client
}

// RegisterHandlers wires tool, resource, and prompt handlers into the MCP
// server. listPageLimit is the default maximum number of items requested from
// eval-hub list endpoints (providers, collections, jobs, and completion caches);
// resource URIs may still set an explicit "limit" query parameter for collections and jobs.
func RegisterHandlers(srv *mcp.Server, client *evalhubclient.Client, info *ServerInfo, logger *slog.Logger, listPageLimit int) error {
	registerVersionResource(srv, info, logger)
	// should we error if no client is provided?
	if client != nil {
		if err := registerPrompts(srv, logger); err != nil {
			return err
		}
		registerResources(srv, client, logger, listPageLimit)
		registerTools(srv, client, logger)
	}
	return nil
}

// CompletionHandlerOption returns a ServerOption that installs a completion handler
// backed by the given data source. Returns nil when ds is nil.
func CompletionHandlerOption(ds EvalHubDiscovery, logger *slog.Logger, listPageLimit int) *ServerOption {
	if ds == nil {
		return nil
	}
	cp := newCompletionProvider(ds, logger, listPageLimit)
	return &ServerOption{applyFn: func(opts *mcp.ServerOptions) {
		opts.CompletionHandler = cp.handle
	}}
}

func Run(ctx context.Context, cfg *config.Config, info *ServerInfo, logger *slog.Logger) error {
	client := NewEvalHubClient(cfg, logger)
	srv := New(info, logger, CompletionHandlerOption(client, logger, cfg.ListPageLimit))
	if err := RegisterHandlers(srv, client, info, logger, cfg.ListPageLimit); err != nil {
		return err
	}

	version := "unknown"
	if info != nil {
		version = info.VersionString()
	}
	logger.Info("Starting evalhub-mcp server",
		"version", version,
		"transport", cfg.Transport,
		"auth_type", cfg.AuthType,
	)

	switch cfg.Transport {
	case config.TransportStdio:
		return runStdio(ctx, srv)
	case config.TransportHTTP:
		return runHTTP(ctx, srv, cfg, logger)
	case config.TransportHTTPSSE:
		logger.Warn("transport http-sse is deprecated; use http (Streamable HTTP) unless connecting to legacy MCP clients",
			"transport", cfg.Transport,
		)
		return runLegacyHTTPSSE(ctx, srv, cfg, logger)
	default:
		return fmt.Errorf("unsupported transport: %s", cfg.Transport)
	}
}

func runStdio(ctx context.Context, srv *mcp.Server) error {
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func wrapRequest(cfg *config.Config, logger *slog.Logger, next http.Handler) http.Handler {
	var h http.Handler
	switch cfg.AuthType {
	case config.AuthTypeRBACProxy:
		// if we have the kube-rbac-proxy then we need to check the HTTP headers for the tenant and user headers
		required := []string{TENANT_HEADER, USER_HEADER}
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, name := range required {
				if strings.TrimSpace(r.Header.Get(name)) == "" {
					http.Error(w, fmt.Sprintf("Missing required header '%s' from kube-rbac-proxy", name), http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	case config.AuthTypeNone:
		h = next
	default:
		h = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Unsupported authentication type", http.StatusInternalServerError)
		})
	}
	return withRequestContext(logger, h)
}

func runHTTP(ctx context.Context, srv *mcp.Server, cfg *config.Config, logger *slog.Logger) error {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		// Server runs behind kube-rbac-proxy which handles authentication;
		// the default localhost DNS-rebinding protection rejects the proxy's
		// forwarded Host header and must be disabled.
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
	handler := wrapRequest(cfg, logger, mcpHandler)
	return serveHTTP(ctx, handler, cfg, logger)
}

// runLegacyHTTPSSE serves the deprecated HTTP+SSE transport (MCP 2024-11-05) for older clients.
func runLegacyHTTPSSE(ctx context.Context, srv *mcp.Server, cfg *config.Config, logger *slog.Logger) error {
	mcpHandler := mcp.NewSSEHandler(
		func(r *http.Request) *mcp.Server { return srv },
		// Same rationale as runHTTP: behind kube-rbac-proxy.
		&mcp.SSEOptions{DisableLocalhostProtection: true},
	)
	logger.Warn("transport 'http-sse' is deprecated; use 'http' (Streamable HTTP) unless connecting to legacy MCP clients", "transport", cfg.Transport)
	handler := wrapRequest(cfg, logger, mcpHandler)
	return serveHTTP(ctx, handler, cfg, logger)
}

func serveHTTP(ctx context.Context, mcpHandler http.Handler, cfg *config.Config, logger *slog.Logger) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.Handle("/", mcpHandler)

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("MCP HTTP Server listening", "addr", addr, "tls", cfg.TLSEnabled())
		if cfg.TLSEnabled() {
			errCh <- httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			errCh <- httpServer.ListenAndServe()
		}
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
