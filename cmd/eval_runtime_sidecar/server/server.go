package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/auth"
	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/clients"
	handlers "github.com/eval-hub/eval-hub/eval_runtime_sidecar/handlers"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/google/uuid"
)

const (
	TRANSACTION_ID_HEADER = "X-Global-Transaction-Id"
	USER_HEADER           = "X-User"
	TENANT_HEADER         = "X-Tenant"
)

type SidecarServer struct {
	httpServer    *http.Server
	port          int
	logger        *slog.Logger
	serviceConfig *config.Config
	authConfig    *auth.AuthConfig
	evalHubClient *clients.EvalHubClient
	mlflowClient  *mlflowclient.Client
}

func (s *SidecarServer) isOTELEnabled() bool {
	return (s.serviceConfig != nil) && s.serviceConfig.IsOTELEnabled()
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
func NewSidecarServer(logger *slog.Logger,
	serviceConfig *config.Config,
	evalHubClient *clients.EvalHubClient,
	mlflowClient *mlflowclient.Client,
) (*SidecarServer, error) {

	if logger == nil {
		return nil, fmt.Errorf("logger is required for the server")
	}
	if serviceConfig == nil {
		return nil, fmt.Errorf("service config is required for the sidecar server")
	}

	if evalHubClient == nil {
		return nil, fmt.Errorf("eval hub client is required for the sidecar server")
	}

	port := 8080
	if serviceConfig.Service != nil {
		port = serviceConfig.Service.Port
	}

	return &SidecarServer{
		port:          port,
		logger:        logger,
		serviceConfig: serviceConfig,
		evalHubClient: evalHubClient,
		mlflowClient:  mlflowClient,
	}, nil
}

func (s *SidecarServer) GetPort() int {
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
func (s *SidecarServer) loggerWithRequest(r *http.Request) (string, *slog.Logger) {
	requestID := r.Header.Get(TRANSACTION_ID_HEADER)
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

func (s *SidecarServer) newExecutionContext(r *http.Request) *executioncontext.ExecutionContext {
	// Enhance logger with request-specific fields
	requestID, enhancedLogger := s.loggerWithRequest(r)

	user := r.Header.Get(USER_HEADER)
	tenant := r.Header.Get(TENANT_HEADER)

	// Use r.Context() so OTEL trace context (and the HTTP span from otelhttp) propagates
	// to handlers and downstream calls (storage, runtime, mlflow). Using context.Background()
	// would break parent-span linkage and create orphan traces.
	return executioncontext.NewExecutionContext(
		r.Context(),
		requestID,
		enhancedLogger,
		3,
		api.User(user),
		api.Tenant(tenant))
}

func (s *SidecarServer) handleFunc(router *http.ServeMux, pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.handle(router, pattern, http.HandlerFunc(handler))
}

func spanNameFormatter(operation string, r *http.Request) string {
	return fmt.Sprintf("%s %s", r.Method, operation)
}

func (s *SidecarServer) handle(router *http.ServeMux, pattern string, handler http.Handler) {
	if s.isOTELEnabled() {
		handler = otelhttp.NewHandler(handler, pattern, otelhttp.WithSpanNameFormatter(spanNameFormatter))
		s.logger.Info("Enabled OTEL handler", "pattern", pattern)
	}
	router.Handle(pattern, handler)
	s.logger.Info("Registered API", "pattern", pattern)
}

func (s *SidecarServer) setupHealthRoutes(h *handlers.Handlers, router *http.ServeMux) {
	s.handleFunc(router, "/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		resp := server.NewRespWrapper(w, ctx)
		req := server.NewRequestWrapper(r)
		switch req.Method() {
		case http.MethodGet:
			build, buildDate := "", ""
			if s.serviceConfig.Service != nil {
				build, buildDate = s.serviceConfig.Service.Build, s.serviceConfig.Service.BuildDate
			}
			h.HandleHealth(ctx, req, resp, build, buildDate)
		default:
			resp.ErrorWithMessageCode(ctx.RequestID, messages.MethodNotAllowed, "Method", req.Method(), "Api", req.URI())
		}
	})
}

func (s *SidecarServer) setupEvaluationJobEventsRoutes(h *handlers.Handlers, router *http.ServeMux) {
	s.handleFunc(router, fmt.Sprintf("/api/v1/evaluations/jobs/{%s}/events", constants.PATH_PARAMETER_JOB_ID), func(w http.ResponseWriter, r *http.Request) {
		ctx := s.newExecutionContext(r)
		switch r.Method {
		case http.MethodPost:
			h.HandleUpdateEvaluation(ctx, w, r)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message_code": "method_not_allowed",
				"message":      fmt.Sprintf("method %s is not allowed for %s", r.Method, r.URL.Path),
			})
		}
	})
}

func (s *SidecarServer) setupRoutes() (http.Handler, error) {
	router := http.NewServeMux()
	h := handlers.New(s.serviceConfig, s.evalHubClient)

	// Health
	s.setupHealthRoutes(h, router)

	s.setupEvaluationJobEventsRoutes(h, router)

	// Prometheus metrics endpoint
	prometheusEnabled := s.serviceConfig.IsPrometheusEnabled()
	if prometheusEnabled {
		//TODO: Explore sending metrics on sidecar shutdown
		//router.Handle("/metrics", promhttp.Handler())
		//s.logger.Info("Registered API", "pattern", "/metrics")
	}

	handler := http.Handler(router)

	// Wrap with metrics middleware (outermost for complete observability)
	handler = server.Middleware(handler, prometheusEnabled, s.logger)

	return handler, nil
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
	err = server.SetSidecarReady(s.serviceConfig, s.logger)
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
	s.logger.Info("Shutting down server gracefully...")
	return s.httpServer.Shutdown(ctx)
}

type ServerClosedError struct {
}

func (e *ServerClosedError) Error() string {
	return "Server closed"
}
