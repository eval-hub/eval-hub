package collectiondesign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestValidateCollectionSharedRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		candidate api.CollectionConfig
		wantCode  string
		wantPath  string
	}{
		{name: "valid", candidate: validProposal()},
		{name: "missing classification", candidate: api.CollectionConfig{Name: "valid", Benchmarks: []api.CollectionBenchmarkConfig{testBenchmark("p1", "b1")}}, wantCode: issueClassificationRequired, wantPath: "domains"},
		{name: "unknown provider", candidate: proposalWithBenchmarks(testBenchmark("missing", "b1")), wantCode: issueUnknownProvider, wantPath: "benchmarks[0].provider_id"},
		{name: "unknown benchmark", candidate: proposalWithBenchmarks(testBenchmark("p1", "missing")), wantCode: issueUnknownBenchmark, wantPath: "benchmarks[0].id"},
		{name: "duplicate pair", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1"), testBenchmark("p1", "b1")), wantCode: issueDuplicateBenchmark, wantPath: "benchmarks[1]"},
		{name: "same pair with different parameters", candidate: func() api.CollectionConfig {
			first, second := testBenchmark("p1", "b1"), testBenchmark("p1", "b1")
			first.Parameters = map[string]any{"task": "task-a"}
			second.Parameters = map[string]any{"task": "task-b"}
			return proposalWithBenchmarks(first, second)
		}()},
		{name: "unadvertised metric", candidate: proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "missing", false)), wantCode: issueMetricNotAdvertised, wantPath: "benchmarks[0].primary_score.metric"},
		{name: "wrong direction", candidate: proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "accuracy", true)), wantCode: issueDirectionConflict, wantPath: "benchmarks[0].primary_score.lower_is_better"},
		{name: "threshold without metric", candidate: proposalWithBenchmarks(testBenchmarkWithThreshold("p2", "b2", 0.5)), wantCode: issueMetricNotAdvertised, wantPath: "benchmarks[0].pass_criteria.threshold"},
		{name: "nonfinite weight", candidate: proposalWithBenchmarks(testBenchmarkWithWeight("p1", "b1", float32(math.Inf(1)))), wantCode: issueInvalidNumber, wantPath: "benchmarks[0].weight"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			issues, err := ValidateCollection(context.Background(), proposalTestSource(), test.candidate)
			if err != nil {
				t.Fatalf("ValidateCollection() error = %v", err)
			}
			if test.wantCode == "" {
				if len(issues) != 0 {
					t.Fatalf("valid collection has issues: %+v", issues)
				}
			} else if !hasIssue(issues, test.wantCode, test.wantPath) {
				t.Fatalf("issues %+v do not contain (%s, %s)", issues, test.wantCode, test.wantPath)
			}
		})
	}
}

func TestValidateCollectionDirectionConflictReportsExpectedValue(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		defaultLower  bool
		selectedLower bool
		wantMessage   string
	}{
		{name: "provider expects true", defaultLower: true, selectedLower: false, wantMessage: "expected true"},
		{name: "provider expects false", defaultLower: false, selectedLower: true, wantMessage: "expected false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "accuracy", test.selectedLower))
			source := &fakeCatalogSource{providers: []api.ProviderResource{
				testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy", LowerIsBetter: test.defaultLower}, nil),
			}}
			issues, err := ValidateCollection(context.Background(), source, candidate)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, issue := range issues {
				if issue.Code == issueDirectionConflict && issue.Path == "benchmarks[0].primary_score.lower_is_better" {
					found = strings.Contains(issue.Message, test.wantMessage)
				}
			}
			if !found {
				t.Fatalf("direction issue does not contain %q: %+v", test.wantMessage, issues)
			}
		})
	}
}

func TestValidateCollectionDoesNotApplyGenerationOptions(t *testing.T) {
	t.Parallel()
	candidate := proposalWithBenchmarks(testBenchmark("p1", "b1"), testBenchmark("p2", "b2"))
	candidate.CurationOrder = 2
	candidate.Benchmarks[0].URL = "https://server.example/benchmark"
	issues, err := ValidateCollection(context.Background(), proposalTestSource(), candidate)
	if err != nil || len(issues) != 0 {
		t.Fatalf("ValidateCollection() issues=%+v err=%v", issues, err)
	}
	if candidate.CurationOrder != 2 || candidate.Benchmarks[0].URL != "https://server.example/benchmark" {
		t.Fatal("ValidateCollection mutated the caller's config")
	}
	options := proposalTestOptions(t, "p1", 1, StrictnessModerate)
	_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), options, candidate)
	if err != nil || !hasIssue(issues, issueAdminOnlyField, "curation_order") {
		t.Fatalf("admin-only finding: issues=%+v err=%v", issues, err)
	}
	candidate.CurationOrder = 0
	_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), options, candidate)
	if err != nil || !hasIssue(issues, issueBenchmarkCap, "benchmarks") || !hasIssue(issues, issueProviderFilter, "benchmarks[1].provider_id") {
		t.Fatalf("generation-option findings: issues=%+v err=%v", issues, err)
	}
}

func TestValidateCollectionErrorsAndShortCircuitsInvalidFields(t *testing.T) {
	t.Parallel()
	if _, err := ValidateCollection(context.Background(), nil, validProposal()); err == nil || !strings.Contains(err.Error(), "catalog source is nil") {
		t.Fatalf("nil catalog error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateCollection(ctx, proposalTestSource(), validProposal()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}

	providerErr := errors.New("provider catalog unavailable")
	source := &fakeCatalogSource{providerErr: providerErr}
	if _, err := ValidateCollection(context.Background(), source, validProposal()); !errors.Is(err, providerErr) {
		t.Fatalf("provider catalog error = %v", err)
	}
	source = proposalTestSource()
	issues, err := ValidateCollection(context.Background(), source, api.CollectionConfig{})
	if err != nil || !hasIssue(issues, issueRequiredField, "name") || len(source.providerOffsets) != 0 {
		t.Fatalf("invalid config: issues=%+v err=%v provider reads=%v", issues, err, source.providerOffsets)
	}
}

func TestValidateProposalReturnsIndependentPostableCopy(t *testing.T) {
	t.Parallel()
	options := proposalTestOptions(t, "", 3, StrictnessModerate)
	threshold := float32(0.4)
	typedSlice := []int{7, 8}
	typedMap := map[string][]int{"values": {9, 10}}
	custom := map[string]any{"nested": map[string]any{"values": []any{"one", "two"}}, "typed_slice": typedSlice, "typed_map": typedMap, "null": nil}
	candidate := validProposal()
	candidate.Tags = []string{"tag"}
	candidate.Agent = &api.CollectionAgentMetadata{Hints: []string{"hint"}}
	candidate.Custom = &custom
	candidate.Benchmarks[0].URL = "https://server.example/benchmark"
	candidate.Benchmarks[0].PassCriteria = &api.PassCriteria{Threshold: &threshold}
	candidate.Benchmarks[0].Parameters = map[string]any{"nested": map[string]any{"values": []any{1, 2}}}
	candidate.Benchmarks[0].TestDataRef = &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: "data-claim"}}
	source := proposalTestSource()

	got, issues, err := ValidateProposal(context.Background(), source, options, candidate)
	if err != nil || len(issues) != 0 {
		t.Fatalf("ValidateProposal() issues=%+v err=%v", issues, err)
	}
	if got.Benchmarks[0].URL != "" {
		t.Fatalf("server-enriched URL was retained: %q", got.Benchmarks[0].URL)
	}
	registeredValidator, err := validation.NewValidator()
	if err != nil {
		t.Fatalf("initialize request validator: %v", err)
	}
	if err := registeredValidator.Struct(got); err != nil {
		t.Fatalf("returned config does not pass the collection POST validator: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal returned config: %v", err)
	}
	var decoded api.CollectionConfig
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal returned config: %v", err)
	}
	postIssues, err := ValidateCollection(context.Background(), source, decoded)
	if err != nil || len(postIssues) != 0 {
		t.Fatalf("serialized proposal does not pass collection validation: issues=%+v err=%v", postIssues, err)
	}
	if candidate.Benchmarks[0].URL == "" {
		t.Fatal("ValidateProposal mutated the caller's candidate")
	}
	got.Tags[0] = "changed"
	got.Agent.Hints[0] = "changed"
	(*got.Custom)["nested"].(map[string]any)["values"].([]any)[0] = "changed"
	clonedSlice, ok := (*got.Custom)["typed_slice"].([]any)
	if !ok || clonedSlice[0] != float64(7) {
		t.Fatalf("JSON copy did not normalize typed slice: %#v", (*got.Custom)["typed_slice"])
	}
	clonedSlice[0] = float64(100)
	clonedMap, ok := (*got.Custom)["typed_map"].(map[string]any)
	if !ok {
		t.Fatalf("JSON copy did not normalize typed map: %#v", (*got.Custom)["typed_map"])
	}
	clonedMap["values"].([]any)[0] = float64(101)
	got.Benchmarks[0].Parameters["nested"].(map[string]any)["values"].([]any)[0] = 99
	*got.Benchmarks[0].PassCriteria.Threshold = 0.9
	got.Benchmarks[0].TestDataRef.PVC.ClaimName = "changed"
	if candidate.Tags[0] != "tag" || candidate.Agent.Hints[0] != "hint" || custom["nested"].(map[string]any)["values"].([]any)[0] != "one" ||
		typedSlice[0] != 7 || typedMap["values"][0] != 9 || (*got.Custom)["null"] != nil ||
		candidate.Benchmarks[0].Parameters["nested"].(map[string]any)["values"].([]any)[0] != 1 || *candidate.Benchmarks[0].PassCriteria.Threshold != threshold || candidate.Benchmarks[0].TestDataRef.PVC.ClaimName != "data-claim" {
		t.Fatal("returned proposal shares nested mutable data with caller")
	}
}

func TestValidateProposalFindings(t *testing.T) {
	t.Parallel()
	validOptions := proposalTestOptions(t, "p1", 1, StrictnessModerate)
	tests := []struct {
		name      string
		candidate api.CollectionConfig
		options   Options
		wantCode  string
		wantPath  string
	}{
		{name: "missing required classification", candidate: api.CollectionConfig{Name: "valid", Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}}}, options: validOptions, wantCode: issueClassificationRequired, wantPath: "domains"},
		{name: "empty benchmark selection", candidate: api.CollectionConfig{Name: "valid", Domains: []string{"reasoning"}}, options: validOptions, wantCode: issueSchemaInvalid, wantPath: "benchmarks"},
		{name: "empty name", candidate: api.CollectionConfig{Category: "reasoning", Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}}}, options: validOptions, wantCode: issueRequiredField, wantPath: "name"},
		{name: "missing benchmark ID uses API path", candidate: proposalWithBenchmarks(testBenchmark("p1", "")), options: validOptions, wantCode: issueRequiredField, wantPath: "benchmarks[0].id"},
		{name: "unknown provider", candidate: proposalWithBenchmarks(testBenchmark("missing", "b1")), options: validOptions, wantCode: issueUnknownProvider, wantPath: "benchmarks[0].provider_id"},
		{name: "unknown benchmark pair", candidate: proposalWithBenchmarks(testBenchmark("p1", "missing")), options: validOptions, wantCode: issueUnknownBenchmark, wantPath: "benchmarks[0].id"},
		{name: "duplicate pair", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1"), testBenchmark("p1", "b1")), options: proposalTestOptions(t, "", 3, StrictnessModerate), wantCode: issueDuplicateBenchmark, wantPath: "benchmarks[1]"},
		{name: "provider filter violation", candidate: proposalWithBenchmarks(testBenchmark("p2", "b2")), options: validOptions, wantCode: issueProviderFilter, wantPath: "benchmarks[0].provider_id"},
		{name: "unknown filtered provider", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1")), options: proposalTestOptions(t, "p1,missing", 3, StrictnessModerate), wantCode: issueUnknownProvider, wantPath: "provider_filter"},
		{name: "comma only provider filter", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1")), options: proposalTestOptions(t, ", ,", 3, StrictnessModerate), wantCode: issueInvalidOptions, wantPath: "provider_filter"},
		{name: "benchmark cap", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1"), testBenchmark("p2", "b2")), options: validOptions, wantCode: issueBenchmarkCap, wantPath: "benchmarks"},
		{name: "unadvertised metric", candidate: proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "unadvertised", false)), options: validOptions, wantCode: issueMetricNotAdvertised, wantPath: "benchmarks[0].primary_score.metric"},
		{name: "conflicting direction", candidate: proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "accuracy", true)), options: validOptions, wantCode: issueDirectionConflict, wantPath: "benchmarks[0].primary_score.lower_is_better"},
		{name: "threshold without metric", candidate: proposalWithBenchmarks(testBenchmarkWithThreshold("p2", "b2", 0.5)), options: validOptions, wantCode: issueMetricNotAdvertised, wantPath: "benchmarks[0].pass_criteria.threshold"},
		{name: "admin curation field", candidate: func() api.CollectionConfig { c := validProposal(); c.CurationOrder = 2; return c }(), options: validOptions, wantCode: issueAdminOnlyField, wantPath: "curation_order"},
		{name: "server-managed test data identity", candidate: func() api.CollectionConfig {
			c := validProposal()
			c.Benchmarks[0].TestDataRef = &api.TestDataRef{Git: &api.GitTestDataRef{URL: "https://example.com/data.git", Ref: "0123456789abcdef0123456789abcdef01234567"}, ResolvedSHA: "0123456789abcdef0123456789abcdef01234567"}
			return c
		}(), options: validOptions, wantCode: issueAdminOnlyField, wantPath: "benchmarks[0].test_data_ref.resolved_sha"},
		{name: "non finite threshold", candidate: proposalWithBenchmarks(testBenchmarkWithThreshold("p1", "b1", float32(math.NaN()))), options: validOptions, wantCode: issueInvalidNumber, wantPath: "benchmarks[0].pass_criteria.threshold"},
		{name: "non finite weight", candidate: proposalWithBenchmarks(testBenchmarkWithWeight("p1", "b1", float32(math.Inf(1)))), options: validOptions, wantCode: issueInvalidNumber, wantPath: "benchmarks[0].weight"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, issues, err := ValidateProposal(context.Background(), proposalTestSource(), test.options, test.candidate)
			if err != nil {
				t.Fatalf("ValidateProposal() error = %v", err)
			}
			if !hasIssue(issues, test.wantCode, test.wantPath) {
				t.Fatalf("issues %+v do not contain (%s, %s)", issues, test.wantCode, test.wantPath)
			}
			for i := 1; i < len(issues); i++ {
				if issues[i-1].Path > issues[i].Path {
					t.Fatalf("issues are not sorted deterministically: %+v", issues)
				}
			}
		})
	}
}

func TestValidateProposalOptionsCatalogErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	if got, issues, err := ValidateProposal(context.Background(), nil, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); err == nil || !strings.Contains(err.Error(), "catalog source is nil") || !reflect.DeepEqual(got, api.CollectionConfig{}) || issues != nil {
		t.Fatalf("nil source validation error = %v", err)
	}
	if got, decisions, err := CalibrateProposal(context.Background(), nil, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); err == nil || !strings.Contains(err.Error(), "catalog source is nil") || !reflect.DeepEqual(got, api.CollectionConfig{}) || decisions != nil {
		t.Fatalf("nil source calibration error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, issues, err := ValidateProposal(ctx, proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, api.CollectionConfig{}) || issues != nil {
		t.Fatalf("pre-canceled validation error = %v", err)
	}

	invalidOptions := Options{Goal: "goal", MaxBenchmarks: -1, Strictness: "invalid"}
	_, issues, err := ValidateProposal(context.Background(), proposalTestSource(), invalidOptions, validProposal())
	if err != nil || !hasIssue(issues, issueInvalidOptions, "max_benchmarks") || !hasIssue(issues, issueInvalidOptions, "strictness") {
		t.Fatalf("invalid options: issues=%+v err=%v", issues, err)
	}
	invalidGoal := Options{MaxBenchmarks: 2, Strictness: StrictnessModerate}
	_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), invalidGoal, validProposal())
	if err != nil || len(issues) != 1 || issues[0].Path != "evaluation_goal" {
		t.Fatalf("invalid goal: issues=%+v err=%v", issues, err)
	}
	for _, strictness := range []string{"extreme", "max_benchmarks", "evaluation_goal"} {
		invalidStrictness := Options{Goal: "goal", MaxBenchmarks: 2, Strictness: strictness}
		_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), invalidStrictness, validProposal())
		if err != nil || !hasIssue(issues, issueInvalidOptions, "strictness") {
			t.Fatalf("invalid strictness %q: issues=%+v err=%v", strictness, issues, err)
		}
	}

	providerErr := errors.New("catalog unavailable")
	source := &fakeCatalogSource{providerErr: providerErr}
	got, issues, err := ValidateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if len(issues) != 0 || !errors.Is(err, providerErr) || !strings.Contains(err.Error(), "fetching provider catalog") || !reflect.DeepEqual(got, api.CollectionConfig{}) {
		t.Fatalf("provider failure: issues=%+v err=%v", issues, err)
	}

	duplicateProvider := &fakeCatalogSource{providers: []api.ProviderResource{{Resource: api.Resource{ID: "p1"}}, {Resource: api.Resource{ID: "p1"}}}}
	_, _, err = ValidateProposal(context.Background(), duplicateProvider, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if err == nil || !strings.Contains(err.Error(), "duplicate provider id") {
		t.Fatalf("duplicate provider catalog error = %v", err)
	}

	duplicateBenchmark := &fakeCatalogSource{providers: []api.ProviderResource{{Resource: api.Resource{ID: "p1"}, ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{ID: "b1"}, {ID: "b1"}}}}}}
	_, _, err = ValidateProposal(context.Background(), duplicateBenchmark, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if err == nil || !strings.Contains(err.Error(), "duplicate benchmark pair") {
		t.Fatalf("duplicate benchmark catalog error = %v", err)
	}
}

func TestValidateProposalNormalizesHandBuiltOptions(t *testing.T) {
	t.Parallel()
	options := Options{
		Goal:           "  goal  ",
		ProviderFilter: " p1, p1, , ",
		ProviderIDs:    map[string]struct{}{"stale": {}},
		// Zero max and empty strictness use the same defaults as ParseOptions.
	}
	_, issues, err := ValidateProposal(context.Background(), proposalTestSource(), options, validProposal())
	if err != nil || len(issues) != 0 {
		t.Fatalf("hand-built options were not normalized consistently: issues=%+v err=%v", issues, err)
	}
}

func TestValidateProposalRejectsNonJSONMetadata(t *testing.T) {
	t.Parallel()
	options := proposalTestOptions(t, "", 2, StrictnessModerate)

	candidate := validProposal()
	candidate.Benchmarks[0].Parameters = map[string]any{"not_json": math.NaN()}
	source := proposalTestSource()
	if _, _, err := ValidateProposal(context.Background(), source, options, candidate); err == nil || !strings.Contains(err.Error(), "benchmarks[0].parameters") {
		t.Fatalf("non-JSON parameters error = %v", err)
	}
	if len(source.providerOffsets) != 0 {
		t.Fatalf("catalog was read after parameter copy failed: offsets=%v", source.providerOffsets)
	}

	custom := map[string]any{"not_json": math.Inf(1)}
	candidate = validProposal()
	candidate.Custom = &custom
	if _, _, err := CalibrateProposal(context.Background(), proposalTestSource(), options, candidate); err == nil || !strings.Contains(err.Error(), "custom") {
		t.Fatalf("non-JSON custom metadata error = %v", err)
	}
}

func TestValidateProposalPaginatesCatalogAndStopsAfterCancellation(t *testing.T) {
	t.Parallel()
	providers := make([]api.ProviderResource, 201)
	providers[0] = testProvider("p1", "b1", []string{"accuracy"}, nil, nil)
	for i := 1; i < len(providers); i++ {
		providers[i] = api.ProviderResource{Resource: api.Resource{ID: fmt.Sprintf("provider-%03d", i)}}
	}
	options := proposalTestOptions(t, "", 2, StrictnessModerate)

	source := &fakeCatalogSource{providers: providers}
	if _, issues, err := ValidateProposal(context.Background(), source, options, validProposal()); err != nil || len(issues) != 0 {
		t.Fatalf("paginated validation: issues=%+v err=%v", issues, err)
	}
	if !reflect.DeepEqual(source.providerOffsets, []int{0, 200}) {
		t.Fatalf("provider pages read at offsets %v", source.providerOffsets)
	}

	ctx, cancel := context.WithCancel(context.Background())
	source = &fakeCatalogSource{providers: providers, afterProviderPage: cancel}
	_, _, err := ValidateProposal(ctx, source, options, validProposal())
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(source.providerOffsets, []int{0}) {
		t.Fatalf("canceled pagination: err=%v offsets=%v", err, source.providerOffsets)
	}
}

func TestCalibrateProposalStrictnessAndPrimaryScoreDefault(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		lower      bool
		strictness string
		want       float32
	}{
		{name: "higher lenient", strictness: StrictnessLenient, want: 0.2},
		{name: "higher moderate even median", strictness: StrictnessModerate, want: 0.5},
		{name: "higher strict", strictness: StrictnessStrict, want: 0.8},
		{name: "lower lenient", lower: true, strictness: StrictnessLenient, want: 0.8},
		{name: "lower moderate even median", lower: true, strictness: StrictnessModerate, want: 0.5},
		{name: "lower strict", lower: true, strictness: StrictnessStrict, want: 0.2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy", LowerIsBetter: test.lower}, nil)
			candidate := validProposal()
			candidate.Benchmarks[0].PrimaryScore = nil
			source := &fakeCatalogSource{
				providers: []api.ProviderResource{provider},
				collections: []api.CollectionResource{
					systemReference("ref-z", testBenchmarkWithScoreAndThreshold("p1", "b1", "accuracy", test.lower, 0.8)),
					systemReference("ref-a", testBenchmarkWithScoreAndThreshold("p1", "b1", "accuracy", test.lower, 0.2)),
				},
			}
			got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 3, test.strictness), candidate)
			if err != nil {
				t.Fatalf("CalibrateProposal() error = %v", err)
			}
			if got.Benchmarks[0].PrimaryScore == nil || got.Benchmarks[0].PrimaryScore.Metric != "accuracy" || got.Benchmarks[0].PrimaryScore.LowerIsBetter != test.lower {
				t.Fatalf("provider primary score was not copied: %+v", got.Benchmarks[0].PrimaryScore)
			}
			if got.Benchmarks[0].PassCriteria == nil || got.Benchmarks[0].PassCriteria.Threshold == nil || *got.Benchmarks[0].PassCriteria.Threshold != test.want {
				t.Fatalf("threshold = %+v, want %v", got.Benchmarks[0].PassCriteria, test.want)
			}
			if len(decisions) != 3 || decisions[0].Field != "benchmarks[0].primary_score" || decisions[1].Field != "benchmarks[0].weight" || decisions[2].Field != "benchmarks[0].pass_criteria.threshold" {
				t.Fatalf("unexpected deterministic decisions: %+v", decisions)
			}
			if len(decisions[2].ReferenceCollectionIDs) != 2 || decisions[2].ReferenceCollectionIDs[0] != "ref-a" || decisions[2].ReferenceCollectionIDs[1] != "ref-z" {
				t.Fatalf("reference provenance is not sorted: %+v", decisions[2])
			}
		})
	}
}

func TestCalibrateProposalMedianAndUnnormalizedThresholds(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"latency_ms"}, &api.PrimaryScore{Metric: "latency_ms", LowerIsBetter: true}, nil)
	source := &fakeCatalogSource{
		providers: []api.ProviderResource{provider},
		collections: []api.CollectionResource{
			systemReference("one", testBenchmarkWithScoreAndThreshold("p1", "b1", "latency_ms", true, -10)),
			systemReference("two", testBenchmarkWithScoreAndThreshold("p1", "b1", "latency_ms", true, 10)),
			systemReference("three", testBenchmarkWithScoreAndThreshold("p1", "b1", "latency_ms", true, 50)),
		},
	}
	got, _, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 3, StrictnessModerate), validProposal())
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PassCriteria == nil || *got.Benchmarks[0].PassCriteria.Threshold != 10 {
		t.Fatalf("odd median should be preserved without normalized-scale clamping: %+v", got.Benchmarks[0].PassCriteria)
	}
}

func TestCalibrateProposalPreservesExplicitValuesAndUsesOnlyComparableSystemReferences(t *testing.T) {
	t.Parallel()
	candidate := validProposal()
	candidate.Benchmarks[0].Weight = 4
	threshold := float32(0.91)
	candidate.Benchmarks[0].PassCriteria = &api.PassCriteria{Threshold: &threshold}
	candidate.Benchmarks[0].PrimaryScore = &api.PrimaryScore{Metric: "accuracy", LowerIsBetter: false}
	source := &fakeCatalogSource{
		providers: []api.ProviderResource{testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)},
		collections: []api.CollectionResource{
			systemReference("same", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 2, "accuracy", false, 0.1)),
			systemReference("different-metric", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "loss", false, 0.2)),
			{Resource: api.Resource{ID: "tenant-ref", Owner: "alice"}, CollectionConfig: api.CollectionConfig{Benchmarks: []api.CollectionBenchmarkConfig{testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 8, "accuracy", false, 0.3)}}},
			systemReference("other-benchmark", testBenchmark("p1", "b2")),
		},
	}
	got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 3, StrictnessStrict), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].Weight != 4 || *got.Benchmarks[0].PassCriteria.Threshold != threshold || got.Benchmarks[0].PrimaryScore.Metric != "accuracy" {
		t.Fatalf("explicit values were overwritten: %+v", got.Benchmarks[0])
	}
	if candidate.Benchmarks[0].URL != "" {
		t.Fatal("calibration mutated caller candidate")
	}
	for _, field := range []string{"benchmarks[0].primary_score", "benchmarks[0].weight", "benchmarks[0].pass_criteria.threshold"} {
		if !hasDecision(decisions, field, "preserved") {
			t.Errorf("missing explicit-value decision for %s: %+v", field, decisions)
		}
	}
}

func TestCalibrateProposalRequiresMatchingParametersAndTestDataForThresholds(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)
	candidate := validProposal()
	candidate.Benchmarks[0].Weight = 1
	candidate.Benchmarks[0].Parameters = map[string]any{"task": "candidate"}
	candidate.Benchmarks[0].TestDataRef = &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: "candidate-data", SubPath: "dataset"}}

	reference := func(id, task, claim, subPath string, threshold float32) api.CollectionResource {
		benchmark := testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "accuracy", false, threshold)
		benchmark.Parameters = map[string]any{"task": task}
		benchmark.TestDataRef = &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: claim, SubPath: subPath}}
		return systemReference(id, benchmark)
	}

	source := &fakeCatalogSource{
		providers: []api.ProviderResource{provider},
		collections: []api.CollectionResource{
			reference("different-parameters", "other-task", "candidate-data", "dataset", 0.99),
			reference("different-test-data", "candidate", "other-data", "dataset", 0.1),
			reference("different-subpath", "candidate", "candidate-data", "other-dataset", 0.2),
		},
	}
	got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessStrict), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PassCriteria != nil {
		t.Fatalf("threshold was inferred from references with different parameters or PVC locations: %+v", got.Benchmarks[0].PassCriteria)
	}
	if !hasDecision(decisions, "benchmarks[0].pass_criteria.threshold", "no system references match the candidate parameters and test-data identity") {
		t.Fatalf("missing explanation for references that do not match the candidate: %+v", decisions)
	}
}

func TestCalibrateProposalUsesMatchingPVCSourceDescriptorWithoutResolvedSHA(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)
	candidate := validProposal()
	candidate.Benchmarks[0].TestDataRef = &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: "calibration-data", SubPath: "dataset"}}
	reference := testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "accuracy", false, 0.73)
	reference.TestDataRef = &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: "calibration-data", SubPath: "dataset"}, ResolvedSHA: "0123456789abcdef0123456789abcdef01234567"}
	source := &fakeCatalogSource{
		providers:   []api.ProviderResource{provider},
		collections: []api.CollectionResource{systemReference("same-pvc-source", reference)},
	}

	got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PassCriteria == nil || got.Benchmarks[0].PassCriteria.Threshold == nil || *got.Benchmarks[0].PassCriteria.Threshold != 0.73 {
		t.Fatalf("threshold from matching PVC source descriptor was not used: %+v", got.Benchmarks[0].PassCriteria)
	}
	if !reflect.DeepEqual(decisionIDs(decisions, "benchmarks[0].pass_criteria.threshold"), []string{"same-pvc-source"}) {
		t.Fatalf("unexpected threshold provenance: %+v", decisions)
	}
}

func TestCalibrateProposalUsesImmutableGitRevisionWithoutResolvedSHA(t *testing.T) {
	t.Parallel()
	const commit = "0123456789abcdef0123456789abcdef01234567"
	candidate := validProposal()
	candidate.Benchmarks[0].TestDataRef = &api.TestDataRef{Git: &api.GitTestDataRef{
		URL: "https://example.com/data.git", Ref: commit, SubPath: "dataset",
	}}
	reference := testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "accuracy", false, 0.73)
	reference.TestDataRef = &api.TestDataRef{Git: &api.GitTestDataRef{
		URL: "https://example.com/data.git", Ref: commit, SubPath: "dataset",
	}}
	source := &fakeCatalogSource{
		providers:   []api.ProviderResource{testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)},
		collections: []api.CollectionResource{systemReference("pinned-data", reference)},
	}

	got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PassCriteria == nil || got.Benchmarks[0].PassCriteria.Threshold == nil || *got.Benchmarks[0].PassCriteria.Threshold != 0.73 {
		t.Fatalf("threshold from identical immutable Git data was not used: %+v", got.Benchmarks[0].PassCriteria)
	}
	if !reflect.DeepEqual(decisionIDs(decisions, "benchmarks[0].pass_criteria.threshold"), []string{"pinned-data"}) {
		t.Fatalf("unexpected threshold provenance: %+v", decisions)
	}
}

func TestSameTestDataIdentityUsesStableSourceDescriptors(t *testing.T) {
	t.Parallel()
	const commit = "0123456789abcdef0123456789abcdef01234567"
	gitPinned := &api.TestDataRef{Git: &api.GitTestDataRef{URL: "https://example.com/data.git", Ref: commit, SubPath: "set"}}
	hfPinned := &api.TestDataRef{HF: &api.HFTestDataRef{RepoID: "org/data", Revision: commit, SubPath: "set"}}
	gitBranch := &api.TestDataRef{Git: &api.GitTestDataRef{URL: "https://example.com/data.git", Ref: "main", SubPath: "set"}}
	hfBranch := &api.TestDataRef{HF: &api.HFTestDataRef{RepoID: "org/data", Revision: "main", SubPath: "set"}}
	pvc := &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: "data", SubPath: "set"}}
	s3 := &api.TestDataRef{S3: &api.S3TestDataRef{Bucket: "bucket", Key: "set/data.jsonl", SecretRef: "credentials"}}

	for _, test := range []struct {
		name  string
		left  *api.TestDataRef
		right *api.TestDataRef
		want  bool
	}{
		{name: "both absent", want: true},
		{name: "both same git commit", left: gitPinned, right: gitPinned, want: true},
		{name: "both same Hugging Face commit", left: hfPinned, right: hfPinned, want: true},
		{name: "immutable commit with one recorded resolved identity", left: &api.TestDataRef{Git: gitPinned.Git, ResolvedSHA: commit}, right: gitPinned, want: true},
		{name: "mutable git branch", left: gitBranch, right: gitBranch},
		{name: "mutable Hugging Face revision", left: hfBranch, right: hfBranch},
		{name: "same PVC location", left: pvc, right: pvc, want: true},
		{name: "same S3 object location", left: s3, right: s3, want: true},
		{name: "same PVC location with one resolved identity", left: &api.TestDataRef{PVC: pvc.PVC, ResolvedSHA: commit}, right: pvc, want: true},
		{name: "different immutable revision", left: gitPinned, right: &api.TestDataRef{Git: &api.GitTestDataRef{URL: "https://example.com/data.git", Ref: strings.Repeat("f", 40), SubPath: "set"}}},
		{name: "different credential refs do not change HF dataset identity", left: &api.TestDataRef{HF: &api.HFTestDataRef{RepoID: "org/data", Revision: commit, SubPath: "set", SecretRef: "secret-a"}}, right: &api.TestDataRef{HF: &api.HFTestDataRef{RepoID: "org/data", Revision: commit, SubPath: "set", SecretRef: "secret-b"}}, want: true},
		{name: "mutable git branch with matching resolved identity", left: &api.TestDataRef{Git: gitBranch.Git, ResolvedSHA: commit}, right: &api.TestDataRef{Git: gitBranch.Git, ResolvedSHA: commit}, want: true},
		{name: "matching resolved identity", left: &api.TestDataRef{PVC: pvc.PVC, ResolvedSHA: commit}, right: &api.TestDataRef{PVC: pvc.PVC, ResolvedSHA: commit}, want: true},
		{name: "different resolved identity", left: &api.TestDataRef{PVC: pvc.PVC, ResolvedSHA: commit}, right: &api.TestDataRef{PVC: pvc.PVC, ResolvedSHA: strings.Repeat("f", 40)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sameTestDataIdentity(test.left, test.right); got != test.want {
				t.Fatalf("sameTestDataIdentity() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCalibrateProposalPaginatesReferencesAndDoesNotInventDirection(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)
	collections := make([]api.CollectionResource, 201)
	for i := 0; i < len(collections)-1; i++ {
		collections[i] = api.CollectionResource{Resource: api.Resource{ID: fmt.Sprintf("tenant-%03d", i), Owner: "alice"}}
	}
	reference := systemReference("last-reference", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 2, "accuracy", false, 0.7))
	collectionThreshold := float32(0.99)
	reference.PassCriteria = &api.PassCriteria{Threshold: &collectionThreshold}
	collections[len(collections)-1] = reference
	source := &fakeCatalogSource{providers: []api.ProviderResource{provider}, collections: collections}
	got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.collectionOffsets, []int{0, 200}) {
		t.Fatalf("collection pages read at offsets %v", source.collectionOffsets)
	}
	if got.Benchmarks[0].Weight != 2 || got.Benchmarks[0].PassCriteria == nil || *got.Benchmarks[0].PassCriteria.Threshold != 0.7 {
		t.Fatalf("single visible system reference was not used: %+v", got.Benchmarks[0])
	}
	if got.PassCriteria != nil {
		t.Fatalf("collection-level threshold was incorrectly inferred: %+v", got.PassCriteria)
	}
	if !reflect.DeepEqual(decisionIDs(decisions, "benchmarks[0].pass_criteria.threshold"), []string{"last-reference"}) {
		t.Fatalf("threshold provenance = %+v", decisions)
	}

	provider.ProviderConfig.Benchmarks[0].PrimaryScore = nil
	withoutDirection := &fakeCatalogSource{
		providers: []api.ProviderResource{provider},
		collections: []api.CollectionResource{systemReference("no-trusted-direction",
			testBenchmarkWithThreshold("p1", "b1", 0.9))},
	}
	candidate := validProposal()
	candidate.Benchmarks[0].PrimaryScore = &api.PrimaryScore{Metric: "accuracy"}
	got, decisions, err = CalibrateProposal(context.Background(), withoutDirection, proposalTestOptions(t, "", 2, StrictnessModerate), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PassCriteria != nil {
		t.Fatalf("calibration invented a threshold without a provider-advertised direction: %+v", got.Benchmarks[0].PassCriteria)
	}
	if !hasDecision(decisions, "benchmarks[0].pass_criteria.threshold", "references do not declare the selected primary-score metric and direction") {
		t.Fatalf("missing no-direction explanation: %+v", decisions)
	}
}

func TestCalibrateProposalIgnoresInvalidProviderDefaultScore(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "missing-metric"}, nil)
	source := &fakeCatalogSource{
		providers: []api.ProviderResource{provider},
		collections: []api.CollectionResource{systemReference("reference",
			testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "missing-metric", false, 0.8))},
	}
	got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PrimaryScore != nil || got.Benchmarks[0].PassCriteria != nil {
		t.Fatalf("unadvertised provider default was used: %+v", got.Benchmarks[0])
	}
	if !hasDecision(decisions, "benchmarks[0].primary_score", "provider-advertised default metric \"missing-metric\" is not included") || !hasDecision(decisions, "benchmarks[0].pass_criteria.threshold", "provider-advertised default metric \"missing-metric\" is not included") {
		t.Fatalf("missing reasons for invalid provider default: %+v", decisions)
	}
}

func TestCalibrateProposalDistinguishesMissingProviderDefault(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, nil, nil)
	source := &fakeCatalogSource{
		providers:   []api.ProviderResource{provider},
		collections: []api.CollectionResource{systemReference("reference", testBenchmarkWithThreshold("p1", "b1", 0.8))},
	}
	_, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDecision(decisions, "benchmarks[0].primary_score", "provider does not advertise a default primary score") {
		t.Fatalf("missing distinct no-default explanation: %+v", decisions)
	}
}

func TestInspectCustomAllowsExplicitMetricAndCalibratesRepeatedReferences(t *testing.T) {
	t.Parallel()
	provider := testProvider("inspect", "inspect/custom", nil, nil, nil)
	candidateBenchmark := testBenchmarkWithScore("inspect", "inspect/custom", "accuracy", false)
	candidateBenchmark.Parameters = map[string]any{"task": "inspect_evals/gsm8k", "task_args": map[string]any{"fewshot": float64(5)}}
	candidate := proposalWithBenchmarks(candidateBenchmark)
	options := proposalTestOptions(t, "", 3, StrictnessModerate)

	firstReference := testBenchmarkWithWeightScoreAndThreshold("inspect", "inspect/custom", 1, "accuracy", false, 0.2)
	firstReference.Parameters = map[string]any{"task": "inspect_evals/gsm8k", "task_args": map[string]any{"fewshot": 0}}
	matchingReference := testBenchmarkWithWeightScoreAndThreshold("inspect", "inspect/custom", 1, "accuracy", false, 0.9)
	matchingReference.Parameters = map[string]any{"task": "inspect_evals/gsm8k", "task_args": map[string]any{"fewshot": float64(5)}}
	source := &fakeCatalogSource{
		providers: []api.ProviderResource{provider},
		collections: []api.CollectionResource{
			systemReference("evaluate-model-accuracy", firstReference, matchingReference),
		},
	}

	_, issues, err := ValidateProposal(context.Background(), source, options, candidate)
	if err != nil || len(issues) != 0 {
		t.Fatalf("explicit metric on provider with unknown metrics was rejected: issues=%+v err=%v", issues, err)
	}

	got, decisions, err := CalibrateProposal(context.Background(), source, options, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Benchmarks[0].PassCriteria == nil || got.Benchmarks[0].PassCriteria.Threshold == nil || *got.Benchmarks[0].PassCriteria.Threshold != 0.9 {
		t.Fatalf("matching repeated reference did not calibrate threshold: %+v", got.Benchmarks[0].PassCriteria)
	}
	if !reflect.DeepEqual(decisionIDs(decisions, "benchmarks[0].pass_criteria.threshold"), []string{"evaluate-model-accuracy"}) {
		t.Fatalf("unexpected reference provenance: %+v", decisions)
	}
}

func TestCalibrateProposalMissingEvidenceAndWeightConsensus(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)
	for _, test := range []struct {
		name                string
		refs                []api.CollectionResource
		wantWeight          float32
		wantThreshold       bool
		wantWeightReason    string
		wantThresholdReason string
	}{
		{name: "no references", wantWeightReason: "no matching system references", wantThresholdReason: "no matching system references"},
		{name: "agreed weights but no thresholds", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.NaN()))),
			systemReference("b", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.Inf(1)))),
		}, wantWeight: 3, wantThresholdReason: "no finite thresholds"},
		{name: "conflicting weights", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.NaN()))),
			systemReference("b", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 4, "accuracy", false, float32(math.NaN()))),
		}, wantWeightReason: "consensus positive weight", wantThresholdReason: "no finite thresholds"},
		{name: "omitted reference weight prevents consensus", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.NaN()))),
			systemReference("b", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 0, "accuracy", false, float32(math.NaN()))),
		}, wantWeightReason: "consensus positive weight", wantThresholdReason: "no finite thresholds"},
		{name: "direction mismatches", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "accuracy", true, 0.4)),
		}, wantWeight: 1, wantThresholdReason: "no finite thresholds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := validProposal()
			source := &fakeCatalogSource{providers: []api.ProviderResource{provider}, collections: test.refs}
			got, decisions, err := CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 3, StrictnessModerate), candidate)
			if err != nil {
				t.Fatal(err)
			}
			if got.Benchmarks[0].Weight != test.wantWeight {
				t.Fatalf("weight=%v, want %v", got.Benchmarks[0].Weight, test.wantWeight)
			}
			if (got.Benchmarks[0].PassCriteria != nil) != test.wantThreshold {
				t.Fatalf("threshold presence=%t, want %t", got.Benchmarks[0].PassCriteria != nil, test.wantThreshold)
			}
			if test.wantWeightReason != "" && !hasDecision(decisions, "benchmarks[0].weight", test.wantWeightReason) {
				t.Fatalf("expected weight reason containing %q in %+v", test.wantWeightReason, decisions)
			}
			if test.wantThresholdReason != "" && !hasDecision(decisions, "benchmarks[0].pass_criteria.threshold", test.wantThresholdReason) {
				t.Fatalf("expected threshold reason containing %q in %+v", test.wantThresholdReason, decisions)
			}
		})
	}
}

func TestCalibrateProposalErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, decisions, err := CalibrateProposal(ctx, proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, api.CollectionConfig{}) || decisions != nil {
		t.Fatalf("pre-canceled calibration error = %v", err)
	}

	got, decisions, err := CalibrateProposal(context.Background(), proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), api.CollectionConfig{})
	if err == nil || !strings.Contains(err.Error(), issueClassificationRequired) || !reflect.DeepEqual(got, api.CollectionConfig{}) || decisions != nil {
		t.Fatalf("invalid proposal error = %v", err)
	}

	collectionErr := errors.New("references unavailable")
	source := &fakeCatalogSource{providers: proposalTestSource().providers, collectionErr: collectionErr}
	got, decisions, err = CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if !errors.Is(err, collectionErr) || !strings.Contains(err.Error(), "fetching calibration references") || !reflect.DeepEqual(got, api.CollectionConfig{}) || decisions != nil {
		t.Fatalf("reference read error = %v", err)
	}

	badCandidate := validProposal()
	badCandidate.Benchmarks[0].PrimaryScore = &api.PrimaryScore{Metric: "not-advertised"}
	got, decisions, err = CalibrateProposal(context.Background(), proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), badCandidate)
	if err == nil || !strings.Contains(err.Error(), issueMetricNotAdvertised) || !reflect.DeepEqual(got, api.CollectionConfig{}) || decisions != nil {
		t.Fatalf("catalog-invalid proposal error = %v", err)
	}
}

func TestProposalSchemaNestedAndCollectionThresholdValidation(t *testing.T) {
	t.Parallel()
	candidate := validProposal()
	candidate.Benchmarks[0].PrimaryScore = &api.PrimaryScore{}
	candidate.Benchmarks[0].PassCriteria = &api.PassCriteria{}
	_, issues, err := ValidateProposal(context.Background(), proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssue(issues, issueRequiredField, "benchmarks[0].primary_score.metric") {
		t.Fatalf("missing nested metric issue absent: %+v", issues)
	}
	if !hasIssue(issues, issueRequiredField, "benchmarks[0].pass_criteria.threshold") {
		t.Fatalf("missing nested threshold issue absent: %+v", issues)
	}

	candidate = validProposal()
	collectionThreshold := float32(math.Inf(-1))
	candidate.PassCriteria = &api.PassCriteria{Threshold: &collectionThreshold}
	_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), candidate)
	if err != nil || !hasIssue(issues, issueInvalidNumber, "pass_criteria.threshold") {
		t.Fatalf("non-finite collection threshold: issues=%+v err=%v", issues, err)
	}
}

func TestProposalFormattingAndReferenceHelpers(t *testing.T) {
	t.Parallel()

	if got := jsonPath("CollectionConfig.Name"); got != "Name" {
		t.Fatalf("jsonPath() = %q, want Name", got)
	}
	if got := jsonPath("Name"); got != "Name" {
		t.Fatalf("jsonPath() without a namespace = %q, want Name", got)
	}
	if got := jsonPath("CollectionConfig.benchmarks[0].Ref.id"); got != "benchmarks[0].id" {
		t.Fatalf("jsonPath() for an embedded Ref field = %q, want benchmarks[0].id", got)
	}

	issues := []Issue{
		{Path: "benchmarks[0]", Code: "schema_invalid", Message: "z"},
		{Path: "benchmarks[0]", Code: "schema_invalid", Message: "a"},
		{Path: "benchmarks[0]", Code: "required_field", Message: "missing"},
	}
	sorted := sortedIssues(issues)
	if sorted[0].Code != "required_field" || sorted[1].Message != "a" || sorted[2].Message != "z" {
		t.Fatalf("sortedIssues() order = %+v", sorted)
	}

	if got := uniqueSorted(nil); got != nil {
		t.Fatalf("uniqueSorted(nil) = %v, want nil", got)
	}
	if got := uniqueSorted([]string{"b", "a", "b", "c", "a"}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("uniqueSorted() = %v, want [a b c]", got)
	}

	key := benchmarkKey{providerID: "p1", benchmarkID: "b1"}
	matching := testBenchmark("p1", "b1")
	references := []api.CollectionResource{
		systemReference("same", matching),
		systemReference("same", matching),
		systemReference("ambiguous", matching, matching),
		{Resource: api.Resource{ID: "tenant", Owner: "alice"}, CollectionConfig: api.CollectionConfig{Benchmarks: []api.CollectionBenchmarkConfig{matching}}},
		systemReference("unrelated", testBenchmark("p1", "other")),
	}
	got := matchingReferenceBenchmarks(references, key)
	wantCollectionIDs := []string{"ambiguous", "ambiguous", "same", "same"}
	if len(got) != len(wantCollectionIDs) {
		t.Fatalf("matchingReferenceBenchmarks() returned %d references, want %d: %+v", len(got), len(wantCollectionIDs), got)
	}
	for i, wantID := range wantCollectionIDs {
		if got[i].collectionID != wantID {
			t.Fatalf("matchingReferenceBenchmarks()[%d].collectionID = %q, want %q", i, got[i].collectionID, wantID)
		}
	}
}

func TestCollectionValidationFlattensEmbeddedBenchmarkIDPath(t *testing.T) {
	t.Parallel()
	issues, err := validateCollectionConfig(api.CollectionConfig{
		Name:    "valid",
		Domains: []string{"reasoning"},
		Benchmarks: []api.CollectionBenchmarkConfig{{
			Ref:        api.Ref{ID: ""},
			ProviderID: "p1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssue(issues, issueRequiredField, "benchmarks[0].id") {
		t.Fatalf("embedded benchmark ID should use its API path: %+v", issues)
	}
}

func TestCalibratedThresholdFiltersIncomparableReferenceEvidence(t *testing.T) {
	t.Parallel()

	provider := api.BenchmarkResource{
		Metrics:      []string{"accuracy", "loss"},
		PrimaryScore: &api.PrimaryScore{Metric: "accuracy"},
	}
	refs := []referenceBenchmark{
		{collectionID: "default-score", benchmark: testBenchmarkWithThreshold("p1", "b1", 0.4)},
		{collectionID: "different-score", benchmark: testBenchmarkWithScoreAndThreshold("p1", "b1", "loss", false, 0.6)},
		{collectionID: "missing-pass-criteria", benchmark: testBenchmarkWithScore("p1", "b1", "accuracy", false)},
		{collectionID: "missing-threshold", benchmark: func() api.CollectionBenchmarkConfig {
			benchmark := testBenchmarkWithScore("p1", "b1", "accuracy", false)
			benchmark.PassCriteria = &api.PassCriteria{}
			return benchmark
		}()},
	}

	threshold, ids := calibratedThreshold(refs, provider, &api.PrimaryScore{Metric: "accuracy"}, StrictnessModerate)
	if threshold == nil || *threshold != 0.4 || !reflect.DeepEqual(ids, []string{"default-score"}) {
		t.Fatalf("calibratedThreshold() = (%v, %v), want (0.4, [default-score])", threshold, ids)
	}
}

func TestCloneTestDataRefCopiesEverySourceAndHandlesNil(t *testing.T) {
	t.Parallel()

	if cloneTestDataRef(nil) != nil {
		t.Fatal("cloneTestDataRef(nil) should return nil")
	}
	original := &api.TestDataRef{
		S3:  &api.S3TestDataRef{Bucket: "bucket", Key: "key", SecretRef: "secret"},
		PVC: &api.PVCTestDataRef{ClaimName: "claim", SubPath: "data"},
		Git: &api.GitTestDataRef{URL: "https://example.com/repo", Ref: "main", SubPath: "data", SecretRef: "git-secret"},
		HF:  &api.HFTestDataRef{RepoID: "org/repo", Revision: "main", SubPath: "data", SecretRef: "hf-secret"},
	}
	cloned := cloneTestDataRef(original)
	cloned.S3.Bucket = "changed"
	cloned.PVC.ClaimName = "changed"
	cloned.Git.Ref = "changed"
	cloned.HF.RepoID = "changed"
	if original.S3.Bucket != "bucket" || original.PVC.ClaimName != "claim" || original.Git.Ref != "main" || original.HF.RepoID != "org/repo" {
		t.Fatal("cloned test-data sources share mutable pointers with the original")
	}
}

func TestCloneJSONMapCopiesJSONShapeAndRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()

	if cloned, err := cloneJSONMap(nil); err != nil || cloned != nil {
		t.Fatalf("cloneJSONMap(nil) = (%v, %v), want (nil, nil)", cloned, err)
	}
	original := map[string]any{"nested": map[string]any{"values": []int{7, 8}}, "nil": nil}
	cloned, err := cloneJSONMap(original)
	if err != nil {
		t.Fatal(err)
	}
	values := cloned["nested"].(map[string]any)["values"].([]any)
	if values[0] != float64(7) || cloned["nil"] != nil {
		t.Fatalf("JSON copy shape = %#v", cloned)
	}
	values[0] = float64(9)
	if original["nested"].(map[string]any)["values"].([]int)[0] != 7 {
		t.Fatal("JSON copy shares nested mutable data with original")
	}

	if _, err := cloneJSONMap(map[string]any{"not_json": math.NaN()}); err == nil {
		t.Fatal("cloneJSONMap accepted a non-finite number")
	}
}

func proposalTestOptions(t *testing.T, filter string, max int, strictness string) Options {
	t.Helper()
	options, err := ParseOptions("design a collection", filter, strconv.Itoa(max), strictness)
	if err != nil {
		t.Fatal(err)
	}
	return options
}

func proposalTestSource() *fakeCatalogSource {
	return &fakeCatalogSource{providers: []api.ProviderResource{
		testProvider("p1", "b1", []string{"accuracy", "loss"}, &api.PrimaryScore{Metric: "accuracy"}, nil),
		testProvider("p2", "b2", []string{"latency"}, nil, nil),
	}}
}

func testProvider(providerID, benchmarkID string, metrics []string, primary *api.PrimaryScore, criteria *api.PassCriteria) api.ProviderResource {
	return api.ProviderResource{
		Resource: api.Resource{ID: providerID},
		ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{
			ID: benchmarkID, Metrics: append([]string(nil), metrics...), PrimaryScore: primary, PassCriteria: criteria,
		}}},
	}
}

func validProposal() api.CollectionConfig {
	return api.CollectionConfig{Name: "generated", Domains: []string{"reasoning"}, Benchmarks: []api.CollectionBenchmarkConfig{testBenchmark("p1", "b1")}}
}

func proposalWithBenchmarks(benchmarks ...api.CollectionBenchmarkConfig) api.CollectionConfig {
	return api.CollectionConfig{Name: "generated", Domains: []string{"reasoning"}, Benchmarks: benchmarks}
}

func testBenchmark(providerID, benchmarkID string) api.CollectionBenchmarkConfig {
	return api.CollectionBenchmarkConfig{Ref: api.Ref{ID: benchmarkID}, ProviderID: providerID}
}

func testBenchmarkWithScore(providerID, benchmarkID, metric string, lowerIsBetter bool) api.CollectionBenchmarkConfig {
	benchmark := testBenchmark(providerID, benchmarkID)
	benchmark.PrimaryScore = &api.PrimaryScore{Metric: metric, LowerIsBetter: lowerIsBetter}
	return benchmark
}

func testBenchmarkWithThreshold(providerID, benchmarkID string, value float32) api.CollectionBenchmarkConfig {
	benchmark := testBenchmark(providerID, benchmarkID)
	benchmark.PassCriteria = &api.PassCriteria{Threshold: &value}
	return benchmark
}

func testBenchmarkWithWeight(providerID, benchmarkID string, value float32) api.CollectionBenchmarkConfig {
	benchmark := testBenchmark(providerID, benchmarkID)
	benchmark.Weight = value
	return benchmark
}

func testBenchmarkWithScoreAndThreshold(providerID, benchmarkID, metric string, lowerIsBetter bool, value float32) api.CollectionBenchmarkConfig {
	benchmark := testBenchmarkWithScore(providerID, benchmarkID, metric, lowerIsBetter)
	benchmark.PassCriteria = &api.PassCriteria{Threshold: &value}
	return benchmark
}

func testBenchmarkWithWeightScoreAndThreshold(providerID, benchmarkID string, weight float32, metric string, lowerIsBetter bool, threshold float32) api.CollectionBenchmarkConfig {
	benchmark := testBenchmarkWithScoreAndThreshold(providerID, benchmarkID, metric, lowerIsBetter, threshold)
	benchmark.Weight = weight
	return benchmark
}

func systemReference(collectionID string, benchmarks ...api.CollectionBenchmarkConfig) api.CollectionResource {
	return api.CollectionResource{Resource: api.Resource{ID: collectionID, Owner: "system"}, CollectionConfig: api.CollectionConfig{Benchmarks: benchmarks}}
}

func hasIssue(issues []Issue, code, path string) bool {
	for _, issue := range issues {
		if issue.Code == code && issue.Path == path {
			return true
		}
	}
	return false
}

func hasDecision(decisions []CalibrationDecision, field, reason string) bool {
	for _, decision := range decisions {
		if decision.Field == field && strings.Contains(decision.Reason, reason) {
			return true
		}
	}
	return false
}

func decisionIDs(decisions []CalibrationDecision, field string) []string {
	for _, decision := range decisions {
		if decision.Field == field {
			return decision.ReferenceCollectionIDs
		}
	}
	return nil
}
