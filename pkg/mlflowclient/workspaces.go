package mlflowclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	endpointServerInfo   = "/api/3.0/mlflow/server-info"
	workspacesAPIBase    = "/api/3.0/mlflow/workspaces"
	defaultWorkspaceName = "default"

	// One probe per ResolveWorkspaceSupport call. While support remains unknown,
	// the next MLflow-dependent job tries again.
	workspaceProbeTimeout = 5 * time.Second
)

// ErrWorkspaceSupportUnresolved is returned when server-info cannot be reached
// and workspace capability remains unknown.
var ErrWorkspaceSupportUnresolved = errors.New("could not detect MLflow workspace support")

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
// False when support is unknown or the server reported workspaces disabled.
func (c *Client) WorkspacesEnabled() bool {
	if c == nil || c.ws == nil {
		return false
	}
	return workspaceSupportKind(c.ws.support.Load()) == workspaceSupportEnabled
}

// WorkspaceSupportResolved reports whether ProbeWorkspacesEnabled has completed
// successfully (enabled or disabled). False means MLflow was unreachable and
// support will be probed again on the next EnsureWorkspace call.
func (c *Client) WorkspaceSupportResolved() bool {
	if c == nil || c.ws == nil {
		return false
	}
	return workspaceSupportKind(c.ws.support.Load()) != workspaceSupportUnknown
}

// ResolveWorkspaceSupport probes server-info once (5s timeout). Concurrent callers share
// one in-flight probe. The probe uses its own timeout so one caller's cancellation does
// not fail other waiters; each caller waits or cancels via ctx. On failure support stays
// unknown so a later call can try again. Once enabled or disabled, the result is kept
// for the process lifetime.
func (c *Client) ResolveWorkspaceSupport(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("mlflow client does not exist")
	}
	if c.ws == nil {
		return fmt.Errorf("mlflow client workspace capability is nil")
	}
	if ctx == nil {
		return fmt.Errorf("context is nil for workspace support probe")
	}
	// Cached capability wins over a cancelled ctx — no probe is needed.
	if c.WorkspaceSupportResolved() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.ws.mu.Lock()
	if c.WorkspaceSupportResolved() {
		c.ws.mu.Unlock()
		return nil
	}
	if call := c.ws.inflight; call != nil {
		c.ws.mu.Unlock()
		return c.waitForWorkspaceProbeCall(ctx, call)
	}
	call := &workspaceProbeCall{done: make(chan struct{})}
	c.ws.inflight = call
	c.ws.mu.Unlock()

	probeCtx, probeCancel := context.WithTimeout(context.Background(), workspaceProbeTimeout)
	go func() {
		err := c.runWorkspaceSupportProbe(probeCtx)
		probeCancel()
		c.ws.mu.Lock()
		call.err = err
		c.ws.inflight = nil
		close(call.done)
		c.ws.mu.Unlock()
	}()

	return c.waitForWorkspaceProbeCall(ctx, call)
}

func (c *Client) waitForWorkspaceProbeCall(ctx context.Context, call *workspaceProbeCall) error {
	if c.ws != nil && c.ws.onJoinWait != nil {
		c.ws.onJoinWait()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.done:
		return call.err
	}
}

func (c *Client) runWorkspaceSupportProbe(ctx context.Context) error {
	enabled, err := c.WithContext(ctx).ProbeWorkspacesEnabled()
	if err != nil {
		c.logger.Info("MLflow workspace support probe failed", "error", err.Error())
		return fmt.Errorf("%w: %v", ErrWorkspaceSupportUnresolved, err)
	}

	if enabled {
		c.ws.support.Store(int32(workspaceSupportEnabled))
	} else {
		if name := strings.TrimSpace(c.workspace); name != "" {
			c.logger.Warn(
				"MLFLOW_WORKSPACE is set but the MLflow server does not support workspaces; workspace headers will not be sent",
				"workspace", name,
			)
		}
		c.ws.support.Store(int32(workspaceSupportDisabled))
	}
	c.logger.Info("MLflow workspace support probed", "workspaces_enabled", enabled)
	return nil
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
// If workspace support has not been resolved yet, this probes before continuing.
func (c *Client) EnsureWorkspace() error {
	if c == nil {
		return fmt.Errorf("mlflow client does not exist")
	}
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.ResolveWorkspaceSupport(ctx); err != nil {
		return err
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
