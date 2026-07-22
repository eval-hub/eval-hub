package mlflowclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// API endpoint constants
const (
	// Version endpoint
	endpointVersion = "/version"

	// Base API path
	apiBasePath = "/api/2.0/mlflow"

	// Base URLs for API sections
	experimentsBaseURL = apiBasePath + "/experiments"

	// Experiments endpoints
	endpointExperimentsCreate        = experimentsBaseURL + "/create"
	endpointExperimentsGetBase       = experimentsBaseURL + "/get"
	endpointExperimentsGetByNameBase = experimentsBaseURL + "/get-by-name"
	endpointExperimentsDeleteBase    = experimentsBaseURL + "/delete"
)

// Client represents an MLflow API client
type Client struct {
	ctx                        context.Context
	baseURL                    string
	httpClient                 *http.Client
	authToken                  string
	authTokenPath              string
	authTokenPathWarningLogged atomic.Bool
	workspace                  string
	workspacesEnabled          bool
	logger                     *slog.Logger
}

func (c *Client) copy() *Client {
	if c == nil {
		return nil
	}
	cp := &Client{
		ctx:               c.ctx,
		baseURL:           c.baseURL,
		httpClient:        c.httpClient,
		authToken:         c.authToken,
		authTokenPath:     c.authTokenPath,
		workspace:         c.workspace,
		workspacesEnabled: c.workspacesEnabled,
		logger:            c.logger,
	}
	cp.authTokenPathWarningLogged.Store(c.authTokenPathWarningLogged.Load())
	return cp
}

// NewClient creates a new MLflow client
func NewClient(baseURL string) *Client {
	// Ensure baseURL doesn't end with a slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &Client{
		ctx:     context.Background(), // this is the default - it should be overridden for each API call using the WithContext method
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.New(slog.DiscardHandler),
	}
}

func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.httpClient = httpClient
	return cp
}

func (c *Client) WithContext(ctx context.Context) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.ctx = ctx
	return cp
}

func (c *Client) WithLogger(logger *slog.Logger) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.logger = logger
	return cp
}

// WithToken sets a static auth token (for local development without a token file).
func (c *Client) WithToken(authToken string) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.authToken = authToken
	return cp
}

// WithTokenPath sets a file path from which the auth token is read on each request.
// This supports Kubernetes projected ServiceAccount tokens that are rotated on disk.
// When set, takes precedence over a static token from WithToken.
func (c *Client) WithTokenPath(authTokenPath string) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.authTokenPath = authTokenPath
	return cp
}

// WithWorkspacesSupport records whether the server supports X-MLFLOW-WORKSPACE headers.
// Call ProbeWorkspacesEnabled during client setup, then pass the result here.
func (c *Client) WithWorkspacesSupport(enabled bool) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.workspacesEnabled = enabled
	if !enabled {
		cp.workspace = ""
	}
	return cp
}

// WithWorkspace sets the workspace name sent as X-MLFLOW-WORKSPACE when the server supports workspaces.
func (c *Client) WithWorkspace(workspace string) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		cp.workspace = ""
		return cp
	}
	if !cp.workspacesEnabled {
		cp.logger.Info(
			"MLflow workspaces not enabled on server; ignoring workspace",
			"workspace", workspace,
		)
		cp.workspace = ""
		return cp
	}
	cp.workspace = workspace
	return cp
}

func (c *Client) GetHTTPClient() *http.Client {
	return c.httpClient
}

func (c *Client) GetLogger() *slog.Logger {
	return c.logger
}

func (c *Client) GetBaseURL() string {
	return c.baseURL
}

func (c *Client) GetExperimentsURL() string {
	return c.baseURL + experimentsBaseURL
}

// resolveAuthToken returns the auth token to use for a request.
// Token file (authTokenPath) takes precedence over a static token, supporting
// Kubernetes projected SA tokens that are rotated on disk by the kubelet.
// Falls back to the static authToken for local development.
func (c *Client) resolveAuthToken() string {
	if c.authTokenPath != "" {
		tokenData, err := os.ReadFile(c.authTokenPath)
		if err != nil {
			if !c.authTokenPathWarningLogged.Load() {
				c.authTokenPathWarningLogged.Store(true)
				c.logger.Warn("Failed to read auth token file, falling back to static token", "path", c.authTokenPath, "error", err)
			}
		} else if token := strings.TrimSpace(string(tokenData)); token != "" {
			return token
		}
	}
	return c.authToken
}

func (c *Client) applyAuthHeader(req *http.Request) {
	token := c.resolveAuthToken()
	if token == "" {
		return
	}
	if strings.HasPrefix(token, "Bearer ") || strings.HasPrefix(token, "Basic ") {
		req.Header.Set("Authorization", token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

// doRequest performs an HTTP request to the MLflow API, including X-MLFLOW-WORKSPACE when configured.
func (c *Client) doRequest(method, endpoint string, body any) ([]byte, error) {
	return c.doRequestInternal(method, endpoint, body, true)
}

// doRequestWithoutWorkspace performs an MLflow API request without the X-MLFLOW-WORKSPACE header.
func (c *Client) doRequestWithoutWorkspace(method, endpoint string, body any) ([]byte, error) {
	return c.doRequestInternal(method, endpoint, body, false)
}

func (c *Client) doRequestInternal(method, endpoint string, body any, includeWorkspaceHeader bool) ([]byte, error) {
	c.logger.Info("MLFlow request started", "method", method, "endpoint", endpoint)

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			c.logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to marshal request body", "error", err.Error())
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	if c.ctx == nil {
		// this should never happen - the context should be set for each API call using the WithContext method
		c.logger.Error("context is nil for MLFlow request")
		return nil, fmt.Errorf("context is nil for MLFlow request")
	}

	req, err := http.NewRequestWithContext(c.ctx, method, c.baseURL+endpoint, reqBody)
	if err != nil {
		c.logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to create request", "error", err.Error())
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	c.applyAuthHeader(req)
	// X-MLFLOW-WORKSPACE scopes requests to a workspace on servers started with
	// --enable-workspaces (MLflow 3.10+). Sending it when workspaces are disabled
	// returns FEATURE_DISABLED from MLflow 3.13+.
	if includeWorkspaceHeader && c.workspacesEnabled && c.workspace != "" {
		req.Header.Set("X-MLFLOW-WORKSPACE", c.workspace)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to execute request", "error", err.Error())
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to read response body", "error", err.Error())
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		mlflowError := MLFlowError{}
		if err := json.Unmarshal(respBody, &mlflowError); err == nil {
			apiErr := &APIError{
				StatusCode:   resp.StatusCode,
				ResponseBody: string(respBody),
				MLFlowError:  &mlflowError,
			}
			c.logger.Info("MLFlow request failed", "method", method, "endpoint", endpoint, "status", resp.StatusCode, "error_code", mlflowError.ErrorCode, "message", mlflowError.Message)
			return nil, apiErr
		}
		apiErr := &APIError{
			StatusCode:   resp.StatusCode,
			ResponseBody: string(respBody),
			MLFlowError:  nil,
		}
		c.logger.Info("MLFlow request failed", "method", method, "endpoint", endpoint, "status", apiErr.StatusCode, "response", apiErr.ResponseBody)
		return nil, apiErr
	}

	c.logger.Info("MLFlow request successful", "method", method, "endpoint", endpoint, "status", resp.StatusCode, "response", string(respBody))
	return respBody, nil
}

// unmarshalResponse unmarshals JSON response body into a struct of type T
func unmarshalResponse[T any](respBody []byte) (*T, error) {
	var response T
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &response, nil
}

// Version API

// GetVersion returns the version of the MLflow server, or an error if it does not exist.
func (c *Client) GetVersion() (string, error) {
	if c == nil {
		return "", fmt.Errorf("mlflow client does not exist")
	}

	respBody, err := c.doRequestWithoutWorkspace(http.MethodGet, endpointVersion, nil)
	if err != nil {
		return "", err
	}
	return string(respBody), nil
}

// Experiments API

// CreateExperiment creates a new experiment
func (c *Client) CreateExperiment(req *CreateExperimentRequest) (*CreateExperimentResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("Create experiment request is nil")
	}
	respBody, err := c.doRequest(http.MethodPost, endpointExperimentsCreate, req)
	if err != nil {
		return nil, err
	}

	return unmarshalResponse[CreateExperimentResponse](respBody)
}

// GetExperiment gets an experiment by ID
func (c *Client) GetExperiment(experimentID string) (*GetExperimentResponse, error) {
	req := GetExperimentRequest{
		ExperimentID: experimentID,
	}
	respBody, err := c.doRequest(http.MethodGet, endpointExperimentsGetBase, req)
	if err != nil {
		return nil, err
	}

	return unmarshalResponse[GetExperimentResponse](respBody)
}

// GetExperimentByName gets an experiment by name
func (c *Client) GetExperimentByName(experimentName string) (*GetExperimentResponse, error) {
	req := GetExperimentByNameRequest{
		ExperimentName: experimentName,
	}
	respBody, err := c.doRequest(http.MethodGet, endpointExperimentsGetByNameBase, req)
	if err != nil {
		return nil, err
	}

	return unmarshalResponse[GetExperimentResponse](respBody)
}

// DeleteExperiment deletes an experiment
func (c *Client) DeleteExperiment(experimentID string) error {
	req := map[string]string{
		"experiment_id": experimentID,
	}
	_, err := c.doRequest(http.MethodPost, endpointExperimentsDeleteBase, req)
	return err
}
