package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- mock data source ---

type mockDataSource struct {
	providers  []api.ProviderResource
	benchmarks []api.BenchmarkResource
}

func (m *mockDataSource) ListProviders(_ ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
	return &api.ProviderResourceList{
		Items: m.providers,
		Page:  api.Page{TotalCount: len(m.providers)},
	}, nil
}

func (m *mockDataSource) GetProvider(id string) (*api.ProviderResource, error) {
	for i := range m.providers {
		if m.providers[i].Resource.ID == id {
			return &m.providers[i], nil
		}
	}
	return nil, &evalhubclient.APIError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("provider %q not found", id),
	}
}

func (m *mockDataSource) ListBenchmarks() ([]api.BenchmarkResource, error) {
	return m.benchmarks, nil
}

func (m *mockDataSource) GetBenchmark(id string) (*api.BenchmarkResource, error) {
	for i := range m.benchmarks {
		if m.benchmarks[i].ID == id {
			return &m.benchmarks[i], nil
		}
	}
	return nil, &evalhubclient.APIError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("benchmark %q not found", id),
	}
}

func (m *mockDataSource) ListBenchmarksByLabel(labels []string) ([]api.BenchmarkResource, error) {
	var result []api.BenchmarkResource
	for _, b := range m.benchmarks {
		if hasAllLabels(b.Tags, labels) {
			result = append(result, b)
		}
	}
	return result, nil
}

func hasAllLabels(tags, labels []string) bool {
	tagSet := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		tagSet[t] = struct{}{}
	}
	for _, l := range labels {
		if _, ok := tagSet[l]; !ok {
			return false
		}
	}
	return true
}

// --- test fixtures ---

func testDataSource() *mockDataSource {
	return &mockDataSource{
		providers: []api.ProviderResource{
			{
				Resource:       api.Resource{ID: "lighteval"},
				ProviderConfig: api.ProviderConfig{Name: "lighteval", Title: "LightEval", Description: "Lightweight evaluation framework"},
			},
			{
				Resource:       api.Resource{ID: "unitxt"},
				ProviderConfig: api.ProviderConfig{Name: "unitxt", Title: "Unitxt", Description: "Flexible text evaluation"},
			},
		},
		benchmarks: []api.BenchmarkResource{
			{ID: "hellaswag", Name: "HellaSwag", Category: "reasoning", Tags: []string{"reasoning", "general"}},
			{ID: "mmlu", Name: "MMLU", Category: "knowledge", Tags: []string{"knowledge", "general"}},
			{ID: "rag_eval", Name: "RAG Evaluation", Category: "rag", Tags: []string{"rag", "safety"}},
			{ID: "toxigen", Name: "ToxiGen", Category: "safety", Tags: []string{"safety"}},
		},
	}
}

func emptyDataSource() *mockDataSource {
	return &mockDataSource{}
}

// --- test helpers ---

func connectWithResources(t *testing.T, ds EvalHubDiscovery) (context.Context, *mcp.ClientSession) {
	t.Helper()

	srv := New(&ServerInfo{Version: "test"}, discardLogger)
	registerResources(srv, ds, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return ctx, clientSession
}

func readResourceJSON[T any](t *testing.T, ctx context.Context, cs *mcp.ClientSession, uri string) T {
	t.Helper()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%q) failed: %v", uri, err)
	}
	if len(result.Contents) == 0 {
		t.Fatalf("ReadResource(%q): no contents returned", uri)
	}
	var v T
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &v); err != nil {
		t.Fatalf("ReadResource(%q): unmarshal failed: %v\nbody: %s", uri, err, result.Contents[0].Text)
	}
	return v
}

// --- resources/list ---

func TestResourcesListIncludesProvidersAndBenchmarks(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	result, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}

	want := map[string]bool{"evalhub://providers": false, "evalhub://benchmarks": false}
	for _, r := range result.Resources {
		if _, ok := want[r.URI]; ok {
			want[r.URI] = true
		}
	}
	for uri, found := range want {
		if !found {
			t.Errorf("resources/list missing %s", uri)
		}
	}
}

func TestResourceTemplatesListIncludesExpected(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	result, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates failed: %v", err)
	}

	wantTemplates := map[string]bool{
		"evalhub://providers/{id}":      false,
		"evalhub://benchmarks/{id}":     false,
		"evalhub://benchmarks{?label*}": false,
	}
	for _, rt := range result.ResourceTemplates {
		if _, ok := wantTemplates[rt.URITemplate]; ok {
			wantTemplates[rt.URITemplate] = true
		}
	}
	for tmpl, found := range wantTemplates {
		if !found {
			t.Errorf("resources/templates/list missing %s", tmpl)
		}
	}
}

// --- providers ---

func TestListProviders(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	providers := readResourceJSON[[]api.ProviderResource](t, ctx, cs, "evalhub://providers")
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
	if providers[0].Resource.ID != "lighteval" {
		t.Errorf("first provider ID = %q, want %q", providers[0].Resource.ID, "lighteval")
	}
	if providers[1].Resource.ID != "unitxt" {
		t.Errorf("second provider ID = %q, want %q", providers[1].Resource.ID, "unitxt")
	}
}

func TestGetProviderByID(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	provider := readResourceJSON[api.ProviderResource](t, ctx, cs, "evalhub://providers/lighteval")
	if provider.Resource.ID != "lighteval" {
		t.Errorf("provider ID = %q, want %q", provider.Resource.ID, "lighteval")
	}
	if provider.Name != "lighteval" {
		t.Errorf("provider name = %q, want %q", provider.Name, "lighteval")
	}
}

func TestGetProviderNotFound(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	_, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://providers/nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent provider")
	}
}

// --- benchmarks ---

func TestListBenchmarks(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	benchmarks := readResourceJSON[[]api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks")
	if len(benchmarks) != 4 {
		t.Fatalf("expected 4 benchmarks, got %d", len(benchmarks))
	}
}

func TestGetBenchmarkByID(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	benchmark := readResourceJSON[api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks/hellaswag")
	if benchmark.ID != "hellaswag" {
		t.Errorf("benchmark ID = %q, want %q", benchmark.ID, "hellaswag")
	}
	if benchmark.Name != "HellaSwag" {
		t.Errorf("benchmark name = %q, want %q", benchmark.Name, "HellaSwag")
	}
}

func TestGetBenchmarkNotFound(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	_, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://benchmarks/nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent benchmark")
	}
}

// --- label filtering ---

func TestListBenchmarksSingleLabel(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	benchmarks := readResourceJSON[[]api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks?label=rag")
	if len(benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark with label 'rag', got %d", len(benchmarks))
	}
	if benchmarks[0].ID != "rag_eval" {
		t.Errorf("benchmark ID = %q, want %q", benchmarks[0].ID, "rag_eval")
	}
}

func TestListBenchmarksMultipleLabels(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	benchmarks := readResourceJSON[[]api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks?label=rag&label=safety")
	if len(benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark with labels 'rag' AND 'safety', got %d", len(benchmarks))
	}
	if benchmarks[0].ID != "rag_eval" {
		t.Errorf("benchmark ID = %q, want %q", benchmarks[0].ID, "rag_eval")
	}
}

func TestListBenchmarksNonExistentLabel(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	benchmarks := readResourceJSON[[]api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks?label=nonexistent")
	if len(benchmarks) != 0 {
		t.Errorf("expected 0 benchmarks for non-existent label, got %d", len(benchmarks))
	}
}

func TestListBenchmarksSafetyLabel(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	benchmarks := readResourceJSON[[]api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks?label=safety")
	if len(benchmarks) != 2 {
		t.Fatalf("expected 2 benchmarks with label 'safety', got %d", len(benchmarks))
	}
	ids := map[string]bool{}
	for _, b := range benchmarks {
		ids[b.ID] = true
	}
	if !ids["rag_eval"] || !ids["toxigen"] {
		t.Errorf("expected rag_eval and toxigen, got %v", ids)
	}
}

// --- empty results ---

func TestListProvidersEmpty(t *testing.T) {
	ctx, cs := connectWithResources(t, emptyDataSource())

	providers := readResourceJSON[[]api.ProviderResource](t, ctx, cs, "evalhub://providers")
	if providers == nil {
		t.Fatal("expected empty array, got nil")
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(providers))
	}
}

func TestListBenchmarksEmpty(t *testing.T) {
	ctx, cs := connectWithResources(t, emptyDataSource())

	benchmarks := readResourceJSON[[]api.BenchmarkResource](t, ctx, cs, "evalhub://benchmarks")
	if benchmarks == nil {
		t.Fatal("expected empty array, got nil")
	}
	if len(benchmarks) != 0 {
		t.Errorf("expected 0 benchmarks, got %d", len(benchmarks))
	}
}

// --- MIME type ---

func TestResourceContentType(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://providers"})
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("no contents returned")
	}
	if result.Contents[0].MIMEType != "application/json" {
		t.Errorf("MIME type = %q, want %q", result.Contents[0].MIMEType, "application/json")
	}
}

// --- URI edge cases ---

func TestReadResourceInvalidURI(t *testing.T) {
	ctx, cs := connectWithResources(t, testDataSource())

	_, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://unknown/resource"})
	if err == nil {
		t.Fatal("expected error for unknown resource URI")
	}
}

// --- RegisterHandlers nil client ---

func TestRegisterHandlersNilClient(t *testing.T) {
	srv := New(&ServerInfo{Version: "test"}, discardLogger)
	RegisterHandlers(srv, nil, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("expected 0 resources with nil client, got %d", len(result.Resources))
	}
}
