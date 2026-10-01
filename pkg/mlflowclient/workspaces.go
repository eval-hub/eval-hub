package mlflowclient

import (
	"fmt"
	"strings"

	mlflow "github.com/opendatahub-io/mlflow-go/mlflow"
)

const defaultWorkspaceName = "default"

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
	d, err := c.resolveDelegate()
	if err != nil {
		return false, err
	}
	info, err := d.Workspaces().GetServerInfo(c.Context())
	if err != nil {
		// MLflow releases before workspace support do not expose this endpoint.
		if mlflow.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return info.WorkspacesEnabled, nil
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
	d, err := c.resolveDelegate()
	if err != nil {
		return nil, err
	}
	ws, err := d.Workspaces().GetWorkspace(c.Context(), name)
	if err != nil {
		return nil, mapError(err)
	}
	return &Workspace{Name: ws.Name}, nil
}

// CreateWorkspace creates a workspace without the X-MLFLOW-WORKSPACE header (global operation).
// The mlflow-go Workspace type has no description field, so CreateWorkspaceRequest.Description
// is ignored.
func (c *Client) CreateWorkspace(req *CreateWorkspaceRequest) (*Workspace, error) {
	if c == nil {
		return nil, fmt.Errorf("mlflow client does not exist")
	}
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("create workspace request is nil or missing name")
	}
	d, err := c.resolveDelegate()
	if err != nil {
		return nil, err
	}
	ws, err := d.Workspaces().CreateWorkspace(c.Context(), req.Name)
	if err != nil {
		return nil, mapError(err)
	}
	return &Workspace{Name: ws.Name}, nil
}

// EnsureWorkspace creates the client's active workspace when workspaces are enabled.
// The reserved "default" workspace is assumed to exist. It uses get-first
// semantics — the workspace usually already exists, so a needless create (and the
// write it implies) is avoided — and tolerates a concurrent creator.
// Callers must set WithWorkspacesSupport after resolving capability (service layer).
func (c *Client) EnsureWorkspace() error {
	if c == nil {
		return fmt.Errorf("mlflow client does not exist")
	}
	if !c.WorkspacesEnabled() {
		return nil
	}
	name := strings.TrimSpace(c.configuredWorkspaceName())
	if name == "" {
		return nil
	}
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

	_, err = c.CreateWorkspace(&CreateWorkspaceRequest{Name: name})
	if err == nil {
		c.logger.Info("Created MLflow workspace", "workspace", name)
		return nil
	}
	// A concurrent creator won the race; confirm the workspace now exists.
	if IsResourceAlreadyExistsError(err) {
		_, getErr := c.GetWorkspace(name)
		return getErr
	}
	return err
}
