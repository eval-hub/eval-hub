package clients

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const eventsPath = "/api/v1/evaluations/jobs/%s/events"

// EvalHubClient posts status events to the eval-hub service REST API.
type EvalHubClient struct {
	BaseURL       string
	HTTPClient    *http.Client
	Logger        *slog.Logger
	authTokenPath string
	authToken     string
}

type SidecarResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// NewEvalHubClientFromConfig builds an EvalHubClient from EvalHubConfig, initializing the HTTP client
// with timeout, TLS (CA cert, insecure skip verify), and auth (token path / static token), similar to MLFlowClient.
// If isOTELEnabled is true, the client's transport is wrapped with OTEL instrumentation.
// Returns (nil, nil) when cfg is nil or BaseURL is empty.
func NewEvalHubClientFromConfig(cfg *config.EvalHubConfig, isOTELEnabled bool, logger *slog.Logger) (*EvalHubClient, error) {
	if cfg == nil || strings.TrimSpace(cfg.BaseURL) == "" {
		logger.Warn("EvalHub base URL is not set, skipping eval-hub client creation")
		return nil, nil
	}
	baseURL := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")

	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}

	if cfg.TLSConfig == nil {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		}
		if cfg.CACertPath != "" {
			caCert, err := os.ReadFile(cfg.CACertPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read EvalHub CA certificate at %s: %w", cfg.CACertPath, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse EvalHub CA certificate at %s: file contains no valid PEM certificates", cfg.CACertPath)
			}
			tlsConfig.RootCAs = caCertPool
			logger.Info("Loaded EvalHub CA certificate", "path", cfg.CACertPath)
		}
		if cfg.InsecureSkipVerify {
			tlsConfig.InsecureSkipVerify = true
			logger.Warn("EvalHub TLS certificate verification is disabled")
		}
		cfg.TLSConfig = tlsConfig
	}

	httpClient := &http.Client{
		Timeout: cfg.HTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: cfg.TLSConfig,
		},
	}

	if isOTELEnabled {
		httpClient = &http.Client{
			Transport: otelhttp.NewTransport(httpClient.Transport),
			Timeout:   httpClient.Timeout,
		}
		logger.Info("Enabled OTEL transport for EvalHub client")
	}

	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	return &EvalHubClient{
		BaseURL:       baseURL,
		HTTPClient:    httpClient,
		Logger:        logger,
		authTokenPath: tokenPath,
		authToken:     cfg.Token,
	}, nil
}

// NewEvalHubClient returns a client that posts to the given base URL (e.g. "https://eval.example.com").
// If httpClient is nil, http.DefaultClient is used. Prefer NewEvalHubClientFromConfig when config is available.
func NewEvalHubClient(baseURL string, httpClient *http.Client, logger *slog.Logger, authTokenPath string, authToken string) *EvalHubClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &EvalHubClient{BaseURL: baseURL, HTTPClient: httpClient, Logger: logger, authTokenPath: authTokenPath, authToken: authToken}
}

// PostEvent sends a status event for the given job ID.
// On success the server returns 204 No Content. Non-2xx responses return an error.
func (c *EvalHubClient) PostEvent(jobID string, body []byte) (*SidecarResponse, error) {
	if jobID == "" {
		return nil, fmt.Errorf("job id is required")
	}

	url := c.BaseURL + fmt.Sprintf(eventsPath, jobID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if token := c.resolveAuthToken(); token != "" {
		if strings.HasPrefix(token, "Bearer ") || strings.HasPrefix(token, "Basic ") {
			req.Header.Set("Authorization", token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	c.Logger.Info("Posting event to eval-hub", "url", url, "body", string(body))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.Logger.Error("Error posting event to eval-hub", "error", err)
		return nil, fmt.Errorf("post event: %w", err)
	}
	c.Logger.Info("Response from eval-hub", "status", resp.StatusCode, "headers", resp.Header, "body", string(body))
	defer resp.Body.Close()

	return &SidecarResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
	}, nil
}

// resolveAuthToken returns the auth token to use for a request.
// Token file (authTokenPath) takes precedence over a static token, supporting
// Kubernetes projected SA tokens that are rotated on disk by the kubelet.
// Falls back to the static authToken for local development.
func (c *EvalHubClient) resolveAuthToken() string {
	if c.authTokenPath != "" {
		tokenData, err := os.ReadFile(c.authTokenPath)
		if err != nil {
			c.Logger.Warn("Failed to read auth token file, falling back to static token", "path", c.authTokenPath, "error", err)
		} else if token := strings.TrimSpace(string(tokenData)); token != "" {
			return token
		}
	}
	return c.authToken
}
