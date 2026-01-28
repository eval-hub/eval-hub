package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	httpServer      *http.Server
	port            int
	logger          *slog.Logger
	serviceConfig   *config.Config
	providerConfigs map[string]*api.ProviderResource
	storage         abstractions.Storage
	validate        *validator.Validate
}

// NewServer creates a new HTTP server instance with the provided logger and configuration.
// The server uses standard library net/http.ServeMux for routing without a web framework.
//
// The server implements the routing pattern where:
//   - Basic handlers (health, status, OpenAPI) receive http.ResponseWriter, *http.Request
//   - Evaluation-related handlers receive *ExecutionContext, http.ResponseWriter, *http.Request
//   - ExecutionContext is created at the route level before calling handlers
//   - Routes manually switch on HTTP method in handler functions
//
// All routes are wrapped with Prometheus metrics middleware for request duration and
// status code tracking.
//
// Parameters:
//   - logger: The structured logger for the server
//   - serviceConfig: The service configuration containing port and other settings
//
// Returns:
//   - *Server: A configured server instance
//   - error: An error if logger or serviceConfig is nil
func NewServer(logger *slog.Logger, serviceConfig *config.Config, providerConfigs map[string]*api.ProviderResource, storage abstractions.Storage, validate *validator.Validate) (*Server, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required for the server")
	}
	if (serviceConfig == nil) || (serviceConfig.Service == nil) {
		return nil, fmt.Errorf("service config is required for the server")
	}
	if storage == nil {
		return nil, fmt.Errorf("executioncontext is required for the server")
	}
	if validate == nil {
		return nil, fmt.Errorf("validator is required for the server")
	}

	return &Server{
		port:            serviceConfig.Service.Port,
		logger:          logger,
		serviceConfig:   serviceConfig,
		providerConfigs: providerConfigs,
		storage:         storage,
		validate:        validate,
	}, nil
}

func (s *Server) GetPort() int {
	return s.port
}

// LoggerWithRequest enhances a logger with request-specific fields for distributed
// tracing and structured logging. This function is called when creating an ExecutionContext
// to automatically enrich all log entries for a given HTTP request with consistent metadata.
//
// The enhanced logger includes the following fields (when available):
//   - request_id: Extracted from X-Global-Transaction-Id header, or auto-generated UUID if missing
//   - method: HTTP method (GET, POST, etc.)
//   - uri: Request path (from URL.Path or RequestURI)
//   - user_agent: Client user agent from User-Agent header
//   - remote_addr: Client IP address
//   - remote_user: Authenticated user from URL user info or Remote-User header
//   - referer: HTTP referer header
//
// This enables correlating logs across services using the request_id and provides
// comprehensive request context in all log entries.
//
// Parameters:
//   - logger: The base logger to enhance
//   - r: The HTTP request to extract fields from
//
// Returns:
//   - *slog.Logger: A new logger instance with request-specific fields attached
func (s *Server) loggerWithRequest(r *http.Request) (string, *slog.Logger) {
	requestID := r.Header.Get("X-Global-Transaction-Id")
	if requestID == "" {
		requestID = uuid.New().String() // generate a UUID if not present
	}

	enhancedLogger := s.logger.With(constants.LOG_REQUEST_ID, requestID)

	// Extract and add HTTP method and URI if they exist
	method := r.Method
	if method != "" {
		enhancedLogger = enhancedLogger.With(constants.LOG_METHOD, method)
	}

	uri := ""
	if r.URL != nil {
		uri = r.URL.Path
	}
	if uri == "" {
		uri = r.RequestURI
	}
	if uri != "" {
		enhancedLogger = enhancedLogger.With(constants.LOG_URI, uri)
	}

	// Extract and add HTTP request fields to logger if they exist
	userAgent := r.Header.Get("User-Agent")
	if userAgent != "" {
		enhancedLogger = enhancedLogger.With(constants.LOG_USER_AGENT, userAgent)
	}

	remoteAddr := r.RemoteAddr
	if remoteAddr != "" {
		enhancedLogger = enhancedLogger.With(constants.LOG_REMOTE_ADR, remoteAddr)
	}

	// Extract remote_user from URL user info or header
	remoteUser := ""
	if r.URL != nil && r.URL.User != nil {
		remoteUser = r.URL.User.Username()
	}
	if remoteUser == "" {
		remoteUser = r.Header.Get("Remote-User")
	}
	if remoteUser != "" {
		enhancedLogger = enhancedLogger.With(constants.LOG_USER, remoteUser)
	}

	referer := r.Header.Get("Referer")
	if referer != "" {
		enhancedLogger = enhancedLogger.With(constants.LOG_REFERER, referer)
	}

	return requestID, enhancedLogger
}

func (s *Server) setupRoutes() (http.Handler, error) {
	router := http.NewServeMux()
	h := handlers.New(s.storage, s.validate)

	// Health and status endpoints
	router.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleHealth(ctx, w)
	})
	router.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleStatus(ctx, w)
	})

	// Evaluation jobs endpoints
	router.HandleFunc("/api/v1/evaluations/jobs", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		switch r.Method {
		case http.MethodPost:
			h.HandleCreateEvaluation(ctx, w)
		case http.MethodGet:
			h.HandleListEvaluations(ctx, w)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	// Handle summary endpoint first (more specific)
	router.HandleFunc("/api/v1/evaluations/jobs/", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		path := r.URL.Path
		if strings.HasSuffix(path, "/summary") && r.Method == http.MethodGet {
			h.HandleGetEvaluationSummary(ctx, w)
			return
		}
		// Handle individual job endpoints
		switch r.Method {
		case http.MethodGet:
			h.HandleGetEvaluation(ctx, w)
		case http.MethodDelete:
			h.HandleCancelEvaluation(ctx, w)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Benchmarks endpoint
	router.HandleFunc("/api/v1/evaluations/benchmarks", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleListBenchmarks(ctx, w)
	})

	// Collections endpoints
	router.HandleFunc("/api/v1/evaluations/collections", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		switch r.Method {
		case http.MethodPost:
			h.HandleCreateCollection(ctx, w)
		case http.MethodGet:
			h.HandleListCollections(ctx, w)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	router.HandleFunc("/api/v1/evaluations/collections/", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		switch r.Method {
		case http.MethodGet:
			h.HandleGetCollection(ctx, w)
		case http.MethodPut:
			h.HandleUpdateCollection(ctx, w)
		case http.MethodPatch:
			h.HandlePatchCollection(ctx, w)
		case http.MethodDelete:
			h.HandleDeleteCollection(ctx, w)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Providers endpoints
	router.HandleFunc("/api/v1/evaluations/providers", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleListProviders(ctx, w)
	})

	router.HandleFunc("/api/v1/evaluations/providers/", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleGetProvider(ctx, w)
	})

	// System metrics endpoint
	router.HandleFunc("/api/v1/metrics/system", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleGetSystemMetrics(ctx, w)
	})

	// OpenAPI documentation endpoints
	router.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleOpenAPI(ctx, w)
	})
	router.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		h.HandleDocs(ctx, w)
	})

	// Prometheus metrics endpoint
	router.Handle("/metrics", promhttp.Handler())

	// Wrap router with metrics middleware
	return Middleware(router), nil
}

// SetupRoutes exposes the route setup for testing
func (s *Server) SetupRoutes() (http.Handler, error) {
	return s.setupRoutes()
}

func (s *Server) Start() error {
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

	s.logger.Info("Writing the server ready message", "file", s.serviceConfig.Service.ReadyFile)
	err = SetReady(s.serviceConfig, s.logger)
	if err != nil {
		return err
	}

	s.logger.Info("Server starting", "port", s.port)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}

	s.logger.Info("Shutting down server gracefully...")
	// do we need to flush the logs?

	return s.httpServer.Shutdown(ctx)
}
