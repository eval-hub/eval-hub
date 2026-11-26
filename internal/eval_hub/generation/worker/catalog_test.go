package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestWorkerRunsGenerationThroughSidecar(t *testing.T) {
	useTestNamespace(t)
	ctx := context.Background()
	var mu sync.Mutex
	var events []api.StatusEvent
	var modelCalls int
	var reads []string
	system := api.ProviderResource{
		Resource: api.Resource{ID: "system-provider", Owner: "system"},
		ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{
			ID: "tool-check", Name: "Tool use", Domains: []string{"tool_use"}, Metrics: []string{"pass"},
		}}},
	}
	tenant := api.ProviderResource{
		Resource: api.Resource{ID: "tenant-provider", Owner: "different-owner", Tenant: "tenant-test"},
		ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{
			ID: "tool-use", Name: "Tool use", Domains: []string{"tool_use"}, Metrics: []string{"pass"},
		}}},
	}
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-User") != "" || r.Header.Get("Authorization") != "" {
			t.Error("the worker supplied an identity or token override")
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/evaluations/") && r.Header.Get("X-Tenant") != "tenant-test" {
			t.Errorf("catalog or callback is missing the pod namespace: %s", r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/evaluations/jobs/56ffacc2-deb2-4e10-bf9a-0b69ed6da7c0/events":
			var status api.StatusEvent
			if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
				t.Errorf("decode status: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			events = append(events, status)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/evaluations/providers":
			reads = append(reads, r.URL.Path)
			_ = json.NewEncoder(w).Encode(api.ProviderResourceList{
				Page: api.Page{TotalCount: 2}, Items: []api.ProviderResource{system, tenant},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/evaluations/providers/tenant-provider":
			reads = append(reads, r.URL.Path)
			_ = json.NewEncoder(w).Encode(tenant)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/evaluations/collections":
			reads = append(reads, r.URL.Path)
			_ = json.NewEncoder(w).Encode(api.CollectionResourceList{Page: api.Page{TotalCount: 0}})
		case r.Method == http.MethodPost && r.URL.Path == "/model/evaluation/v1/chat/completions":
			modelCalls++
			var request struct {
				Messages []loop.Message       `json:"messages"`
				Tools    loop.ToolDefinitions `json:"tools"`
				Model    string               `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode model request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(request.Tools) != 3 || request.Model != "generator" {
				t.Errorf("unexpected model request shape")
			}
			message := loop.Message{Role: loop.RoleAssistant}
			finish := "stop"
			if modelCalls == 1 {
				if len(request.Messages) != 3 || !strings.Contains(request.Messages[2].Content, "system-provider") ||
					!strings.Contains(request.Messages[2].Content, "tenant-provider") || strings.Contains(request.Messages[2].Content, "generator-auth") {
					t.Errorf("unexpected model context")
				}
				message.ToolCalls = []loop.ToolCall{{
					ID: "details", Type: "function", Function: loop.FunctionCall{
						Name: "get_provider_details", Arguments: `{"provider_id":"tenant-provider"}`,
					},
				}}
				finish = "tool_calls"
			} else {
				encoded, _ := json.Marshal(map[string]any{
					"collection": api.CollectionConfig{
						Name: "Tool use evaluation", Domains: []string{"tool_use"},
						Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "tool-use"}, ProviderID: "tenant-provider"}},
					},
					"rationale": strings.Repeat("The benchmark measures tool use. ", 155),
					"explanation": api.CollectionGenerationExplanation{SelectedBenchmarks: []api.CollectionGenerationBenchmarkReason{{
						ProviderID: "tenant-provider", ID: "tool-use", Reason: "Measures tool use.",
					}}},
				})
				message.Content = string(encoded)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": message, "finish_reason": finish}},
			})
		default:
			t.Errorf("unexpected sidecar request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer sidecar.Close()
	spec, _ := validWorkerSpec(t, sidecar.URL, sidecar.URL+"/model/evaluation/v1")
	guardrailsCalls := 0
	err := runSpec(ctx, spec, sidecar.Client(), func(_ context.Context, goal string) (string, error) {
		guardrailsCalls++
		if goal != "safe tool use" {
			t.Errorf("guardrail received wrong goal: %q", goal)
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if guardrailsCalls != 1 || modelCalls != 2 || len(reads) != 4 {
		t.Fatalf("unexpected call counts: safety=%d model=%d reads=%v", guardrailsCalls, modelCalls, reads)
	}
	if len(events) < 3 {
		t.Fatalf("only %d lifecycle events", len(events))
	}
	var previous *api.CollectionGenerationProgress
	for _, event := range events[1 : len(events)-1] {
		progress := event.BenchmarkStatusEvent.CollectionGenerationProgress
		if progress == nil || event.BenchmarkStatusEvent.AdditionalInfo != nil {
			t.Fatal("non-progress data sent before terminal event")
		}
		if err := generation.ValidateProgressEvent(*progress, previous); err != nil {
			t.Fatal(err)
		}
		previous = &api.CollectionGenerationProgress{Sequence: progress.Sequence,
			CollectionGenerationStatusProgress: api.CollectionGenerationStatusProgress{Phase: progress.Phase}}
	}
	last := events[len(events)-1].BenchmarkStatusEvent
	if last.Status != api.StateCompleted {
		t.Fatalf("expected terminal proposal, got %s: %+v", last.Status, last.AdditionalInfo)
	}
	result, err := generation.ResultFromAdditionalInfo(api.CollectionGenerationRequest{MaxBenchmarks: 12}, last.AdditionalInfo)
	if err != nil || len(result.Rationale) < 4096 || result.Collection.Benchmarks[0].ProviderID != "tenant-provider" {
		t.Fatalf("unexpected result or error: %.120q, %v", result.Rationale, err)
	}
}

func TestSidecarCatalogEnforcesReadScopeAndDoesNotFollowRedirects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resource  api.Resource
		redirect  bool
		wantError bool
	}{
		{"system provider", api.Resource{ID: "p", Owner: "system"}, false, false},
		{"another owner in tenant", api.Resource{ID: "p", Tenant: "tenant-a", Owner: "another-owner"}, false, false},
		{"cross tenant", api.Resource{ID: "p", Tenant: "tenant-b", Owner: "another-owner"}, false, true},
		{"redirect", api.Resource{}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/evaluations/providers/p" ||
					r.Header.Get("X-Tenant") != "tenant-a" || r.Header.Get("X-User") != "" {
					t.Errorf("invalid catalog request: %s %s", r.Method, r.URL)
				}
				if tc.redirect {
					res := response(http.StatusTemporaryRedirect)
					res.Header.Set("Location", "https://outside.invalid")
					return res, nil
				}
				res := response(http.StatusOK)
				res.Body = io.NopCloser(strings.NewReader(fmt.Sprintf(`{"resource":%s}`, mustJSON(t, tc.resource))))
				return res, nil
			})}
			base, _ := url.Parse("http://localhost:8080")
			source := sidecarCatalog{client: localClient(client), base: *base, tenant: "tenant-a"}
			_, err := source.GetProvider(context.Background(), "p")
			if (err != nil) != tc.wantError || calls != 1 {
				t.Fatalf("unexpected catalog result (%v) after %d calls", err, calls)
			}
			if _, err := source.GetProvider(context.Background(), "../jobs"); err == nil || calls != 1 {
				t.Fatalf("unsupported path reached the sidecar transport: %v", err)
			}
		})
	}
}

func TestSidecarCatalogRejectsInvalidListResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resource  string
		body      string
		transport error
		want      string
	}{
		{"cross-tenant provider", "providers", `{"items":[{"resource":{"id":"private-provider","tenant":"tenant-b"}}],"total_count":1}`, nil, "provider response is outside the catalog scope"},
		{"cross-tenant collection", "collections", `{"items":[{"resource":{"id":"private-collection","tenant":"tenant-b"}}],"total_count":1}`, nil, "collection response is outside the catalog scope"},
		{"malformed provider response", "providers", "private upstream JSON", nil, "catalog response is malformed or exceeds the size limit"},
		{"oversized collection response", "collections", strings.Repeat(" ", maxCatalogReply+1), nil, "catalog response is malformed or exceeds the size limit"},
		{"sidecar transport failure", "collections", "", errors.New("private transport detail"), "send catalog request through the local sidecar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := url.Parse("http://localhost:8080")
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != catalogPath+tc.resource ||
					r.URL.RawQuery != "limit=5&offset=10" || r.Header.Get("X-Tenant") != "tenant-a" {
					t.Fatalf("unexpected catalog request: %s %s", r.Method, r.URL)
				}
				if tc.transport != nil {
					return nil, tc.transport
				}
				res := response(http.StatusOK)
				res.Body = io.NopCloser(strings.NewReader(tc.body))
				return res, nil
			})}
			source := sidecarCatalog{client: localClient(client), base: *base, tenant: "tenant-a"}
			if tc.resource == "providers" {
				page, err := source.ListProviders(context.Background(), 10, 5)
				if len(page.Items) != 0 || err == nil || err.Error() != tc.want {
					t.Fatalf("ListProviders() = %+v, %v, want no items and %q", page, err, tc.want)
				}
			} else {
				page, err := source.ListCollections(context.Background(), 10, 5)
				if len(page.Items) != 0 || err == nil || err.Error() != tc.want {
					t.Fatalf("ListCollections() = %+v, %v, want no items and %q", page, err, tc.want)
				}
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
