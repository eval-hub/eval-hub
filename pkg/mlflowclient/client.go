// Package mlflowclient is a thin adapter shim over the Red Hat Go SDK for MLflow
// (github.com/opendatahub-io/mlflow-go). It preserves the historical eval-hub
// client API — the fluent, immutable builder and the request/response structs —
// so callers in internal/eval_hub/* do not change, while delegating all
// wire-level MLflow protocol handling to mlflow-go.
//
// The underlying mlflow-go client is built lazily on first use from the builder
// configuration. The per-client context recorded via WithContext is passed into
// every mlflow-go call.
package mlflowclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mlflow "github.com/opendatahub-io/mlflow-go/mlflow"
	"github.com/opendatahub-io/mlflow-go/mlflow/tracking"
)

// API path constants retained for URL-building helpers (GetExperimentsURL and the
// artifact URL returned by UploadArtifact). The wire requests themselves are
// issued by mlflow-go.
const (
	// Base API path
	apiBasePath = "/api/2.0/mlflow"

	// Base URLs for API sections
	experimentsBaseURL = apiBasePath + "/experiments"
)

// Client is an MLflow API client. It presents the historical eval-hub API but
// delegates to an mlflow-go client built lazily from the builder configuration.
//
// The builder methods (WithX) return immutable copies; each copy builds its own
// mlflow-go client on first use.
type Client struct {
	ctx               context.Context
	baseURL           string
	httpClient        *http.Client
	authToken         string
	authTokenPath     string
	workspace         string
	workspacesEnabled bool
	logger            *slog.Logger

	// Lazily-built mlflow-go delegate, scoped to this Client copy.
	mu       sync.Mutex
	delegate *mlflow.Client
	built    bool
	buildErr error

	insecureAuthWarned atomic.Bool
}

// copy returns a builder copy carrying the configuration only; the lazily-built
// delegate is intentionally not carried over so it is rebuilt from the new
// configuration on first use.
func (c *Client) copy() *Client {
	if c == nil {
		return nil
	}
	return &Client{
		ctx:               c.ctx,
		baseURL:           c.baseURL,
		httpClient:        c.httpClient,
		authToken:         c.authToken,
		authTokenPath:     c.authTokenPath,
		workspace:         c.workspace,
		workspacesEnabled: c.workspacesEnabled,
		logger:            c.logger,
	}
}

// NewClient creates a new MLflow client for the given tracking base URL.
func NewClient(baseURL string) *Client {
	// Ensure baseURL doesn't end with a slash
	baseURL = strings.TrimRight(baseURL, "/")

	return &Client{
		ctx:     context.Background(), // default; override per API call with WithContext
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
// Call ProbeWorkspacesEnabled (or the service-layer WorkspaceSupport.Resolve) during
// setup, then pass the result here.
func (c *Client) WithWorkspacesSupport(enabled bool) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.workspacesEnabled = enabled
	return cp
}

// WithWorkspace sets the workspace name sent as X-MLFLOW-WORKSPACE when workspaces are enabled.
func (c *Client) WithWorkspace(workspace string) *Client {
	if c == nil {
		return nil
	}
	cp := c.copy()
	cp.workspace = strings.TrimSpace(workspace)
	return cp
}

func (c *Client) configuredWorkspaceName() string {
	if c == nil {
		return ""
	}
	return c.workspace
}

// WorkspaceName returns the configured X-MLFLOW-WORKSPACE value for this client copy.
func (c *Client) WorkspaceName() string {
	return c.configuredWorkspaceName()
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

// Context returns the request context associated with this client copy.
func (c *Client) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *Client) GetExperimentsURL() string {
	return c.baseURL + experimentsBaseURL
}

// isInsecureScheme reports whether the tracking URL uses plain HTTP. mlflow-go
// refuses plain HTTP unless insecure mode is enabled, and refuses to send
// credentials over an insecure connection; both are handled in newDelegate.
func isInsecureScheme(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http")
}

// resolveDelegate builds (once) and returns the mlflow-go client for this copy.
func (c *Client) resolveDelegate() (*mlflow.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("mlflow client does not exist")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.built {
		return c.delegate, c.buildErr
	}
	c.delegate, c.buildErr = c.newDelegate()
	c.built = true
	return c.delegate, c.buildErr
}

// newDelegate constructs an mlflow-go client from the builder configuration.
func (c *Client) newDelegate() (*mlflow.Client, error) {
	opts := []mlflow.Option{mlflow.WithTrackingURI(c.baseURL)}

	insecure := isInsecureScheme(c.baseURL)
	if insecure {
		opts = append(opts, mlflow.WithInsecure())
	}
	if c.httpClient != nil {
		opts = append(opts, mlflow.WithHTTPClient(c.httpClient))
	}
	if c.logger != nil {
		opts = append(opts, mlflow.WithLogger(c.logger.Handler()))
	}

	// mlflow-go refuses to send credentials over an insecure (plain HTTP)
	// connection. Over HTTPS the token/token-file options map directly; over
	// plain HTTP they are dropped so the client can still be constructed.
	if !insecure {
		if c.authTokenPath != "" {
			opts = append(opts, mlflow.WithTokenPath(c.authTokenPath))
		}
		if c.authToken != "" {
			opts = append(opts, mlflow.WithToken(c.authToken))
		}
	} else if (c.authToken != "" || c.authTokenPath != "") && !c.insecureAuthWarned.Swap(true) {
		c.logger.Warn("MLflow auth token configured but tracking URI is plain HTTP; credentials will not be sent")
	}

	// Attach the workspace header on non-exempt endpoints when workspaces are
	// enabled and a workspace is configured. mlflow-go's transport already
	// exempts server-info and workspace-management endpoints. We do not enable
	// mlflow-go's own probe (WithWorkspacesSupport): the eval-hub service layer
	// resolves capability via ProbeWorkspacesEnabled and records the result here.
	if c.workspacesEnabled && c.workspace != "" {
		opts = append(opts, mlflow.WithWorkspace(c.workspace))
	}

	return mlflow.NewClient(opts...)
}

func (c *Client) tracking() (*tracking.Client, error) {
	d, err := c.resolveDelegate()
	if err != nil {
		return nil, err
	}
	return d.Tracking(), nil
}

// GetVersion returns the version of the MLflow server, or an error if it does not exist.
func (c *Client) GetVersion() (string, error) {
	t, err := c.tracking()
	if err != nil {
		return "", err
	}
	version, err := t.GetVersion(c.Context())
	if err != nil {
		return "", mapError(err)
	}
	return version, nil
}

// CreateExperiment creates a new experiment.
func (c *Client) CreateExperiment(req *CreateExperimentRequest) (*CreateExperimentResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("Create experiment request is nil")
	}
	t, err := c.tracking()
	if err != nil {
		return nil, err
	}
	id, err := t.CreateExperiment(c.Context(), req.Name, createExperimentOptions(req)...)
	if err != nil {
		return nil, mapError(err)
	}
	return &CreateExperimentResponse{ExperimentID: id}, nil
}

// GetExperiment gets an experiment by ID.
func (c *Client) GetExperiment(experimentID string) (*GetExperimentResponse, error) {
	t, err := c.tracking()
	if err != nil {
		return nil, err
	}
	exp, err := t.GetExperiment(c.Context(), experimentID)
	if err != nil {
		return nil, mapError(err)
	}
	return &GetExperimentResponse{Experiment: experimentFromSDK(exp)}, nil
}

// GetExperimentByName gets an experiment by name.
func (c *Client) GetExperimentByName(experimentName string) (*GetExperimentResponse, error) {
	t, err := c.tracking()
	if err != nil {
		return nil, err
	}
	exp, err := t.GetExperimentByName(c.Context(), experimentName)
	if err != nil {
		return nil, mapError(err)
	}
	return &GetExperimentResponse{Experiment: experimentFromSDK(exp)}, nil
}

// DeleteExperiment deletes an experiment.
func (c *Client) DeleteExperiment(experimentID string) error {
	t, err := c.tracking()
	if err != nil {
		return err
	}
	if err := t.DeleteExperiment(c.Context(), experimentID); err != nil {
		return mapError(err)
	}
	return nil
}
