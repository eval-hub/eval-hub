package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	handlers "github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/handlers"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type SidecarServer struct {
	httpServer *http.Server
	port       int
	logger     *slog.Logger
	config     *config.Config
}

// NewSidecarServer creates a new sidecar HTTP server with the given logger and config.
func NewSidecarServer(logger *slog.Logger,
	config *config.Config,
) (*SidecarServer, error) {

	if logger == nil {
		return nil, fmt.Errorf("logger is required for the server")
	}
	if config == nil {
		return nil, fmt.Errorf("service config is required for the sidecar server")
	}

	port := 8080

	if config.Sidecar != nil {
		if baseURL := strings.TrimSpace(config.Sidecar.BaseURL); baseURL != "" {
			if strings.Contains(baseURL, ":") {
				parts := strings.Split(baseURL, ":")
				portStr := parts[len(parts)-1]
				portInt, err := strconv.Atoi(portStr)
				if err != nil {
					logger.Warn("invalid port in base URL, using default port 8080", "error", err)
				} else {
					port = portInt
				}
			}
		}
	}

	return &SidecarServer{
		port:   port,
		logger: logger,
		config: config,
	}, nil
}

func (s *SidecarServer) isOTELEnabled() bool {
	return s.config != nil && s.config.IsOTELEnabled()
}

func (s *SidecarServer) GetPort() int {
	return s.port
}

func (s *SidecarServer) setupRoutes() (http.Handler, error) {
	router := http.NewServeMux()
	h, err := handlers.New(s.config, s.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create handlers: %w", err)
	}

	s.handleFunc(router, "GET /health", h.HandleHealth)
	s.handleFunc(router, "/", h.HandleProxyCall)

	handler := http.Handler(router)

	return handler, nil
}

func (s *SidecarServer) handleFunc(router *http.ServeMux, pattern string, handler func(http.ResponseWriter, *http.Request)) {
	h := http.Handler(http.HandlerFunc(handler))
	if s.isOTELEnabled() {
		h = otelhttp.NewHandler(h, pattern)
		s.logger.Info("Enabled OTEL handler", "pattern", pattern)
	}
	router.Handle(pattern, h)
}

// SetupRoutes exposes the route setup for testing
func (s *SidecarServer) SetupRoutes() (http.Handler, error) {
	return s.setupRoutes()
}

func (s *SidecarServer) Start() error {
	handler, err := s.setupRoutes()
	if err != nil {
		return err
	}
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: handler,
		// ReadHeaderTimeout bounds slow-header attacks; ReadTimeout is left unset so the
		// server can receive large request bodies (e.g. inference payloads) without a deadline.
		ReadHeaderTimeout: 15 * time.Second,
		// WriteTimeout must be 0 for a reverse proxy: Go measures it from the moment the
		// request is received, so a non-zero value fires while the sidecar is still waiting
		// for the upstream (LiteLLM / eval-hub) to respond. Upstream latency is instead
		// bounded by each HTTP client's own Timeout (see http_client.go).
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		},
	}

	s.logger.Info("Sidecar server starting", "port", s.port)
	err = s.httpServer.ListenAndServe()

	if err == http.ErrServerClosed {
		s.logger.Info("Sidecar server closed gracefully")
		return &ServerClosedError{}
	}
	return err
}

func (s *SidecarServer) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down sidecar server gracefully...")
	return s.httpServer.Shutdown(ctx)
}

type ServerClosedError struct {
}

func (e *ServerClosedError) Error() string {
	return "Sidecar server closed"
}

func (e *ServerClosedError) Is(target error) bool {
	_, ok := target.(*ServerClosedError)
	return ok
}
