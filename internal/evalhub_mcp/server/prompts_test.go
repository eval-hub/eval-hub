package server

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- test helpers ---

func connectWithPrompts(t *testing.T) (context.Context, *mcp.ClientSession) {
	t.Helper()
	return connectWithPromptsAndDS(t, &mockDataSource{})
}

func connectWithPromptsAndDS(t *testing.T, ds EvalHubDiscovery) (context.Context, *mcp.ClientSession) {
	t.Helper()

	srv := New(&ServerInfo{Build: "test"}, discardLogger, nil)
	if err := registerPrompts(srv, ds, discardLogger); err != nil {
		t.Fatalf("registerPrompts failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	return ctx, connectClient(t, ctx, srv)
}

func getPrompt(t *testing.T, ctx context.Context, cs *mcp.ClientSession, name string, args map[string]string) *mcp.GetPromptResult {
	t.Helper()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("GetPrompt(%s) failed: %v", name, err)
	}
	return result
}

func getPromptExpectError(t *testing.T, ctx context.Context, cs *mcp.ClientSession, name string, args map[string]string) string {
	t.Helper()
	_, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      name,
		Arguments: args,
	})
	if err == nil {
		t.Fatalf("GetPrompt(%s): expected error, got nil", name)
	}
	return err.Error()
}

// --- prompts/list ---

func TestPromptsListIncludesAll(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}

	want := map[string]bool{
		"edd_workflow":      false,
		"evaluate_model":    false,
		"compare_runs":      false,
		"design_collection": false,
	}
	for _, p := range result.Prompts {
		if _, ok := want[p.Name]; ok {
			want[p.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("prompts/list missing %s", name)
		}
	}
}

func TestPromptsHaveDescriptions(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}

	for _, p := range result.Prompts {
		if p.Description == "" {
			t.Errorf("prompt %q has empty description", p.Name)
		}
	}
}

func TestPromptsHaveArgumentMetadata(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}

	wantArgs := map[string]map[string]bool{
		"edd_workflow":      {"application_type": true},
		"evaluate_model":    {"model_url": false, "benchmark_preferences": false},
		"compare_runs":      {"job_ids": false},
		"design_collection": {"evaluation_goal": true, "provider_filter": false, "max_benchmarks": false, "strictness": false},
	}

	for _, p := range result.Prompts {
		expected, ok := wantArgs[p.Name]
		if !ok {
			continue
		}
		argMap := map[string]bool{}
		for _, arg := range p.Arguments {
			argMap[arg.Name] = arg.Required
			if arg.Description == "" {
				t.Errorf("prompt %q argument %q has empty description", p.Name, arg.Name)
			}
		}
		for argName, wantRequired := range expected {
			gotRequired, exists := argMap[argName]
			if !exists {
				t.Errorf("prompt %q missing argument %q", p.Name, argName)
				continue
			}
			if gotRequired != wantRequired {
				t.Errorf("prompt %q argument %q: required = %v, want %v", p.Name, argName, gotRequired, wantRequired)
			}
		}
	}
}

// TestPromptArgumentOrder verifies that prompt arguments are surfaced in their
// declared (YAML) order rather than a non-deterministic map order.
func TestPromptArgumentOrder(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}

	wantOrder := map[string][]string{
		"design_collection": {"evaluation_goal", "max_benchmarks", "strictness", "provider_filter"},
	}

	for _, p := range result.Prompts {
		expected, ok := wantOrder[p.Name]
		if !ok {
			continue
		}
		got := make([]string, 0, len(p.Arguments))
		for _, arg := range p.Arguments {
			got = append(got, arg.Name)
		}
		if !slices.Equal(got, expected) {
			t.Errorf("prompt %q argument order = %v, want %v", p.Name, got, expected)
		}
	}
}

// --- edd_workflow ---

func TestEddWorkflowRag(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "edd_workflow", map[string]string{
		"application_type": "rag",
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	for _, keyword := range []string{"Define", "Measure", "Iterate", "RAG", "retrieval"} {
		if !containsCI(text, keyword) {
			t.Errorf("rag guidance missing keyword %q", keyword)
		}
	}
}

func TestEddWorkflowAgent(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "edd_workflow", map[string]string{
		"application_type": "agent",
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	for _, keyword := range []string{"Define", "Measure", "Iterate", "agent", "tool"} {
		if !containsCI(text, keyword) {
			t.Errorf("agent guidance missing keyword %q", keyword)
		}
	}
}

func TestEddWorkflowSafety(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "edd_workflow", map[string]string{
		"application_type": "safety",
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	for _, keyword := range []string{"Define", "Measure", "Iterate", "safety", "harmful"} {
		if !containsCI(text, keyword) {
			t.Errorf("safety guidance missing keyword %q", keyword)
		}
	}
}

func TestEddWorkflowClassifier(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "edd_workflow", map[string]string{
		"application_type": "classifier",
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	for _, keyword := range []string{"Define", "Measure", "Iterate", "classifier", "precision"} {
		if !containsCI(text, keyword) {
			t.Errorf("classifier guidance missing keyword %q", keyword)
		}
	}
}

func TestEddWorkflowDistinctGuidancePerType(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	responses := map[string]string{}
	for _, appType := range validApplicationTypes {
		result := getPrompt(t, ctx, cs, "edd_workflow", map[string]string{
			"application_type": appType,
		})
		responses[appType] = allMessageText(result.Messages)
	}

	for i, a := range validApplicationTypes {
		for _, b := range validApplicationTypes[i+1:] {
			if responses[a] == responses[b] {
				t.Errorf("edd_workflow produced identical guidance for %q and %q", a, b)
			}
		}
	}
}

func TestEddWorkflowMissingApplicationType(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	errMsg := getPromptExpectError(t, ctx, cs, "edd_workflow", map[string]string{})

	if !strings.Contains(errMsg, "application_type") {
		t.Errorf("error should mention application_type, got: %s", errMsg)
	}
	for _, valid := range validApplicationTypes {
		if !strings.Contains(errMsg, valid) {
			t.Errorf("error should list valid value %q, got: %s", valid, errMsg)
		}
	}
}

func TestEddWorkflowInvalidApplicationType(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	errMsg := getPromptExpectError(t, ctx, cs, "edd_workflow", map[string]string{
		"application_type": "chatbot",
	})

	if !strings.Contains(errMsg, "chatbot") {
		t.Errorf("error should mention the invalid value, got: %s", errMsg)
	}
	for _, valid := range validApplicationTypes {
		if !strings.Contains(errMsg, valid) {
			t.Errorf("error should list valid value %q, got: %s", valid, errMsg)
		}
	}
}

// --- evaluate_model ---

func TestEvaluateModelWithoutModelURL(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "evaluate_model", map[string]string{})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	if !containsCI(text, "provide") && !containsCI(text, "identify") {
		t.Error("prompt without model_url should include model collection step")
	}
	if !containsCI(text, "benchmark") {
		t.Error("prompt should include benchmark selection step")
	}
	if !containsCI(text, "submit") {
		t.Error("prompt should include job submission step")
	}
}

func TestEvaluateModelWithModelURL(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	modelURL := "http://my-model:8080/v1"
	result := getPrompt(t, ctx, cs, "evaluate_model", map[string]string{
		"model_url": modelURL,
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	if !strings.Contains(text, modelURL) {
		t.Error("prompt with model_url should reference the provided URL")
	}
	if containsCI(text, "provide the URL") {
		t.Error("prompt with model_url should skip the collection step")
	}
	if !containsCI(text, "benchmark") {
		t.Error("prompt should include benchmark selection step")
	}
	if !containsCI(text, "submit") {
		t.Error("prompt should include job submission step")
	}
}

func TestEvaluateModelWithBenchmarkPreferences(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "evaluate_model", map[string]string{
		"model_url":             "http://model:8080",
		"benchmark_preferences": "safety",
	})

	text := allMessageText(result.Messages)
	if !strings.Contains(text, "safety") {
		t.Error("prompt with benchmark_preferences should reference the preferences")
	}
}

func TestEvaluateModelIncludesAllSteps(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "evaluate_model", map[string]string{
		"model_url": "http://model:8080",
	})

	text := allMessageText(result.Messages)
	for _, step := range []string{"benchmark", "experiment", "submit", "monitor"} {
		if !containsCI(text, step) {
			t.Errorf("evaluate_model guidance missing step keyword %q", step)
		}
	}
}

// --- compare_runs ---

func TestCompareRunsWithoutJobIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "compare_runs", map[string]string{})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	if !containsCI(text, "select") {
		t.Error("prompt without job_ids should include job selection step")
	}
	if !containsCI(text, "compare") {
		t.Error("prompt should include comparison step")
	}
}

func TestCompareRunsWhitespaceOnlyJobIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "compare_runs", map[string]string{
		"job_ids": "   \t  ",
	})

	text := allMessageText(result.Messages)
	if !containsCI(text, "across multiple runs") {
		t.Error("whitespace-only job_ids should use the no-job-id intro (selection guidance)")
	}
	if strings.Contains(text, "I want to compare evaluation jobs:") {
		t.Error("whitespace-only job_ids must not use the with-ids user message")
	}
}

func TestCompareRunsWithJobIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "compare_runs", map[string]string{
		"job_ids": "job-1, job-2, job-3",
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	for _, id := range []string{"job-1", "job-2", "job-3"} {
		if !strings.Contains(text, id) {
			t.Errorf("prompt with job_ids should reference %q", id)
		}
	}
}

func TestCompareRunsWithJobIDsSkipsSelection(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	withIDs := getPrompt(t, ctx, cs, "compare_runs", map[string]string{
		"job_ids": "job-a, job-b",
	})
	withoutIDs := getPrompt(t, ctx, cs, "compare_runs", map[string]string{})

	textWithIDs := allMessageText(withIDs.Messages)
	textWithoutIDs := allMessageText(withoutIDs.Messages)

	if textWithIDs == textWithoutIDs {
		t.Error("compare_runs should produce different guidance with and without job_ids")
	}
}

func TestCompareRunsIncludesComparisonSteps(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPrompts(t)

	result := getPrompt(t, ctx, cs, "compare_runs", map[string]string{
		"job_ids": "job-1, job-2",
	})

	text := allMessageText(result.Messages)
	for _, step := range []string{"compare", "metrics", "summarize"} {
		if !containsCI(text, step) {
			t.Errorf("compare_runs guidance missing step keyword %q", step)
		}
	}
}

// --- design_collection ---

func testDesignCollectionDS() *mockDataSource {
	return &mockDataSource{
		providers: []api.ProviderResource{
			{
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfig: api.ProviderConfig{
					Name: "LM Evaluation Harness",
					Benchmarks: []api.BenchmarkResource{
						{
							ID:          "toxigen",
							Name:        "Toxigen",
							Description: "Toxicity detection benchmark",
							Tags:        []string{"safety", "toxicity"},
							Metrics:     []string{"acc"},
						},
						{
							ID:          "gsm8k",
							Name:        "GSM8K",
							Description: "Grade school math word problems",
							Tags:        []string{"reasoning", "math"},
							Metrics:     []string{"exact_match"},
						},
					},
				},
			},
			{
				Resource: api.Resource{ID: "lighteval"},
				ProviderConfig: api.ProviderConfig{
					Name: "LightEval",
					Benchmarks: []api.BenchmarkResource{
						{
							ID:          "ifeval",
							Name:        "IFEval",
							Description: "Instruction following evaluation",
							Tags:        []string{"instruction_following"},
							Metrics:     []string{"inst_level_strict_acc"},
						},
					},
				},
			},
		},
		collections: []api.CollectionResource{
			{
				Resource: api.Resource{ID: "safety-v1", Owner: "system"},
				CollectionConfig: api.CollectionConfig{
					Name:    "Safety Suite v1",
					Domains: []string{"safety"},
					Tags:    []string{"safety"},
					Benchmarks: []api.CollectionBenchmarkConfig{
						{Ref: api.Ref{ID: "toxigen"}, ProviderID: "lm_evaluation_harness", Weight: 3},
					},
				},
			},
		},
	}
}

func TestDesignCollectionBasic(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPromptsAndDS(t, testDesignCollectionDS())

	result := getPrompt(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "enterprise safety deployment",
	})

	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	assertMessageRoles(t, result.Messages)

	text := allMessageText(result.Messages)
	for _, keyword := range []string{"enterprise safety deployment", "toxigen", "gsm8k", "Safety Suite v1", "Threshold"} {
		if !containsCI(text, keyword) {
			t.Errorf("design_collection output missing keyword %q", keyword)
		}
	}
}

func TestDesignCollectionWithProviderFilter(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPromptsAndDS(t, testDesignCollectionDS())

	resultFiltered := getPrompt(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "safety check",
		"provider_filter": "lighteval",
	})
	resultAll := getPrompt(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "safety check",
	})

	textFiltered := allMessageText(resultFiltered.Messages)
	textAll := allMessageText(resultAll.Messages)

	if !containsCI(textFiltered, "ifeval") {
		t.Error("filtered prompt should include lighteval benchmarks")
	}
	if strings.Contains(textFiltered, `"name": "Toxigen"`) {
		t.Error("filtered prompt benchmark catalog should not include Toxigen (lm_evaluation_harness)")
	}
	if !strings.Contains(textAll, `"name": "Toxigen"`) {
		t.Error("unfiltered prompt should include Toxigen in benchmark catalog")
	}
	if textFiltered == textAll {
		t.Error("filtered and unfiltered prompts should differ")
	}
}

func TestDesignCollectionStrictnessValidation(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPromptsAndDS(t, testDesignCollectionDS())

	errMsg := getPromptExpectError(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "test",
		"strictness":      "extreme",
	})
	if !strings.Contains(errMsg, "extreme") {
		t.Errorf("error should mention invalid value, got: %s", errMsg)
	}
	for _, valid := range validStrictness {
		if !strings.Contains(errMsg, valid) {
			t.Errorf("error should list valid value %q, got: %s", valid, errMsg)
		}
	}
}

func TestDesignCollectionMaxBenchmarksValidation(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPromptsAndDS(t, testDesignCollectionDS())

	for _, bad := range []string{"abc", "0", "-3"} {
		errMsg := getPromptExpectError(t, ctx, cs, "design_collection", map[string]string{
			"evaluation_goal": "test",
			"max_benchmarks":  bad,
		})
		if !strings.Contains(errMsg, "max_benchmarks") {
			t.Errorf("max_benchmarks=%q: error should mention max_benchmarks, got: %s", bad, errMsg)
		}
	}
}

func TestDesignCollectionMissingGoal(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPromptsAndDS(t, testDesignCollectionDS())

	errMsg := getPromptExpectError(t, ctx, cs, "design_collection", map[string]string{})
	if !strings.Contains(errMsg, "evaluation_goal") {
		t.Errorf("error should mention evaluation_goal, got: %s", errMsg)
	}
}

func TestDesignCollectionDefaultStrictness(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithPromptsAndDS(t, testDesignCollectionDS())

	result := getPrompt(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "general evaluation",
	})

	text := allMessageText(result.Messages)
	if !strings.Contains(text, "Strictness: **moderate**") {
		t.Error("default strictness should be moderate")
	}
}

func TestDesignCollectionOnlySystemCollections(t *testing.T) {
	t.Parallel()
	ds := testDesignCollectionDS()
	ds.collections = append(ds.collections, api.CollectionResource{
		Resource: api.Resource{ID: "tenant-col", Owner: "my-tenant"},
		CollectionConfig: api.CollectionConfig{
			Name:    "Tenant Collection",
			Domains: []string{"general"},
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "gsm8k"}, ProviderID: "lm_evaluation_harness"},
			},
		},
	})
	ctx, cs := connectWithPromptsAndDS(t, ds)

	result := getPrompt(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "test",
	})

	text := allMessageText(result.Messages)
	if containsCI(text, "Tenant Collection") {
		t.Error("design_collection should only include system collections as examples")
	}
}

func TestDesignCollectionEmptyProvidersErrors(t *testing.T) {
	t.Parallel()
	ds := &mockDataSource{}
	ctx, cs := connectWithPromptsAndDS(t, ds)

	errMsg := getPromptExpectError(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "safety check",
	})

	if !strings.Contains(errMsg, "benchmark catalog is empty") {
		t.Errorf("expected empty-catalog error, got: %q", errMsg)
	}
}

func TestDesignCollectionEmptyProviderFilterErrors(t *testing.T) {
	t.Parallel()
	ds := testDesignCollectionDS()
	ctx, cs := connectWithPromptsAndDS(t, ds)

	errMsg := getPromptExpectError(t, ctx, cs, "design_collection", map[string]string{
		"evaluation_goal": "safety check",
		"provider_filter": "nonexistent_provider",
	})

	if !strings.Contains(errMsg, "nonexistent_provider") {
		t.Errorf("expected filter-specific empty-catalog error, got: %q", errMsg)
	}
}

// --- RegisterHandlers with prompts ---

func TestRegisterHandlersIncludesPromptsWithoutBackend(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Build: "test"}
	srv := New(info, discardLogger, nil)

	if err := RegisterHandlers(srv, nil, info, discardLogger, evalhubclient.DefaultListPageLimit); err != nil {
		t.Fatalf("RegisterHandlers: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	cs := connectClient(t, ctx, srv)

	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}

	if len(result.Prompts) != 0 {
		t.Errorf("expected 0 prompts, got %d", len(result.Prompts))
	}
}

func TestRegisterHandlersPromptsUnavailableWithoutBackend(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Build: "test"}
	srv := New(info, discardLogger, nil)

	if err := RegisterHandlers(srv, nil, info, discardLogger, evalhubclient.DefaultListPageLimit); err != nil {
		t.Fatalf("RegisterHandlers: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	cs := connectClient(t, ctx, srv)

	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "edd_workflow",
		Arguments: map[string]string{"application_type": "rag"},
	})
	if err == nil {
		t.Fatalf("GetPrompt succeeded when it should fail!")
	}
	if (result != nil) && (len(result.Messages) > 0) {
		t.Error("unexpected prompt messages without backend")
	}
}

// --- message validation helpers ---

func assertMessageRoles(t *testing.T, messages []*mcp.PromptMessage) {
	t.Helper()
	for i, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Errorf("message[%d] has invalid role %q, want 'user' or 'assistant'", i, msg.Role)
		}
		if msg.Content == nil {
			t.Errorf("message[%d] has nil content", i)
		}
	}
}

func allMessageText(messages []*mcp.PromptMessage) string {
	var parts []string
	for _, msg := range messages {
		if tc, ok := msg.Content.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
