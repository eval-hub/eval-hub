package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricsServer struct {
	httpServer *http.Server
	port       int
	host       string
	logger     *slog.Logger
}

func NewMetricsServer(logger *slog.Logger, promConfig *config.PrometheusConfig) *MetricsServer {
	port := promConfig.EffectivePort()
	host := promConfig.EffectiveHost()

	mux := http.NewServeMux()
	mux.Handle("/metrics", withRequestID(promhttp.Handler()))
	// /healthz is the kubelet probe endpoint. It lives here (plain HTTP, 0.0.0.0:8081) so
	// the kubelet can reach it via the pod IP without going through kube-rbac-proxy.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(handlers.HealthzResponse{Status: handlers.STATUS_HEALTHY})
	})

	return &MetricsServer{
		httpServer: &http.Server{
			Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
			Handler:           mux,
			ReadTimeout:       15 * time.Second,
			ReadHeaderTimeout: 15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		port:   port,
		host:   host,
		logger: logger,
	}
}

type contextKeyRequestID struct{}

// withRequestID extracts X-Global-Transaction-Id from the request or generates a UUID,
// injects it into the request context and response header, then delegates to next.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(TRANSACTION_ID_HEADER)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), contextKeyRequestID{}, requestID)
		w.Header().Set(TRANSACTION_ID_HEADER, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *MetricsServer) Start() error {
	m.logger.Info("Metrics server starting", "addr", m.httpServer.Addr)
	err := m.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		m.logger.Info("Metrics server closed gracefully")
		return nil
	}
	return err
}

func (m *MetricsServer) Shutdown(ctx context.Context) error {
	m.logger.Info("Shutting down metrics server...")
	return m.httpServer.Shutdown(ctx)
}

func (m *MetricsServer) GetPort() int {
	return m.port
}

func (m *MetricsServer) Handler() http.Handler {
	return m.httpServer.Handler
}
