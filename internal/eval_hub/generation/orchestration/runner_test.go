package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type modelFunc func(context.Context, loop.ModelRequest) (loop.ModelResponse, error)

func (f modelFunc) Complete(ctx context.Context, request loop.ModelRequest) (loop.ModelResponse, error) {
	return f(ctx, request)
}

type fakeCatalog struct {
	catalogSnapshot
	providerReads   int
	collectionReads int
	details         int
	providersError  error
	beforeRead      func(*fakeCatalog)
}

func (f *fakeCatalog) ListProviders(ctx context.Context, offset, limit int) (collectiondesign.Page[api.ProviderResource], error) {
	f.providerReads++
	if f.beforeRead != nil {
		f.beforeRead(f)
	}
	if f.providersError != nil {
		return collectiondesign.Page[api.ProviderResource]{}, f.providersError
	}
	return f.catalogSnapshot.ListProviders(ctx, offset, limit)
}

func (f *fakeCatalog) ListCollections(ctx context.Context, offset, limit int) (collectiondesign.Page[api.CollectionResource], error) {
	f.collectionReads++
	return f.catalogSnapshot.ListCollections(ctx, offset, limit)
}

func (f *fakeCatalog) GetProvider(_ context.Context, id string) (api.ProviderResource, error) {
	f.details++
	for _, provider := range f.providers {
		if provider.Resource.ID == id {
			return provider, nil
		}
	}
	return api.ProviderResource{}, errors.New("missing provider")
}

func jobSnapshot(t *testing.T) generation.JobConfig {
	t.Helper()
	request, err := generation.NormalizeRequest(api.CollectionGenerationRequest{
		EvaluationGoal: "Evaluate toxicity", Model: api.ModelRef{
			Name: "generator", URL: "https://private-model.example/v1", Auth: &api.ModelAuth{SecretRef: "private-secret"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error or value: %+v", err)
	}
	budget, err := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	if err != nil {
		t.Fatalf("unexpected error or value: %+v", err)
	}
	return generation.JobConfig{Request: request, Limits: budget.Limits, ReasoningEffort: "medium"}
}

func testCatalog() *fakeCatalog {
	provider := api.ProviderResource{Resource: api.Resource{ID: "safety"}, ProviderConfig: api.ProviderConfig{
		Runtime: &api.Runtime{K8s: &api.K8sRuntime{Image: "test-image"}},
	}}
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		provider.Benchmarks = append(provider.Benchmarks, api.BenchmarkResource{
			ID: "toxicity-" + suffix, Name: "Toxicity " + suffix, Domains: []string{"safety"},
			Metrics: []string{"risk"}, PrimaryScore: &api.PrimaryScore{Metric: "risk", LowerIsBetter: true},
			EvaluationTargets: []string{"model"}, Tasks: []string{"safety_classification"}, Tags: []string{"toxicity"},
		})
	}
	provider.Benchmarks = append(provider.Benchmarks, api.BenchmarkResource{
		ID: "unrelated", Name: "Algebra", Domains: []string{"math"},
	})
	threshold := float32(0.2)
	return &fakeCatalog{catalogSnapshot: catalogSnapshot{
		providers: []api.ProviderResource{provider},
		collections: []api.CollectionResource{
			{Resource: api.Resource{ID: "curated", Owner: "system"}, CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{{
					Ref: api.Ref{ID: "toxicity-a"}, ProviderID: "safety", Weight: 2,
					Parameters:   map[string]any{"judge_model": "", "num_examples": float64(10)},
					PassCriteria: &api.PassCriteria{Threshold: &threshold},
					PrimaryScore: &api.PrimaryScore{Metric: "risk", LowerIsBetter: true},
				}},
			}},
			{Resource: api.Resource{ID: "custom", Owner: "someone-else", Tenant: "test-tenant"}, CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "toxicity-a"}, ProviderID: "safety", Parameters: map[string]any{"judge_model": "private-judge"}}},
			}},
		},
	}}
}

func testDraft(ids ...string) draft {
	proposal := draft{
		Collection: api.CollectionConfig{
			Name: "Model title", Description: "Model description", Domains: []string{"safety"},
		},
		Rationale: "Measure toxicity across the selected tasks.",
	}
	for _, id := range ids {
		proposal.Collection.Benchmarks = append(proposal.Collection.Benchmarks,
			api.CollectionBenchmarkConfig{Ref: api.Ref{ID: id}, ProviderID: "safety"})
		proposal.Explanation.SelectedBenchmarks = append(proposal.Explanation.SelectedBenchmarks, reason(id))
	}
	return proposal
}

func reason(id string) api.CollectionGenerationBenchmarkReason {
	return api.CollectionGenerationBenchmarkReason{ProviderID: "safety", ID: id, Reason: "Measures toxicity relevant to this goal."}
}

func finalResponse(t *testing.T, value any) loop.ModelResponse {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("unexpected error or value: %+v", err)
	}
	return loop.ModelResponse{Message: loop.Message{Content: string(wire)}}
}

func toolResponse(name, args string) loop.ModelResponse {
	return loop.ModelResponse{FinishReason: "tool_calls", Message: loop.Message{ToolCalls: []loop.ToolCall{{
		ID: name, Type: "function", Function: loop.FunctionCall{Name: name, Arguments: args},
	}}}}
}

func passGuardrail(context.Context, string) (string, error) { return "", nil }

func TestRunnerCompletesPostableProposalWithoutModelToolCalls(t *testing.T) {
	t.Parallel()
	snapshot := jobSnapshot(t)
	snapshot.Request.CollectionName = "Requested title"
	snapshot.Request.CollectionDescription = "Requested description"
	snapshot.Request.MaxBenchmarks = 2
	source := testCatalog()
	proposal := testDraft("toxicity-b", "toxicity-a")
	slices.Reverse(proposal.Explanation.SelectedBenchmarks)
	for _, suffix := range []string{"h", "g", "f", "e", "d", "c"} {
		proposal.Explanation.RejectedBenchmarks = append(proposal.Explanation.RejectedBenchmarks, reason("toxicity-"+suffix))
	}
	var phases []string
	runner := Runner{
		Catalog: source, Guardrail: passGuardrail,
		Model: modelFunc(func(_ context.Context, request loop.ModelRequest) (loop.ModelResponse, error) {
			checkEqual(t, 3, len(request.Tools))
			checkEqual(t, "medium", request.ReasoningEffort)
			checkEqual(t, "generator", request.Model)
			if strings.Contains(request.Messages[2].Content, "private-secret") {
				t.Fatalf("unexpected contents: %+v", request.Messages[2].Content)
			}
			if strings.Contains(request.Messages[2].Content, "private-model.example") {
				t.Fatalf("unexpected contents: %+v", request.Messages[2].Content)
			}
			if !strings.Contains(request.Messages[2].Content, "toxicity-a") {
				t.Fatalf("unexpected contents: %+v", request.Messages[2].Content)
			}
			if strings.Contains(request.Messages[2].Content, "toxicity-h") {
				t.Fatalf("unexpected contents: %+v", request.Messages[2].Content)
			}
			return finalResponse(t, proposal), nil
		}),
		Progress: func(_ context.Context, phase, _ string) error {
			phases = append(phases, phase)
			return nil
		},
	}
	result, failure := runner.Run(context.Background(), snapshot)
	if failure != nil {
		t.Fatalf("unexpected error or value: %+v", failure)
	}
	if err := generation.ValidateResult(snapshot.Request, result); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, "Requested title", result.Collection.Name)
	checkEqual(t, "Requested description", result.Collection.Description)
	checkEqual(t, "toxicity-b", result.Explanation.SelectedBenchmarks[0].ID)
	checkEqual(t, 5, len(result.Explanation.RejectedBenchmarks))
	checkEqual(t, "toxicity-c", result.Explanation.RejectedBenchmarks[0].ID)
	checkEqual(t, "toxicity-g", result.Explanation.RejectedBenchmarks[4].ID)
	if result.Collection.Benchmarks[0].PassCriteria != nil {
		t.Fatalf("unexpected error or value: %+v", result.Collection.Benchmarks[0].PassCriteria)
	}
	checkEqual(t, map[string]any{"judge_model": "", "num_examples": float64(10)}, result.Collection.Benchmarks[1].Parameters)
	calibrated := result.Collection.Benchmarks[1]
	if calibrated.PassCriteria == nil || calibrated.PassCriteria.Threshold == nil || *calibrated.PassCriteria.Threshold != 0.2 ||
		calibrated.Weight != 2 || calibrated.PrimaryScore == nil || !calibrated.PrimaryScore.LowerIsBetter {
		t.Fatalf("did not preserve comparable calibration evidence: %+v", calibrated)
	}
	if !strings.Contains(result.Rationale, "safety/toxicity-a: supply a judge") {
		t.Fatalf("unexpected contents: %+v", result.Rationale)
	}
	if !strings.Contains(result.Rationale, "does not establish all runtime prerequisites") {
		t.Fatalf("unexpected contents: %+v", result.Rationale)
	}
	if strings.Contains(result.YAML, "private-judge") {
		t.Fatalf("unexpected contents: %+v", result.YAML)
	}
	checkEqual(t, []string{"guardrails", "catalog_discovery", "catalog_discovery", "model_generation", "validation", "validation", "validation", "finalization"}, phases)
	checkEqual(t, 2, source.providerReads)
	checkEqual(t, 0, source.details)
}

func TestRunnerCallsCatalogToolsAndCorrectsUnverifiedDraft(t *testing.T) {
	t.Parallel()
	source := testCatalog()
	snapshot := jobSnapshot(t)
	snapshot.Request.ProviderFilter = "safety"
	calls := 0
	bad := testDraft("toxicity-a")
	bad.Collection.Benchmarks[0].PrimaryScore = &api.PrimaryScore{Metric: "invented_metric"}
	runner := Runner{
		Catalog: source, Guardrail: passGuardrail,
		Model: modelFunc(func(_ context.Context, request loop.ModelRequest) (loop.ModelResponse, error) {
			calls++
			last := request.Messages[len(request.Messages)-1].Content
			switch calls {
			case 1:
				return toolResponse(searchBenchmarks, `{"domain":"safety","tag":"toxicity","task":"safety_classification","capability":"model","limit":1}`), nil
			case 2:
				if !strings.Contains(last, "toxicity-a") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				if !strings.Contains(last, `"next_offset":1`) {
					t.Fatalf("unexpected contents: %+v", last)
				}
				return toolResponse(getProviderDetails, `{"provider_id":"safety","offset":1,"limit":1}`), nil
			case 3:
				if !strings.Contains(last, "toxicity-b") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				if !strings.Contains(last, `"lower_is_better":true`) {
					t.Fatalf("unexpected contents: %+v", last)
				}
				return toolResponse(listCollections, `{"limit":2}`), nil
			case 4:
				if !strings.Contains(last, "curated") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				if !strings.Contains(last, "custom") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				if strings.Contains(last, "private-judge") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				return finalResponse(t, bad), nil
			case 5:
				if !strings.Contains(last, "metric_not_advertised") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				if strings.Contains(last, "invented_metric") {
					t.Fatalf("unexpected contents: %+v", last)
				}
				return finalResponse(t, testDraft("toxicity-a")), nil
			default:
				return loop.ModelResponse{}, errors.New("unexpected model call")
			}
		}),
	}
	result, failure := runner.Run(context.Background(), snapshot)
	if failure != nil {
		t.Fatalf("unexpected error or value: %+v", failure)
	}
	checkEqual(t, 5, calls)
	checkEqual(t, 1, source.details)
	checkEqual(t, 2, source.collectionReads)
	if !slices.Contains(result.Actions, api.CollectionGenerationAction{
		Name: "validate_proposal", Outcome: "error", Summary: "Collection proposal validation could not be completed.",
	}) {
		t.Fatalf("unexpected contents: %+v", result.Actions)
	}
}

func TestRunnerStopsBeforeModelWhenGateOrCatalogCannotAdmit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, code, category string
		configure            func(*Runner, *fakeCatalog, *generation.JobConfig)
		noCatalog            bool
	}{
		{"guardrails blocked", "guardrails_rejected", "topic_relevance", func(r *Runner, _ *fakeCatalog, _ *generation.JobConfig) {
			r.Guardrail = func(context.Context, string) (string, error) { return "topic_relevance", nil }
		}, true},
		{"guardrails unavailable", "guardrails_unavailable", "", func(r *Runner, _ *fakeCatalog, _ *generation.JobConfig) {
			r.Guardrail = func(context.Context, string) (string, error) { return "", errors.New("private upstream response") }
		}, true},
		{"unknown guardrail category", "guardrails_unavailable", "", func(r *Runner, _ *fakeCatalog, _ *generation.JobConfig) {
			r.Guardrail = func(context.Context, string) (string, error) { return "unknown", nil }
		}, true},
		{"off topic", "no_relevant_benchmarks", "", func(_ *Runner, _ *fakeCatalog, s *generation.JobConfig) {
			s.Request.EvaluationGoal = "Organize my favorite recipes into a searchable collection"
		}, false},
		{"invalid provider filter", "validation_failed", "", func(_ *Runner, _ *fakeCatalog, s *generation.JobConfig) {
			s.Request.ProviderFilter = "not-visible"
		}, false},
		{"provider filter excludes the matches", "no_relevant_benchmarks", "", func(_ *Runner, c *fakeCatalog, s *generation.JobConfig) {
			c.providers = append(c.providers, api.ProviderResource{Resource: api.Resource{ID: "math"}, ProviderConfig: api.ProviderConfig{
				Benchmarks: []api.BenchmarkResource{{ID: "algebra", Name: "Math"}},
			}})
			s.Request.ProviderFilter = "math"
		}, false},
		{"adapter explicitly unavailable", "guardrails_rejected", "unsupported_capability", func(_ *Runner, c *fakeCatalog, _ *generation.JobConfig) {
			c.providers[0].Runtime.K8s = nil
		}, false},
		{"unsupported explicit target", "guardrails_rejected", "unsupported_capability", func(_ *Runner, _ *fakeCatalog, s *generation.JobConfig) {
			s.Request.EvaluationGoal = "Evaluate toxicity of an AI agent"
		}, false},
		{"catalog unavailable", "tool_failed", "", func(_ *Runner, c *fakeCatalog, _ *generation.JobConfig) {
			c.providersError = errors.New("private upstream response")
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, snapshot := testCatalog(), jobSnapshot(t)
			runner := Runner{Catalog: source, Guardrail: passGuardrail, Model: modelFunc(func(context.Context, loop.ModelRequest) (loop.ModelResponse, error) {
				t.Fatal("model was contacted on a preflight failure")
				return loop.ModelResponse{}, nil
			})}
			tc.configure(&runner, source, &snapshot)
			result, failure := runner.Run(context.Background(), snapshot)
			if failure == nil {
				t.Fatal("expected a generation failure")
			}
			checkEqual(t, 0, len(result.Collection.Benchmarks))
			checkEqual(t, tc.code, failure.Code)
			checkEqual(t, tc.category, failure.Category)
			if strings.Contains(failure.Message, "private upstream response") {
				t.Fatalf("unexpected contents: %+v", failure.Message)
			}
			if tc.noCatalog {
				checkEqual(t, 0, source.providerReads)
			}
			checkEqual(t, 0, source.details)
			checkEqual(t, 0, source.collectionReads)
		})
	}
}

func TestRunnerDiscardsUnsafeToolBatchesBeforeLookup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, args string }{
		{"browse_web", `{"query":"toxicity"}`},
		{searchBenchmarks, `{"query":"https://example.invalid"}`},
		{getProviderDetails, `{"provider_id":"../../jobs"}`},
		{getProviderDetails, `{"provider_id":"safety","path":"/jobs","method":"POST","headers":{"X-Tenant":"other"}}`},
		{listCollections, `{"offset":-1}`},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			source := testCatalog()
			runner := Runner{
				Catalog: source, Guardrail: passGuardrail,
				Model: modelFunc(func(context.Context, loop.ModelRequest) (loop.ModelResponse, error) {
					return toolResponse(tc.name, tc.args), nil
				}),
			}
			_, failure := runner.Run(context.Background(), jobSnapshot(t))
			if failure == nil {
				t.Fatal("expected a generation failure")
			}
			checkEqual(t, "model_failed", failure.Code)
			checkEqual(t, 1, source.providerReads)
			checkEqual(t, 0, source.details)
			checkEqual(t, 0, source.collectionReads)
		})
	}
}

func TestRunnerBoundsCorrectionsAndChecksCurrentCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		limit     int
		draft     func(int) draft
		changed   bool
		wantCalls int
		wantCode  string
	}{
		{"irrelevant draft exhausts retries", 8, func(int) draft { return testDraft("unrelated") }, false, 3, "validation_failed"},
		{"corrections share model budget", 2, func(int) draft { return testDraft("unknown") }, false, 2, "validation_failed"},
		{"revises after catalog changes", 8, func(turn int) draft {
			if turn == 1 {
				return testDraft("toxicity-a")
			}
			return testDraft("toxicity-b")
		}, true, 2, ""},
		{"refuses missing explanation", 8, func(int) draft {
			bad := testDraft("toxicity-a")
			bad.Explanation.SelectedBenchmarks = nil
			return bad
		}, false, 3, "validation_failed"},
		{"rejects duplicate, overlapping, or oversized explanations", 8, func(turn int) draft {
			bad := testDraft("toxicity-a")
			switch turn {
			case 1:
				bad.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{reason("toxicity-a")}
			case 2:
				bad.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{reason("toxicity-b"), reason("toxicity-b")}
			case 3:
				bad.Explanation.SelectedBenchmarks[0].Reason = strings.Repeat("x", 301)
			}
			return bad
		}, false, 3, "validation_failed"},
		{"rejects unknown rejected pairs", 8, func(int) draft {
			bad := testDraft("toxicity-a")
			bad.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{reason("not-in-catalog")}
			return bad
		}, false, 3, "validation_failed"},
		{"refuses invented parameters", 8, func(int) draft {
			bad := testDraft("toxicity-a")
			bad.Collection.Benchmarks[0].Parameters = map[string]any{"judge_model": "invented"}
			return bad
		}, false, 3, "validation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, snapshot := testCatalog(), jobSnapshot(t)
			snapshot.Limits.MaxModelCalls = tc.limit
			snapshot.Limits.MaxIterations = min(snapshot.Limits.MaxIterations, tc.limit)
			if tc.changed {
				source.beforeRead = func(c *fakeCatalog) {
					if c.providerReads == 2 {
						c.providers[0].Benchmarks = c.providers[0].Benchmarks[1:]
					}
				}
			}
			calls := 0
			runner := Runner{Catalog: source, Guardrail: passGuardrail, Model: modelFunc(func(_ context.Context, request loop.ModelRequest) (loop.ModelResponse, error) {
				calls++
				if calls > 1 {
					last := request.Messages[len(request.Messages)-1].Content
					if !(strings.HasPrefix(last, "Validation findings: [")) {
						t.Fatalf("unrecognized correction: %s", last)
					}
					if tc.changed {
						if !strings.Contains(last, "catalog_changed") {
							t.Fatalf("unexpected contents: %+v", last)
						}
					}
				}
				return finalResponse(t, tc.draft(calls)), nil
			})}
			result, failure := runner.Run(context.Background(), snapshot)
			checkEqual(t, tc.wantCalls, calls)
			if tc.wantCode == "" {
				if failure != nil {
					t.Fatalf("unexpected error or value: %+v", failure)
				}
				checkEqual(t, "toxicity-b", result.Collection.Benchmarks[0].ID)
			} else {
				if failure == nil {
					t.Fatal("expected a generation failure")
				}
				checkEqual(t, tc.wantCode, failure.Code)
			}
		})
	}
}

func checkEqual(t *testing.T, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRunnerHandlesTimeoutAndModelFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code string
		fail func(context.Context) error
	}{
		{"model_failed", func(context.Context) error { return errors.New("model provider details") }},
		{"timeout", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			snapshot := jobSnapshot(t)
			snapshot.Limits.MaxDuration = 150 * time.Millisecond
			runner := Runner{Catalog: testCatalog(), Guardrail: passGuardrail, Model: modelFunc(func(ctx context.Context, _ loop.ModelRequest) (loop.ModelResponse, error) {
				return loop.ModelResponse{}, tc.fail(ctx)
			})}
			_, failure := runner.Run(context.Background(), snapshot)
			if failure == nil || failure.Code != tc.code || strings.Contains(failure.Message, "provider details") {
				t.Fatalf("unexpected failure: %+v", failure)
			}
		})
	}
}

func TestProposalRejectsPrimaryScoreWithoutAdvertisedDirection(t *testing.T) {
	t.Parallel()
	source := testCatalog()
	source.providers[0].Benchmarks[0].Metrics = append(source.providers[0].Benchmarks[0].Metrics, "secondary")
	snapshot := jobSnapshot(t)
	options, err := collectiondesign.ParseOptions(snapshot.Request.EvaluationGoal, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	shortlist, issue := discover(source.providers, options)
	checkEqual(t, "", issue)
	proposal := testDraft("toxicity-a")
	proposal.Collection.Benchmarks[0].PrimaryScore = &api.PrimaryScore{Metric: "secondary"}
	_, findings, err := proposal.validate(context.Background(), source.catalogSnapshot, options, snapshot.Request, indexCandidates(shortlist))
	if err != nil {
		t.Fatal(err)
	}
	checkEqual(t, []finding{{Code: "unverified_score_direction", Path: "collection.benchmarks"}}, findings)
}

func TestCatalogReadsAllBoundedPages(t *testing.T) {
	t.Parallel()
	source := testCatalog()
	first := source.providers[0]
	source.providers = nil
	for i := range catalogPageSize + 1 {
		p := first
		p.Resource.ID = fmt.Sprintf("provider-%02d", i)
		source.providers = append(source.providers, p)
	}
	providers, err := readProviders(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	checkEqual(t, 2, source.providerReads)
	checkEqual(t, catalogPageSize+1, len(providers))
}
