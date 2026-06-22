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

// --- mock tool client ---

type mockToolClient struct {
	createJobFn    func(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error)
	cancelJobFn    func(id string) error
	getJobFn       func(id string) (*api.EvaluationJobResource, error)
	listProviderFn func(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error)
	getProviderFn  func(id string) (*api.ProviderResource, error)
	getBenchmarkFn func(id string) (*api.BenchmarkResource, error)
}

func (m *mockToolClient) CreateJob(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
	if m.createJobFn != nil {
		return m.createJobFn(config)
	}
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-new", CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending},
		},
		EvaluationJobConfig: config,
	}, nil
}

func (m *mockToolClient) CancelJob(id string) error {
	if m.cancelJobFn != nil {
		return m.cancelJobFn(id)
	}
	return nil
}

func (m *mockToolClient) GetJob(id string) (*api.EvaluationJobResource, error) {
	if m.getJobFn != nil {
		return m.getJobFn(id)
	}
	return nil, &evalhubclient.APIError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("job %q not found", id),
	}
}

func (m *mockToolClient) ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
	if m.listProviderFn != nil {
		return m.listProviderFn(opts...)
	}
	return &api.ProviderResourceList{Items: []api.ProviderResource{}}, nil
}

func (m *mockToolClient) GetProvider(id string) (*api.ProviderResource, error) {
	if m.getProviderFn != nil {
		return m.getProviderFn(id)
	}
	return nil, &evalhubclient.APIError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("provider %q not found", id),
	}
}

func (m *mockToolClient) GetBenchmark(id string) (*api.BenchmarkResource, error) {
	if m.getBenchmarkFn != nil {
		return m.getBenchmarkFn(id)
	}
	return nil, &evalhubclient.APIError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("benchmark %q not found", id),
	}
}

// --- test helpers ---

func connectWithTools(t *testing.T, client EvalHubToolClient) (context.Context, *mcp.ClientSession) {
	t.Helper()

	srv := New(&ServerInfo{Build: "test"}, discardLogger, nil)
	registerTools(srv, client, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	return ctx, connectClient(t, ctx, srv)
}

func callToolJSON[T any](t *testing.T, ctx context.Context, cs *mcp.ClientSession, name string, args any) T {
	t.Helper()
	argsBytes, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: json.RawMessage(argsBytes),
	})
	if err != nil {
		t.Fatalf("CallTool(%s) failed: %v", name, err)
	}
	if result.StructuredContent == nil {
		t.Fatalf("CallTool(%s): no structured content returned", name)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("CallTool(%s): marshal structured content: %v", name, err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("CallTool(%s): unmarshal structured content: %v\nbody: %s", name, err, data)
	}
	return v
}

func callToolExpectError(t *testing.T, ctx context.Context, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	argsBytes, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: json.RawMessage(argsBytes),
	})
	if err != nil {
		t.Fatalf("CallTool(%s) failed at protocol level: %v", name, err)
	}
	if !result.IsError {
		t.Fatalf("CallTool(%s): expected IsError=true", name)
	}
	if len(result.Content) == 0 {
		t.Fatalf("CallTool(%s): no content in error result", name)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): expected TextContent, got %T", name, result.Content[0])
	}
	return tc.Text
}

// --- tools/list ---

func TestToolsListIncludesAll(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	want := map[string]bool{
		"submit_evaluation":  false,
		"cancel_job":         false,
		"get_job_status":     false,
		"discover_providers": false,
	}
	for _, tool := range result.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tools/list missing %s", name)
		}
	}
}

func TestToolSchemasHaveDescriptions(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema", tool.Name)
			continue
		}
		schemaBytes, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("tool %q: failed to marshal InputSchema: %v", tool.Name, err)
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(schemaBytes, &schema); err != nil {
			t.Errorf("tool %q: InputSchema is not valid JSON: %v", tool.Name, err)
		}
	}
}

// --- submit_evaluation ---

func TestSubmitEvaluationWithBenchmarks(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	out := callToolJSON[SubmitEvaluationOutput](t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "test-eval",
		"model": map[string]any{
			"url":  "http://model:8080",
			"name": "test-model",
		},
		"benchmarks": []map[string]any{
			{"id": "mmlu", "provider_id": "unitxt"},
		},
	})

	if out.JobID != "job-new" {
		t.Errorf("job_id = %q, want %q", out.JobID, "job-new")
	}
	if out.State != "pending" {
		t.Errorf("state = %q, want %q", out.State, "pending")
	}
}

func TestSubmitEvaluationWithCollection(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	out := callToolJSON[SubmitEvaluationOutput](t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "collection-eval",
		"model": map[string]any{
			"url":  "http://model:8080",
			"name": "test-model",
		},
		"collection": map[string]any{
			"id": "safety-suite",
		},
	})

	if out.JobID != "job-new" {
		t.Errorf("job_id = %q, want %q", out.JobID, "job-new")
	}
	if out.State != "pending" {
		t.Errorf("state = %q, want %q", out.State, "pending")
	}
}

func TestSubmitEvaluationMissingBenchmarksAndCollection(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	msg := callToolExpectError(t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "missing-eval",
		"model": map[string]any{
			"url":  "http://model:8080",
			"name": "test-model",
		},
	})

	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestSubmitEvaluationBothBenchmarksAndCollection(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	msg := callToolExpectError(t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "both-eval",
		"model": map[string]any{
			"url":  "http://model:8080",
			"name": "test-model",
		},
		"benchmarks": []map[string]any{
			{"id": "mmlu", "provider_id": "unitxt"},
		},
		"collection": map[string]any{
			"id": "safety-suite",
		},
	})

	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestSubmitEvaluationParameters(t *testing.T) {
	t.Parallel()

	var captured api.EvaluationJobConfig
	client := &mockToolClient{
		createJobFn: func(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
			captured = config
			return &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{ID: "job-params"},
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending},
				},
				EvaluationJobConfig: config,
			}, nil
		},
	}

	ctx, cs := connectWithTools(t, client)

	callToolJSON[SubmitEvaluationOutput](t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "params-eval",
		"model": map[string]any{
			"url":  "http://model:8080",
			"name": "test-model",
			"parameters": map[string]any{
				"temperature": 0.7,
				"max_tokens":  1024,
			},
		},
		"benchmarks": []map[string]any{
			{
				"id":          "mmlu",
				"provider_id": "unitxt",
				"parameters": map[string]any{
					"num_fewshot": 5,
				},
			},
		},
	})

	if captured.Model.Parameters == nil {
		t.Fatal("expected model parameters to be passed through")
	}
	if captured.Model.Parameters["temperature"] != 0.7 {
		t.Errorf("model temperature = %v, want 0.7", captured.Model.Parameters["temperature"])
	}
	if captured.Model.Parameters["max_tokens"] != float64(1024) {
		t.Errorf("model max_tokens = %v, want 1024", captured.Model.Parameters["max_tokens"])
	}
	if len(captured.Benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(captured.Benchmarks))
	}
	if captured.Benchmarks[0].Parameters == nil {
		t.Fatal("expected benchmark parameters to be passed through")
	}
	if captured.Benchmarks[0].Parameters["num_fewshot"] != float64(5) {
		t.Errorf("benchmark num_fewshot = %v, want 5", captured.Benchmarks[0].Parameters["num_fewshot"])
	}
}

func TestSubmitEvaluationExperimentConfig(t *testing.T) {
	t.Parallel()

	var captured api.EvaluationJobConfig
	client := &mockToolClient{
		createJobFn: func(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
			captured = config
			return &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{ID: "job-exp"},
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending},
				},
				EvaluationJobConfig: config,
			}, nil
		},
	}

	ctx, cs := connectWithTools(t, client)

	callToolJSON[SubmitEvaluationOutput](t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "exp-eval",
		"model": map[string]any{
			"url":  "http://model:8080",
			"name": "test-model",
		},
		"benchmarks": []map[string]any{
			{"id": "mmlu", "provider_id": "unitxt"},
		},
		"experiment": map[string]any{
			"name":              "my-experiment",
			"tags":              map[string]string{"team": "ml"},
			"artifact_location": "s3://bucket/artifacts",
		},
	})

	if captured.Experiment == nil {
		t.Fatal("expected experiment config to be passed through")
	}
	if captured.Experiment.Name != "my-experiment" {
		t.Errorf("experiment name = %q, want %q", captured.Experiment.Name, "my-experiment")
	}
	if captured.Experiment.ArtifactLocation != "s3://bucket/artifacts" {
		t.Errorf("artifact_location = %q, want %q", captured.Experiment.ArtifactLocation, "s3://bucket/artifacts")
	}
	if len(captured.Experiment.Tags) != 1 {
		t.Fatalf("expected 1 experiment tag, got %d", len(captured.Experiment.Tags))
	}
}

func TestSubmitEvaluationAPIError(t *testing.T) {
	t.Parallel()

	client := &mockToolClient{
		createJobFn: func(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
			return nil, &evalhubclient.APIError{
				StatusCode: http.StatusBadRequest,
				Message:    "invalid model URL",
			}
		},
	}

	ctx, cs := connectWithTools(t, client)

	msg := callToolExpectError(t, ctx, cs, "submit_evaluation", map[string]any{
		"name": "bad-eval",
		"model": map[string]any{
			"url":  "not-a-url",
			"name": "test-model",
		},
		"benchmarks": []map[string]any{
			{"id": "mmlu", "provider_id": "unitxt"},
		},
	})

	if msg == "" {
		t.Error("expected non-empty error message for API error")
	}
}

// --- cancel_job ---

func TestCancelJobSuccess(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	out := callToolJSON[CancelJobOutput](t, ctx, cs, "cancel_job", map[string]any{
		"job_id": "job-1",
	})

	if out.JobID != "job-1" {
		t.Errorf("job_id = %q, want %q", out.JobID, "job-1")
	}
	if out.Message == "" {
		t.Error("expected non-empty confirmation message")
	}
}

func TestCancelJobNotFound(t *testing.T) {
	t.Parallel()

	client := &mockToolClient{
		cancelJobFn: func(id string) error {
			return &evalhubclient.APIError{
				StatusCode: http.StatusNotFound,
				Message:    fmt.Sprintf("job %q not found", id),
			}
		},
	}

	ctx, cs := connectWithTools(t, client)

	msg := callToolExpectError(t, ctx, cs, "cancel_job", map[string]any{
		"job_id": "nonexistent",
	})

	if msg == "" {
		t.Error("expected non-empty error message for missing job")
	}
}

func TestCancelJobAlreadyCompleted(t *testing.T) {
	t.Parallel()

	client := &mockToolClient{
		cancelJobFn: func(id string) error {
			return &evalhubclient.APIError{
				StatusCode: http.StatusConflict,
				Message:    "job already completed",
			}
		},
	}

	ctx, cs := connectWithTools(t, client)

	msg := callToolExpectError(t, ctx, cs, "cancel_job", map[string]any{
		"job_id": "job-completed",
	})

	if msg == "" {
		t.Error("expected non-empty error message for completed job")
	}
}

func TestCancelJobEmptyID(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	msg := callToolExpectError(t, ctx, cs, "cancel_job", map[string]any{
		"job_id": "",
	})

	if msg == "" {
		t.Error("expected non-empty error message for empty job_id")
	}
}

// --- get_job_status ---

func TestGetJobStatusRunning(t *testing.T) {
	t.Parallel()

	client := &mockToolClient{
		getJobFn: func(id string) (*api.EvaluationJobResource, error) {
			return &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{
						ID:        id,
						CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
					},
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
					Benchmarks: []api.BenchmarkStatus{
						{
							ID:          "hellaswag",
							ProviderID:  "lighteval",
							Status:      api.StateCompleted,
							StartedAt:   "2026-05-01T10:01:00Z",
							CompletedAt: "2026-05-01T10:05:00Z",
						},
						{
							ID:         "mmlu",
							ProviderID: "unitxt",
							Status:     api.StateRunning,
							StartedAt:  "2026-05-01T10:01:00Z",
						},
					},
				},
			}, nil
		},
	}

	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[GetJobStatusOutput](t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "job-1",
	})

	if out.JobID != "job-1" {
		t.Errorf("job_id = %q, want %q", out.JobID, "job-1")
	}
	if out.State != "running" {
		t.Errorf("state = %q, want %q", out.State, "running")
	}
	if out.Progress != 50 {
		t.Errorf("progress = %d, want 50", out.Progress)
	}
	if len(out.Benchmarks) != 2 {
		t.Fatalf("expected 2 benchmark statuses, got %d", len(out.Benchmarks))
	}
	if out.Benchmarks[0].Status != "completed" {
		t.Errorf("first benchmark status = %q, want %q", out.Benchmarks[0].Status, "completed")
	}
	if out.Benchmarks[1].Status != "running" {
		t.Errorf("second benchmark status = %q, want %q", out.Benchmarks[1].Status, "running")
	}
}

func TestGetJobStatusCompleted(t *testing.T) {
	t.Parallel()

	client := &mockToolClient{
		getJobFn: func(id string) (*api.EvaluationJobResource, error) {
			return &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{
						ID:        id,
						CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
					},
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
					Benchmarks: []api.BenchmarkStatus{
						{
							ID:          "mmlu",
							ProviderID:  "unitxt",
							Status:      api.StateCompleted,
							StartedAt:   "2026-05-01T10:01:00Z",
							CompletedAt: "2026-05-01T10:10:00Z",
						},
					},
				},
				Results: &api.EvaluationJobResults{
					Benchmarks: []api.BenchmarkResult{
						{ID: "mmlu", ProviderID: "unitxt", Metrics: map[string]any{"accuracy": 0.85}},
					},
				},
			}, nil
		},
	}

	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[GetJobStatusOutput](t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "job-done",
	})

	if out.State != "completed" {
		t.Errorf("state = %q, want %q", out.State, "completed")
	}
	if out.Progress != 100 {
		t.Errorf("progress = %d, want 100", out.Progress)
	}
}

func TestGetJobStatusNotFound(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	msg := callToolExpectError(t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "nonexistent",
	})

	if msg == "" {
		t.Error("expected non-empty error message for missing job")
	}
}

func TestGetJobStatusEmptyID(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithTools(t, &mockToolClient{})

	msg := callToolExpectError(t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "",
	})

	if msg == "" {
		t.Error("expected non-empty error message for empty job_id")
	}
}

// --- discover_providers ---

func testProviders() []api.ProviderResource {
	return []api.ProviderResource{
		{
			Resource:       api.Resource{ID: "garak"},
			ProviderConfig: api.ProviderConfig{Name: "garak", Title: "Garak Safety"},
		},
		{
			Resource: api.Resource{ID: "agentdojo"},
			ProviderConfig: api.ProviderConfig{
				Name:  "agentdojo",
				Title: "AgentDojo",
				Agent: &api.AgentMetadata{
					TargetType:           "agent",
					Evaluates:            []string{"safety", "prompt-injection"},
					Summary:              "Test agent resilience to prompt injection",
					Hints:                []string{"Requires tool-calling model"},
					ResultInterpretation: []string{"High utility + low security = dangerous"},
					Complements:          []string{"garak"},
					RecommendedWhen:      []string{"Evaluating agentic systems"},
				},
			},
		},
		{
			Resource: api.Resource{ID: "lmeval"},
			ProviderConfig: api.ProviderConfig{
				Name:  "lm_evaluation_harness",
				Title: "LM Evaluation Harness",
				Agent: &api.AgentMetadata{
					TargetType: "model",
					Evaluates:  []string{"knowledge", "reasoning"},
					Summary:    "Standard academic benchmarks",
				},
			},
		},
	}
}

func mockWithProviders(providers []api.ProviderResource) *mockToolClient {
	providerMap := make(map[string]*api.ProviderResource, len(providers))
	for i := range providers {
		providerMap[providers[i].Resource.ID] = &providers[i]
	}
	return &mockToolClient{
		listProviderFn: func(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
			return &api.ProviderResourceList{Items: providers}, nil
		},
		getProviderFn: func(id string) (*api.ProviderResource, error) {
			if p, ok := providerMap[id]; ok {
				return p, nil
			}
			return nil, &evalhubclient.APIError{StatusCode: http.StatusNotFound, Message: fmt.Sprintf("provider %q not found", id)}
		},
	}
}

func TestDiscoverProvidersNoFilter(t *testing.T) {
	t.Parallel()
	client := mockWithProviders(testProviders())
	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[DiscoverProvidersOutput](t, ctx, cs, "discover_providers", map[string]any{})

	if len(out.Providers) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(out.Providers))
	}
	var garakFound bool
	for _, p := range out.Providers {
		if p.ID == "garak" {
			garakFound = true
			if p.Summary != "" {
				t.Errorf("garak should have empty summary (no agent block), got %q", p.Summary)
			}
		}
	}
	if !garakFound {
		t.Error("garak (no agent block) should be included in unfiltered results")
	}
}

func TestDiscoverProvidersFilterByTargetType(t *testing.T) {
	t.Parallel()
	client := mockWithProviders(testProviders())
	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[DiscoverProvidersOutput](t, ctx, cs, "discover_providers", map[string]any{
		"target_type": "agent",
	})

	if len(out.Providers) != 1 {
		t.Fatalf("expected 1 provider for target_type=agent, got %d", len(out.Providers))
	}
	if out.Providers[0].ID != "agentdojo" {
		t.Errorf("expected agentdojo, got %q", out.Providers[0].ID)
	}
	if out.Providers[0].Summary != "Test agent resilience to prompt injection" {
		t.Errorf("summary not populated: %q", out.Providers[0].Summary)
	}
}

func TestDiscoverProvidersFilterByEvaluates(t *testing.T) {
	t.Parallel()
	client := mockWithProviders(testProviders())
	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[DiscoverProvidersOutput](t, ctx, cs, "discover_providers", map[string]any{
		"evaluates": []string{"safety"},
	})

	if len(out.Providers) != 1 {
		t.Fatalf("expected 1 provider for evaluates=[safety], got %d", len(out.Providers))
	}
	if out.Providers[0].ID != "agentdojo" {
		t.Errorf("expected agentdojo, got %q", out.Providers[0].ID)
	}
}

func TestDiscoverProvidersFilterAllNilAgent(t *testing.T) {
	t.Parallel()
	client := mockWithProviders([]api.ProviderResource{
		{
			Resource:       api.Resource{ID: "bare"},
			ProviderConfig: api.ProviderConfig{Name: "bare", Title: "Bare Provider"},
		},
	})
	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[DiscoverProvidersOutput](t, ctx, cs, "discover_providers", map[string]any{
		"target_type": "model",
	})

	if len(out.Providers) != 0 {
		t.Errorf("expected 0 providers when all have nil agent, got %d", len(out.Providers))
	}
}

func TestDiscoverProvidersMixedAgentBlocks(t *testing.T) {
	t.Parallel()
	client := mockWithProviders(testProviders())
	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[DiscoverProvidersOutput](t, ctx, cs, "discover_providers", map[string]any{
		"target_type": "model",
	})

	if len(out.Providers) != 1 {
		t.Fatalf("expected 1 provider for target_type=model, got %d", len(out.Providers))
	}
	if out.Providers[0].ID != "lmeval" {
		t.Errorf("expected lmeval, got %q", out.Providers[0].ID)
	}
}

// --- get_job_status enrichment ---

func TestGetJobStatusEnrichedWithAgentMetadata(t *testing.T) {
	t.Parallel()

	client := mockWithProviders([]api.ProviderResource{
		{
			Resource: api.Resource{ID: "test-provider"},
			ProviderConfig: api.ProviderConfig{
				Name:  "test-provider",
				Title: "Test Provider",
				Agent: &api.AgentMetadata{
					Complements: []string{"other-provider"},
				},
				Benchmarks: []api.BenchmarkResource{
					{
						ID:   "bench-1",
						Name: "Benchmark One",
						Agent: &api.BenchmarkAgentMetadata{
							ResultInterpretation: "Higher is better",
						},
					},
				},
			},
		},
	})
	client.getJobFn = func(id string) (*api.EvaluationJobResource, error) {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: id, CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:          "bench-1",
						ProviderID:  "test-provider",
						Status:      api.StateCompleted,
						StartedAt:   "2026-05-01T10:01:00Z",
						CompletedAt: "2026-05-01T10:05:00Z",
					},
				},
			},
		}, nil
	}

	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[GetJobStatusOutput](t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "job-enriched",
	})

	if len(out.Benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(out.Benchmarks))
	}
	b := out.Benchmarks[0]
	if b.ResultInterpretation != "Higher is better" {
		t.Errorf("result_interpretation = %q, want %q", b.ResultInterpretation, "Higher is better")
	}
	if len(b.Complements) != 1 || b.Complements[0] != "other-provider" {
		t.Errorf("complements = %v, want [other-provider]", b.Complements)
	}
}

func TestGetJobStatusNoAgentMetadata(t *testing.T) {
	t.Parallel()

	client := mockWithProviders([]api.ProviderResource{
		{
			Resource: api.Resource{ID: "bare-provider"},
			ProviderConfig: api.ProviderConfig{
				Name:  "bare-provider",
				Title: "Bare Provider",
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-bare", Name: "Bare Benchmark"},
				},
			},
		},
	})
	client.getJobFn = func(id string) (*api.EvaluationJobResource, error) {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: id, CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:          "bench-bare",
						ProviderID:  "bare-provider",
						Status:      api.StateCompleted,
						StartedAt:   "2026-05-01T10:01:00Z",
						CompletedAt: "2026-05-01T10:05:00Z",
					},
				},
			},
		}, nil
	}

	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[GetJobStatusOutput](t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "job-bare",
	})

	if len(out.Benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(out.Benchmarks))
	}
	b := out.Benchmarks[0]
	if b.ResultInterpretation != "" {
		t.Errorf("result_interpretation should be empty, got %q", b.ResultInterpretation)
	}
	if len(b.Complements) != 0 {
		t.Errorf("complements should be empty, got %v", b.Complements)
	}
}

func TestGetJobStatusRunningNoEnrichment(t *testing.T) {
	t.Parallel()

	client := mockWithProviders([]api.ProviderResource{
		{
			Resource: api.Resource{ID: "some-provider"},
			ProviderConfig: api.ProviderConfig{
				Name:  "some-provider",
				Title: "Some Provider",
				Agent: &api.AgentMetadata{Complements: []string{"should-not-appear"}},
				Benchmarks: []api.BenchmarkResource{
					{
						ID:   "bench-running",
						Name: "Running Benchmark",
						Agent: &api.BenchmarkAgentMetadata{
							ResultInterpretation: "should-not-appear",
						},
					},
				},
			},
		},
	})
	client.getJobFn = func(id string) (*api.EvaluationJobResource, error) {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: id, CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:         "bench-running",
						ProviderID: "some-provider",
						Status:     api.StateRunning,
						StartedAt:  "2026-05-01T10:01:00Z",
					},
				},
			},
		}, nil
	}

	ctx, cs := connectWithTools(t, client)

	out := callToolJSON[GetJobStatusOutput](t, ctx, cs, "get_job_status", map[string]any{
		"job_id": "job-running",
	})

	if len(out.Benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(out.Benchmarks))
	}
	b := out.Benchmarks[0]
	if b.ResultInterpretation != "" {
		t.Errorf("running benchmark should not have result_interpretation, got %q", b.ResultInterpretation)
	}
	if len(b.Complements) != 0 {
		t.Errorf("running benchmark should not have complements, got %v", b.Complements)
	}
}

// --- RegisterHandlers with tools ---

func TestRegisterHandlersWithNilClientHasNoTools(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Build: "test"}
	srv := New(info, discardLogger, nil)

	if err := RegisterHandlers(srv, nil, info, discardLogger, evalhubclient.DefaultListPageLimit); err != nil {
		t.Fatalf("RegisterHandlers: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs := connectClient(t, ctx, srv)

	toolsResult, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(toolsResult.Tools) != 0 {
		t.Errorf("expected 0 tools with nil client, got %d", len(toolsResult.Tools))
	}
}
