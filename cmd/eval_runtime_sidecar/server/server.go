package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	handlers "github.com/eval-hub/eval-hub/eval_runtime_sidecar/handlers"
	"github.com/eval-hub/eval-hub/internal/config"
)

type SidecarServer struct {
	httpServer    *http.Server
	port          int
	logger        *slog.Logger
	serviceConfig *config.Config
}

// NewSidecarServer creates a new sidecar HTTP server with the given logger and config.
func NewSidecarServer(logger *slog.Logger,
	serviceConfig *config.Config,
) (*SidecarServer, error) {

	if logger == nil {
		return nil, fmt.Errorf("logger is required for the server")
	}
	if serviceConfig == nil {
		return nil, fmt.Errorf("service config is required for the sidecar server")
	}

	port := 8080
	if serviceConfig.Service != nil {
		port = serviceConfig.Service.Port
	}

	return &SidecarServer{
		port:          port,
		logger:        logger,
		serviceConfig: serviceConfig,
	}, nil
}

func (s *SidecarServer) GetPort() int {
	return s.port
}

func (s *SidecarServer) setupRoutes() (http.Handler, error) {
	router := http.NewServeMux()
	h, err := handlers.New(s.serviceConfig, s.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create handlers: %w", err)
	}

	s.setupAuthProxyRoutes(h, router)

	handler := http.Handler(router)

	return handler, nil
}

func (s *SidecarServer) setupAuthProxyRoutes(h *handlers.Handlers, router *http.ServeMux) {
	router.HandleFunc("/v1/evaluations/", h.HandleEvalHubProxy)
	router.HandleFunc("/mlflow/", h.HandleMLflowProxy)
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
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	readyFile := ""
	if s.serviceConfig.Service != nil {
		readyFile = s.serviceConfig.Service.ReadyFile
	}
	s.logger.Info("Writing the server ready message", "file", readyFile)
	err = server.SetReady(s.serviceConfig, s.logger)
	if err != nil {
		return err
	}

	s.logger.Info("Server starting", "port", s.port)
	err = s.httpServer.ListenAndServe()

	if err == http.ErrServerClosed {
		s.logger.Info("Server closed gracefully")
		return &ServerClosedError{}
	}
	return err
}

func (s *SidecarServer) Shutdown(ctx context.Context) error {
	//TODO: Explore sending metrics on sidecar shutdown
	s.logger.Info("Shutting down server gracefully...")
	return s.httpServer.Shutdown(ctx)
}

type ServerClosedError struct {
}

func (e *ServerClosedError) Error() string {
	return "Server closed"
}
