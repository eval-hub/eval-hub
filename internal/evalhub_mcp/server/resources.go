package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EvalHubDataSource is the subset of evalhubclient.Client methods used by MCP
// resource handlers. Accepting an interface keeps handlers testable without a
// running eval-hub backend.
type EvalHubDataSource interface {
	ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error)
	GetProvider(id string) (*api.ProviderResource, error)
	ListBenchmarks() ([]api.BenchmarkResource, error)
	GetBenchmark(id string) (*api.BenchmarkResource, error)
	ListBenchmarksByLabel(labels []string) ([]api.BenchmarkResource, error)
}

func registerResources(srv *mcp.Server, ds EvalHubDataSource, logger *slog.Logger) {
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
}

func listProvidersHandler(ds EvalHubDataSource, logger *slog.Logger) mcp.ResourceHandler {
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

func getProviderHandler(ds EvalHubDataSource, logger *slog.Logger) mcp.ResourceHandler {
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

func listBenchmarksHandler(ds EvalHubDataSource, logger *slog.Logger) mcp.ResourceHandler {
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

func getBenchmarkHandler(ds EvalHubDataSource, logger *slog.Logger) mcp.ResourceHandler {
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
