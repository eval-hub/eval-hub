package evalhub_client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3

	apiBasePath = "/api/v1/evaluations"
	healthPath  = "/api/v1/health"
)

// APIError represents a typed error returned by the eval-hub API.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Trace      string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("eval-hub API error (HTTP %d, code=%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("eval-hub API error (HTTP %d): %s", e.StatusCode, e.Message)
}

func (e *APIError) IsNotFound() bool     { return e.StatusCode == http.StatusNotFound }
func (e *APIError) IsUnauthorized() bool { return e.StatusCode == http.StatusUnauthorized }
func (e *APIError) IsForbidden() bool    { return e.StatusCode == http.StatusForbidden }

// Client is an HTTP client for the eval-hub REST API.
// Construct via NewClient; use With* methods to configure auth, TLS, logging, and timeouts.
//
//	client := evalhub_client.NewClient("https://evalhub:8080").
//	    WithToken(token).
//	    WithTenant("my-namespace").
//	    WithLogger(logger)
type Client struct {
	ctx        context.Context
	baseURL    string
	token      string
	tenant     string
	httpClient *http.Client
	logger     *slog.Logger
	maxRetries int
}

// NewClient creates a new eval-hub API client. Trailing slashes in baseURL are stripped.
func NewClient(baseURL string) *Client {
	return &Client{
		ctx:     context.Background(),
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger:     slog.New(slog.DiscardHandler),
		maxRetries: defaultMaxRetries,
	}
}

// WithContext returns a copy of the client that uses ctx for all subsequent requests.
func (c *Client) WithContext(ctx context.Context) *Client {
	cp := *c
	cp.ctx = ctx
	return &cp
}

// WithToken returns a copy of the client that sends token as a Bearer Authorization header.
func (c *Client) WithToken(token string) *Client {
	cp := *c
	cp.token = token
	return &cp
}

// WithTenant returns a copy of the client that sends the X-Tenant header on every request.
func (c *Client) WithTenant(tenant string) *Client {
	cp := *c
	cp.tenant = tenant
	return &cp
}

// WithLogger returns a copy of the client that uses logger for structured request logging.
func (c *Client) WithLogger(logger *slog.Logger) *Client {
	cp := *c
	cp.logger = logger
	return &cp
}

// WithTimeout returns a copy of the client with the given HTTP request timeout.
func (c *Client) WithTimeout(timeout time.Duration) *Client {
	cp := *c
	cp.httpClient = &http.Client{
		Timeout:   timeout,
		Transport: cp.httpClient.Transport,
	}
	return &cp
}

// WithInsecureSkipVerify returns a copy of the client that skips TLS certificate verification.
// Use only in development or test environments.
func (c *Client) WithInsecureSkipVerify() *Client {
	cp := *c
	cp.httpClient = &http.Client{
		Timeout: cp.httpClient.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402
		},
	}
	return &cp
}

// WithMaxRetries returns a copy of the client that retries up to n times on transient failures.
func (c *Client) WithMaxRetries(n int) *Client {
	cp := *c
	cp.maxRetries = n
	return &cp
}

// WithHTTPClient returns a copy of the client using the given http.Client (useful for testing).
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	cp := *c
	cp.httpClient = httpClient
	return &cp
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.tenant != "" {
		req.Header.Set("X-Tenant", c.tenant)
	}
}

// doRequest executes an HTTP request, retrying on network failures and 5xx responses.
// Returns the response body, HTTP status code, and any error.
func (c *Client) doRequest(method, path string, body any, queryParams url.Values) ([]byte, int, error) {
	fullURL := c.baseURL + path
	if len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	var (
		respBody   []byte
		statusCode int
		lastErr    error
	)

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<(attempt-1)) * 500 * time.Millisecond)
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewBuffer(bodyBytes)
		}

		req, err := http.NewRequestWithContext(c.ctx, method, fullURL, reqBody)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		c.setHeaders(req)

		c.logger.Debug("eval-hub request", "method", method, "path", path, "attempt", attempt+1)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt < c.maxRetries {
				c.logger.Warn("eval-hub request failed, retrying", "method", method, "path", path, "attempt", attempt+1, "error", err)
				continue
			}
			return nil, 0, lastErr
		}

		statusCode = resp.StatusCode
		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, statusCode, fmt.Errorf("failed to read response body: %w", err)
		}

		// Retry server errors; propagate client errors immediately.
		if statusCode >= 500 && attempt < c.maxRetries {
			c.logger.Warn("eval-hub server error, retrying", "method", method, "path", path, "status", statusCode, "attempt", attempt+1)
			lastErr = c.parseAPIError(statusCode, respBody)
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, statusCode, lastErr
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, statusCode, c.parseAPIError(statusCode, respBody)
	}

	c.logger.Debug("eval-hub request succeeded", "method", method, "path", path, "status", statusCode)
	return respBody, statusCode, nil
}

func (c *Client) parseAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}
	if len(body) > 0 {
		var errResp api.Error
		if err := json.Unmarshal(body, &errResp); err == nil {
			apiErr.Code = errResp.MessageCode
			apiErr.Message = errResp.Message
			apiErr.Trace = errResp.Trace
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(statusCode)
	}
	return apiErr
}

func decode[T any](body []byte) (*T, error) {
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ─── Health ──────────────────────────────────────────────────────────────────

// GetHealth returns the current health status of the eval-hub service.
func (c *Client) GetHealth() (*api.HealthResponse, error) {
	body, _, err := c.doRequest(http.MethodGet, healthPath, nil, nil)
	if err != nil {
		return nil, err
	}
	return decode[api.HealthResponse](body)
}

// ─── Providers ───────────────────────────────────────────────────────────────

// ListProviders returns all registered evaluation providers. Use WithLimit/WithOffset for pagination.
func (c *Client) ListProviders(opts ...ListOption) (*api.ProviderResourceList, error) {
	body, _, err := c.doRequest(http.MethodGet, apiBasePath+"/providers", nil, applyListOptions(opts))
	if err != nil {
		return nil, err
	}
	return decode[api.ProviderResourceList](body)
}

// GetProvider returns the provider with the given ID.
func (c *Client) GetProvider(id string) (*api.ProviderResource, error) {
	body, _, err := c.doRequest(http.MethodGet, apiBasePath+"/providers/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	return decode[api.ProviderResource](body)
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────
//
// Benchmarks are nested within providers (no dedicated /benchmarks endpoint).
// These helpers aggregate across all provider pages automatically.

// ListBenchmarks returns all benchmarks across all registered providers.
func (c *Client) ListBenchmarks() ([]api.BenchmarkResource, error) {
	return c.listAllBenchmarks(nil)
}

// GetBenchmark searches all providers for a benchmark with the given ID.
// Returns an APIError with HTTP 404 if no match is found.
func (c *Client) GetBenchmark(id string) (*api.BenchmarkResource, error) {
	providers, err := c.allProviders()
	if err != nil {
		return nil, err
	}
	for i := range providers {
		for j := range providers[i].Benchmarks {
			if providers[i].Benchmarks[j].ID == id {
				b := providers[i].Benchmarks[j]
				return &b, nil
			}
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("benchmark %q not found", id),
	}
}

// ListBenchmarksByLabel returns benchmarks whose tags contain all of the given labels.
func (c *Client) ListBenchmarksByLabel(labels []string) ([]api.BenchmarkResource, error) {
	return c.listAllBenchmarks(labels)
}

// allProviders fetches every provider page and returns the combined slice.
func (c *Client) allProviders() ([]api.ProviderResource, error) {
	const pageSize = 100
	var all []api.ProviderResource
	for offset := 0; ; offset += pageSize {
		list, err := c.ListProviders(WithLimit(pageSize), WithOffset(offset))
		if err != nil {
			return nil, err
		}
		all = append(all, list.Items...)
		if len(all) >= list.TotalCount || len(list.Items) < pageSize {
			break
		}
	}
	return all, nil
}

func (c *Client) listAllBenchmarks(labels []string) ([]api.BenchmarkResource, error) {
	providers, err := c.allProviders()
	if err != nil {
		return nil, err
	}
	var benchmarks []api.BenchmarkResource
	for _, p := range providers {
		for _, b := range p.Benchmarks {
			if matchesAllLabels(b.Tags, labels) {
				benchmarks = append(benchmarks, b)
			}
		}
	}
	return benchmarks, nil
}

// matchesAllLabels returns true when tags contains every element of required.
func matchesAllLabels(tags, required []string) bool {
	if len(required) == 0 {
		return true
	}
	tagSet := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		tagSet[t] = struct{}{}
	}
	for _, l := range required {
		if _, ok := tagSet[l]; !ok {
			return false
		}
	}
	return true
}

// ─── Collections ─────────────────────────────────────────────────────────────

// ListCollections returns all benchmark collections. Use WithLimit/WithOffset for pagination.
func (c *Client) ListCollections(opts ...ListOption) (*api.CollectionResourceList, error) {
	body, _, err := c.doRequest(http.MethodGet, apiBasePath+"/collections", nil, applyListOptions(opts))
	if err != nil {
		return nil, err
	}
	return decode[api.CollectionResourceList](body)
}

// GetCollection returns the collection with the given ID.
func (c *Client) GetCollection(id string) (*api.CollectionResource, error) {
	body, _, err := c.doRequest(http.MethodGet, apiBasePath+"/collections/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	return decode[api.CollectionResource](body)
}

// ─── Evaluation Jobs ──────────────────────────────────────────────────────────

// ListJobs returns all evaluation jobs. Use WithLimit/WithOffset for pagination.
func (c *Client) ListJobs(opts ...ListOption) (*api.EvaluationJobResourceList, error) {
	body, _, err := c.doRequest(http.MethodGet, apiBasePath+"/jobs", nil, applyListOptions(opts))
	if err != nil {
		return nil, err
	}
	return decode[api.EvaluationJobResourceList](body)
}

// GetJob returns the evaluation job with the given ID.
func (c *Client) GetJob(id string) (*api.EvaluationJobResource, error) {
	body, _, err := c.doRequest(http.MethodGet, apiBasePath+"/jobs/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	return decode[api.EvaluationJobResource](body)
}

// ListJobsByStatus returns all evaluation jobs in the given state.
func (c *Client) ListJobsByStatus(status api.OverallState, opts ...ListOption) (*api.EvaluationJobResourceList, error) {
	opts = append(opts, withRawParam("status", string(status)))
	return c.ListJobs(opts...)
}

// CreateJob submits a new evaluation job and returns the created resource.
func (c *Client) CreateJob(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
	body, _, err := c.doRequest(http.MethodPost, apiBasePath+"/jobs", config, nil)
	if err != nil {
		return nil, err
	}
	return decode[api.EvaluationJobResource](body)
}

// CancelJob cancels the evaluation job with the given ID.
func (c *Client) CancelJob(id string) error {
	_, _, err := c.doRequest(http.MethodDelete, apiBasePath+"/jobs/"+id, nil, nil)
	return err
}

// ─── List options ─────────────────────────────────────────────────────────────

// ListOption configures query parameters for list endpoints.
type ListOption func(url.Values)

// WithLimit sets the maximum number of items to return.
func WithLimit(n int) ListOption {
	return func(v url.Values) { v.Set("limit", fmt.Sprintf("%d", n)) }
}

// WithOffset sets the result offset for pagination.
func WithOffset(n int) ListOption {
	return func(v url.Values) { v.Set("offset", fmt.Sprintf("%d", n)) }
}

// withRawParam is an unexported option for setting an arbitrary query parameter.
func withRawParam(key, value string) ListOption {
	return func(v url.Values) { v.Set(key, value) }
}

func applyListOptions(opts []ListOption) url.Values {
	params := url.Values{}
	for _, o := range opts {
		o(params)
	}
	return params
}
