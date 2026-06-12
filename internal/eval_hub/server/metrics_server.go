package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
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
	mux.Handle("/metrics", promhttp.Handler())

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
