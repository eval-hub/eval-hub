package mlflowclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	endpointServerInfo   = "/api/3.0/mlflow/server-info"
	workspacesAPIBase    = "/api/3.0/mlflow/workspaces"
	defaultWorkspaceName = "default"
)

func workspaceEndpoint(name string) string {
	return workspacesAPIBase + "/" + url.PathEscape(name)
}

// ServerInfoResponse is the JSON body from GET /api/3.0/mlflow/server-info (MLflow 3.10+).
type ServerInfoResponse struct {
	WorkspacesEnabled bool `json:"workspaces_enabled"`
}

// ProbeWorkspacesEnabled queries the MLflow server-info endpoint (without a workspace header)
// and reports whether workspace-scoped APIs are available.
// Returns false for older servers that do not expose server-info (404).
func (c *Client) ProbeWorkspacesEnabled() (bool, error) {
	if c == nil {
		return false, fmt.Errorf("mlflow client does not exist")
	}
	if c.ctx == nil {
		return false, fmt.Errorf("context is nil for MLflow server-info request")
	}

	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, c.baseURL+endpointServerInfo, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create server-info request: %w", err)
	}
	c.applyAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to execute server-info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read server-info response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var info ServerInfoResponse
		if err := json.Unmarshal(respBody, &info); err != nil {
			return false, fmt.Errorf("failed to unmarshal server-info response: %w", err)
		}
		return info.WorkspacesEnabled, nil
	case http.StatusNotFound:
		// MLflow releases before workspace support do not expose this endpoint.
		return false, nil
	default:
		return false, fmt.Errorf("server-info returned status %d: %s", resp.StatusCode, string(respBody))
	}
}

// WorkspacesEnabled reports whether the client will send X-MLFLOW-WORKSPACE headers.
func (c *Client) WorkspacesEnabled() bool {
	if c == nil {
		return false
	}
	return c.workspacesEnabled
}

// GetWorkspace returns the named workspace, or an error if it does not exist.
func (c *Client) GetWorkspace(name string) (*Workspace, error) {
	if c == nil {
		return nil, fmt.Errorf("mlflow client does not exist")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("workspace name is empty")
	}

	respBody, err := c.doRequest(http.MethodGet, workspaceEndpoint(name), nil)
	if err != nil {
		return nil, err
	}

	resp, err := unmarshalResponse[GetWorkspaceResponse](respBody)
	if err != nil {
		return nil, err
	}
	return &resp.Workspace, nil
}

// CreateWorkspace creates a workspace without the X-MLFLOW-WORKSPACE header (global operation).
func (c *Client) CreateWorkspace(req *CreateWorkspaceRequest) (*Workspace, error) {
	if c == nil {
		return nil, fmt.Errorf("mlflow client does not exist")
	}
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("create workspace request is nil or missing name")
	}

	respBody, err := c.doRequestWithoutWorkspace(http.MethodPost, workspacesAPIBase, req)
	if err != nil {
		return nil, err
	}

	resp, err := unmarshalResponse[GetWorkspaceResponse](respBody)
	if err != nil {
		return nil, err
	}
	return &resp.Workspace, nil
}

// EnsureWorkspace creates the client's active workspace when workspaces are enabled.
// The reserved "default" workspace is assumed to exist. Idempotent for concurrent creators.
func (c *Client) EnsureWorkspace() error {
	if c == nil || !c.workspacesEnabled || strings.TrimSpace(c.workspace) == "" {
		return nil
	}
	name := strings.TrimSpace(c.workspace)
	if name == defaultWorkspaceName {
		return nil
	}

	_, err := c.GetWorkspace(name)
	if err == nil {
		return nil
	}
	if !IsResourceDoesNotExistError(err) {
		return err
	}

	_, err = c.CreateWorkspace(&CreateWorkspaceRequest{
		Name:        name,
		Description: "Created by eval-hub",
	})
	if err == nil {
		c.logger.Info("Created MLflow workspace", "workspace", name)
		return nil
	}
	if IsResourceAlreadyExistsError(err) {
		_, getErr := c.GetWorkspace(name)
		return getErr
	}
	return err
}
