package mlflowclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// API endpoint constants
const (
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
	Ctx        context.Context
	BaseURL    string
	HTTPClient *http.Client
	AuthToken  string
	Logger     *slog.Logger
}

// NewClient creates a new MLflow client
func NewClient(baseURL string) *Client {
	// Ensure baseURL doesn't end with a slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &Client{
		Ctx:     context.Background(),
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) WithContext(ctx context.Context) *Client {
	if c == nil {
		return nil
	}
	return &Client{
		Ctx:        ctx,
		BaseURL:    c.BaseURL,
		HTTPClient: c.HTTPClient,
		AuthToken:  c.AuthToken,
		Logger:     c.Logger,
	}
}

func (c *Client) WithLogger(logger *slog.Logger) *Client {
	if c == nil {
		return nil
	}
	return &Client{
		Ctx:        c.Ctx,
		BaseURL:    c.BaseURL,
		HTTPClient: c.HTTPClient,
		AuthToken:  c.AuthToken,
		Logger:     logger,
	}
}

// SetAuthToken sets the authentication token for the client
func (c *Client) SetAuthToken(token string) {
	c.AuthToken = token
}

// SetTimeout sets the HTTP client timeout
func (c *Client) SetTimeout(timeout time.Duration) {
	c.HTTPClient.Timeout = timeout
}

// doRequest performs an HTTP request to the MLflow API
func (c *Client) doRequest(ctx context.Context, method, endpoint string, body interface{}) ([]byte, error) {
	if c.Logger != nil {
		c.Logger.Info("MLFlow request started", "method", method, "endpoint", endpoint)
	}
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to marshal request body", "error", err.Error())
			}
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+endpoint, reqBody)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to create request", "error", err.Error())
		}
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to execute request", "error", err.Error())
		}
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Info("MLFlow request errored", "method", method, "endpoint", endpoint, "stage", "failed to read response body", "error", err.Error())
		}
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
			if c.Logger != nil {
				c.Logger.Info("MLFlow request failed", "method", method, "endpoint", endpoint, "status", resp.StatusCode, "error_code", mlflowError.ErrorCode, "message", mlflowError.Message)
			}
			return nil, apiErr
		}
		apiErr := &APIError{
			StatusCode:   resp.StatusCode,
			ResponseBody: string(respBody),
			MLFlowError:  nil,
		}
		if c.Logger != nil {
			c.Logger.Info("MLFlow request failed", "method", method, "endpoint", endpoint, "status", apiErr.StatusCode, "response", apiErr.ResponseBody)
		}
		return nil, apiErr
	}

	if c.Logger != nil {
		c.Logger.Info("MLFlow request successful", "method", method, "endpoint", endpoint, "status", resp.StatusCode, "response", string(respBody))
	}
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

// Experiments API

// CreateExperiment creates a new experiment
func (c *Client) CreateExperiment(req *CreateExperimentRequest) (*CreateExperimentResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("Create experiment request is nil")
	}
	respBody, err := c.doRequest(c.Ctx, http.MethodPost, endpointExperimentsCreate, req)
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
	respBody, err := c.doRequest(c.Ctx, http.MethodGet, endpointExperimentsGetBase, req)
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
	respBody, err := c.doRequest(c.Ctx, http.MethodGet, endpointExperimentsGetByNameBase, req)
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
	_, err := c.doRequest(c.Ctx, http.MethodPost, endpointExperimentsDeleteBase, req)
	return err
}
