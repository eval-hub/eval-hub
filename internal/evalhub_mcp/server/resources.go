package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EvalHubDiscovery is the subset of evalhubclient.Client methods used by MCP
// resource handlers. Accepting an interface keeps handlers testable without a
// running eval-hub backend.
type EvalHubDiscovery interface {
	ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error)
	GetProvider(id string) (*api.ProviderResource, error)
	ListBenchmarks() ([]api.BenchmarkResource, error)
	GetBenchmark(id string) (*api.BenchmarkResource, error)
	ListBenchmarksByLabel(labels []string) ([]api.BenchmarkResource, error)
	ListCollections(opts ...evalhubclient.ListOption) (*api.CollectionResourceList, error)
	GetCollection(id string) (*api.CollectionResource, error)
	ListJobs(opts ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error)
	GetJob(id string) (*api.EvaluationJobResource, error)
	ListJobsByStatus(status api.OverallState, opts ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error)
}

func registerResources(srv *mcp.Server, ds EvalHubDiscovery, logger *slog.Logger) {
	benchmarksHandler := listBenchmarksHandler(ds, logger)

	srv.AddResource(&mcp.Resource{
		Name:        "providers",
		Description: "List all evaluation providers",
		MIMEType:    "application/json",
		URI:         "evalhub://providers",
	}, listProvidersHandler(ds, logger))

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "provider",
		Description: "Get an evaluation provider by ID",
		MIMEType:    "application/json",
		URITemplate: "evalhub://providers/{id}",
	}, getProviderHandler(ds, logger))

	srv.AddResource(&mcp.Resource{
		Name:        "benchmarks",
		Description: "List all benchmarks across all providers",
		MIMEType:    "application/json",
		URI:         "evalhub://benchmarks",
	}, benchmarksHandler)

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "benchmarks-by-label",
		Description: "Filter benchmarks by label (e.g. rag, safety, agents)",
		MIMEType:    "application/json",
		URITemplate: "evalhub://benchmarks{?label*}",
	}, benchmarksHandler)

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "benchmark",
		Description: "Get a benchmark by ID",
		MIMEType:    "application/json",
		URITemplate: "evalhub://benchmarks/{id}",
	}, getBenchmarkHandler(ds, logger))

	srv.AddResource(&mcp.Resource{
		Name:        "collections",
		Description: "List all benchmark collections",
		MIMEType:    "application/json",
		URI:         "evalhub://collections",
	}, listCollectionsHandler(ds, logger))

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "collection",
		Description: "Get a benchmark collection by ID",
		MIMEType:    "application/json",
		URITemplate: "evalhub://collections/{id}",
	}, getCollectionHandler(ds, logger))

	jobsHandler := listJobsHandler(ds, logger)

	srv.AddResource(&mcp.Resource{
		Name:        "jobs",
		Description: "List all evaluation jobs",
		MIMEType:    "application/json",
		URI:         "evalhub://jobs",
	}, jobsHandler)

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "jobs-by-status",
		Description: "Filter evaluation jobs by status (pending, running, completed, failed, cancelled, partially_failed)",
		MIMEType:    "application/json",
		URITemplate: "evalhub://jobs{?status}",
	}, jobsHandler)

	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "job",
		Description: "Get an evaluation job by ID (includes full status detail and per-benchmark progress)",
		MIMEType:    "application/json",
		URITemplate: "evalhub://jobs/{id}",
	}, getJobHandler(ds, logger))
}

func listProvidersHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		logger.Debug("reading resource", "uri", req.Params.URI)
		list, err := ds.ListProviders()
		if err != nil {
			return nil, fmt.Errorf("listing providers: %w", err)
		}
		items := list.Items
		if items == nil {
			items = []api.ProviderResource{}
		}
		return jsonResult(items)
	}
}

func getProviderHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, err := extractPathID(req.Params.URI, "providers")
		if err != nil {
			return nil, err
		}
		logger.Debug("reading resource", "uri", req.Params.URI, "id", id)
		provider, err := ds.GetProvider(id)
		if err != nil {
			return nil, toMCPError(req.Params.URI, err)
		}
		return jsonResult(provider)
	}
}

func listBenchmarksHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		logger.Debug("reading resource", "uri", req.Params.URI)
		labels := extractLabels(req.Params.URI, logger)

		var benchmarks []api.BenchmarkResource
		var err error
		if len(labels) > 0 {
			benchmarks, err = ds.ListBenchmarksByLabel(labels)
		} else {
			benchmarks, err = ds.ListBenchmarks()
		}
		if err != nil {
			return nil, fmt.Errorf("listing benchmarks: %w", err)
		}
		if benchmarks == nil {
			benchmarks = []api.BenchmarkResource{}
		}
		return jsonResult(benchmarks)
	}
}

func getBenchmarkHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, err := extractPathID(req.Params.URI, "benchmarks")
		if err != nil {
			return nil, err
		}
		logger.Debug("reading resource", "uri", req.Params.URI, "id", id)
		benchmark, err := ds.GetBenchmark(id)
		if err != nil {
			return nil, toMCPError(req.Params.URI, err)
		}
		return jsonResult(benchmark)
	}
}

func listCollectionsHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		logger.Debug("reading resource", "uri", req.Params.URI)
		opts, err := extractPagination(req.Params.URI)
		if err != nil {
			return nil, err
		}
		list, err := ds.ListCollections(opts...)
		if err != nil {
			return nil, fmt.Errorf("listing collections: %w", err)
		}
		items := list.Items
		if items == nil {
			items = []api.CollectionResource{}
		}
		return jsonResult(items)
	}
}

func getCollectionHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, err := extractPathID(req.Params.URI, "collections")
		if err != nil {
			return nil, err
		}
		logger.Debug("reading resource", "uri", req.Params.URI, "id", id)
		collection, err := ds.GetCollection(id)
		if err != nil {
			return nil, toMCPError(req.Params.URI, err)
		}
		return jsonResult(collection)
	}
}

func listJobsHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		logger.Debug("reading resource", "uri", req.Params.URI)
		status, hasStatus, err := extractStatus(req.Params.URI)
		if err != nil {
			return nil, err
		}
		opts, pErr := extractPagination(req.Params.URI)
		if pErr != nil {
			return nil, pErr
		}

		var list *api.EvaluationJobResourceList
		if hasStatus {
			list, err = ds.ListJobsByStatus(status, opts...)
		} else {
			list, err = ds.ListJobs(opts...)
		}
		if err != nil {
			return nil, fmt.Errorf("listing jobs: %w", err)
		}
		items := list.Items
		if items == nil {
			items = []api.EvaluationJobResource{}
		}
		return jsonResult(items)
	}
}

func getJobHandler(ds EvalHubDiscovery, logger *slog.Logger) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, err := extractPathID(req.Params.URI, "jobs")
		if err != nil {
			return nil, err
		}
		logger.Debug("reading resource", "uri", req.Params.URI, "id", id)
		job, err := ds.GetJob(id)
		if err != nil {
			return nil, toMCPError(req.Params.URI, err)
		}
		return jsonResult(job)
	}
}

func extractPathID(rawURI, kind string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", mcp.ResourceNotFoundError(rawURI)
	}
	if u.Host != kind {
		return "", mcp.ResourceNotFoundError(rawURI)
	}
	id := strings.TrimPrefix(u.Path, "/")
	if id == "" {
		return "", mcp.ResourceNotFoundError(rawURI)
	}
	return id, nil
}

func extractLabels(rawURI string, logger *slog.Logger) []string {
	u, err := url.Parse(rawURI)
	if err != nil {
		logger.Error("failed to parse resource URI", "uri", rawURI, "error", err)
		return nil
	}
	return u.Query()["label"]
}

func extractStatus(rawURI string) (api.OverallState, bool, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", false, nil
	}
	s := u.Query().Get("status")
	if s == "" {
		return "", false, nil
	}
	state, err := api.GetOverallState(s)
	if err != nil {
		return "", true, fmt.Errorf("invalid job status %q: valid values are pending, running, completed, failed, cancelled, partially_failed", s)
	}
	return state, true, nil
}

func extractPagination(rawURI string) ([]evalhubclient.ListOption, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, nil
	}
	var opts []evalhubclient.ListOption
	if v := u.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid limit %q: must be a positive integer", v)
		}
		if n > 0 {
			opts = append(opts, evalhubclient.WithLimit(n))
		}
	}
	if v := u.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid offset %q: must be a non-negative integer", v)
		}
		if n >= 0 {
			opts = append(opts, evalhubclient.WithOffset(n))
		}
	}
	return opts, nil
}

func toMCPError(uri string, err error) error {
	var apiErr *evalhubclient.APIError
	if errors.As(err, &apiErr) && apiErr.IsNotFound() {
		return mcp.ResourceNotFoundError(uri)
	}
	return err
}

func jsonResult(v any) (*mcp.ReadResourceResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshalling response: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{Text: string(data)}},
	}, nil
}
