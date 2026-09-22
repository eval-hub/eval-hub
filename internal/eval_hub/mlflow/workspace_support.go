package mlflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

const workspaceProbeTimeout = 5 * time.Second

// ErrWorkspaceSupportUnresolved is returned when server-info cannot be reached
// and workspace capability remains unknown.
var ErrWorkspaceSupportUnresolved = errors.New("could not detect MLflow workspace support")

type workspaceSupportKind int32

const (
	workspaceSupportUnknown workspaceSupportKind = iota
	workspaceSupportEnabled
	workspaceSupportDisabled
)

// WorkspaceSupport tracks process-wide MLflow workspace capability outside the
// HTTP client so the client stays a thin transport wrapper.
type WorkspaceSupport struct {
	support atomic.Int32 // workspaceSupportKind

	mu       sync.Mutex
	inflight *workspaceProbeCall

	// onJoinWait is invoked when a caller begins waiting on an in-flight probe.
	// Tests set this for explicit synchronization; production leaves it nil.
	onJoinWait func()
}

type workspaceProbeCall struct {
	done chan struct{}
	err  error
}

// NewWorkspaceSupport creates unresolved workspace capability state.
func NewWorkspaceSupport() *WorkspaceSupport {
	return &WorkspaceSupport{}
}

// Enabled reports whether workspace headers should be sent.
func (w *WorkspaceSupport) Enabled() bool {
	if w == nil {
		return false
	}
	return workspaceSupportKind(w.support.Load()) == workspaceSupportEnabled
}

// Resolved reports whether a definitive probe has completed (enabled or disabled).
func (w *WorkspaceSupport) Resolved() bool {
	if w == nil {
		return false
	}
	return workspaceSupportKind(w.support.Load()) != workspaceSupportUnknown
}

// Apply returns a client copy with WithWorkspacesSupport set from the cached capability.
// When still unknown, the client's enabled flag is left unchanged.
func (w *WorkspaceSupport) Apply(client *mlflowclient.Client) *mlflowclient.Client {
	if w == nil || client == nil || !w.Resolved() {
		return client
	}
	return client.WithWorkspacesSupport(w.Enabled())
}

// PrepareClient resolves workspace support then applies it to a client copy.
func (w *WorkspaceSupport) PrepareClient(ctx context.Context, client *mlflowclient.Client) (*mlflowclient.Client, error) {
	if client == nil {
		return nil, fmt.Errorf("mlflow client does not exist")
	}
	if w == nil {
		return client, nil
	}
	if err := w.Resolve(ctx, client); err != nil {
		return nil, err
	}
	return w.Apply(client), nil
}

// Resolve probes server-info once (5s timeout). Concurrent callers share one in-flight
// probe. On failure support stays unknown so a later call can try again. Once enabled
// or disabled, the result is kept for the process lifetime.
func (w *WorkspaceSupport) Resolve(ctx context.Context, client *mlflowclient.Client) error {
	if w == nil {
		return fmt.Errorf("workspace support is nil")
	}
	if client == nil {
		return fmt.Errorf("mlflow client does not exist")
	}
	if ctx == nil {
		return fmt.Errorf("context is nil for workspace support probe")
	}
	if w.Resolved() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	if w.Resolved() {
		w.mu.Unlock()
		return nil
	}
	if call := w.inflight; call != nil {
		w.mu.Unlock()
		return w.waitForProbeCall(ctx, call)
	}
	call := &workspaceProbeCall{done: make(chan struct{})}
	w.inflight = call
	w.mu.Unlock()

	probeCtx, probeCancel := context.WithTimeout(context.Background(), workspaceProbeTimeout)
	go func() {
		err := w.runProbe(probeCtx, client)
		probeCancel()
		w.mu.Lock()
		call.err = err
		w.inflight = nil
		close(call.done)
		w.mu.Unlock()
	}()

	return w.waitForProbeCall(ctx, call)
}

func (w *WorkspaceSupport) waitForProbeCall(ctx context.Context, call *workspaceProbeCall) error {
	if w.onJoinWait != nil {
		w.onJoinWait()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.done:
		return call.err
	}
}

func (w *WorkspaceSupport) runProbe(ctx context.Context, client *mlflowclient.Client) error {
	enabled, err := client.WithContext(ctx).ProbeWorkspacesEnabled()
	if err != nil {
		client.GetLogger().Info("MLflow workspace support probe failed", "error", err.Error())
		return fmt.Errorf("%w: %v", ErrWorkspaceSupportUnresolved, err)
	}

	if enabled {
		w.support.Store(int32(workspaceSupportEnabled))
	} else {
		if name := strings.TrimSpace(client.WorkspaceName()); name != "" {
			client.GetLogger().Warn(
				"MLFLOW_WORKSPACE is set but the MLflow server does not support workspaces; workspace headers will not be sent",
				"workspace", name,
			)
		}
		w.support.Store(int32(workspaceSupportDisabled))
	}
	client.GetLogger().Info("MLflow workspace support probed", "workspaces_enabled", enabled)
	return nil
}
