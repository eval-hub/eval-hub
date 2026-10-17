package collectiondesign

import (
	"context"
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
	if candidate.Benchmarks[0].URL == "" {
		t.Fatal("ValidateProposal mutated the caller's candidate")
	}
	got.Tags[0] = "changed"
	got.Agent.Hints[0] = "changed"
	(*got.Custom)["nested"].(map[string]any)["values"].([]any)[0] = "changed"
	(*got.Custom)["typed_slice"].([]int)[0] = 100
	(*got.Custom)["typed_map"].(map[string][]int)["values"][0] = 101
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
		{name: "unknown provider", candidate: proposalWithBenchmarks(testBenchmark("missing", "b1")), options: validOptions, wantCode: issueUnknownProvider, wantPath: "benchmarks[0].provider_id"},
		{name: "unknown benchmark pair", candidate: proposalWithBenchmarks(testBenchmark("p1", "missing")), options: validOptions, wantCode: issueUnknownBenchmark, wantPath: "benchmarks[0].id"},
		{name: "duplicate pair", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1"), testBenchmark("p1", "b1")), options: proposalTestOptions(t, "", 3, StrictnessModerate), wantCode: issueDuplicateBenchmark, wantPath: "benchmarks[1]"},
		{name: "provider filter violation", candidate: proposalWithBenchmarks(testBenchmark("p2", "b2")), options: validOptions, wantCode: issueProviderFilter, wantPath: "benchmarks[0].provider_id"},
		{name: "unknown filtered provider", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1")), options: proposalTestOptions(t, "p1,missing", 3, StrictnessModerate), wantCode: issueUnknownProvider, wantPath: "provider_filter"},
		{name: "comma only provider filter", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1")), options: proposalTestOptions(t, ", ,", 3, StrictnessModerate), wantCode: issueUnknownProvider, wantPath: "provider_filter"},
		{name: "benchmark cap", candidate: proposalWithBenchmarks(testBenchmark("p1", "b1"), testBenchmark("p2", "b2")), options: validOptions, wantCode: issueBenchmarkCap, wantPath: "benchmarks"},
		{name: "unadvertised metric", candidate: proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "unadvertised", false)), options: validOptions, wantCode: issueMetricNotAdvertised, wantPath: "benchmarks[0].primary_score.metric"},
		{name: "conflicting direction", candidate: proposalWithBenchmarks(testBenchmarkWithScore("p1", "b1", "accuracy", true)), options: validOptions, wantCode: issueDirectionConflict, wantPath: "benchmarks[0].primary_score.lower_is_better"},
		{name: "threshold without metric", candidate: proposalWithBenchmarks(testBenchmarkWithThreshold("p2", "b2", 0.5)), options: validOptions, wantCode: issueMetricNotAdvertised, wantPath: "benchmarks[0].pass_criteria.threshold"},
		{name: "admin curation field", candidate: func() api.CollectionConfig { c := validProposal(); c.CurationOrder = 2; return c }(), options: validOptions, wantCode: issueAdminOnlyField, wantPath: "curation_order"},
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
	if _, _, err := ValidateProposal(context.Background(), nil, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); err == nil || !strings.Contains(err.Error(), "catalog source is nil") {
		t.Fatalf("nil source validation error = %v", err)
	}
	if _, _, err := CalibrateProposal(context.Background(), nil, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); err == nil || !strings.Contains(err.Error(), "catalog source is nil") {
		t.Fatalf("nil source calibration error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ValidateProposal(ctx, proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled validation error = %v", err)
	}

	invalidOptions := Options{Goal: "goal", MaxBenchmarks: 0, Strictness: "invalid"}
	_, issues, err := ValidateProposal(context.Background(), proposalTestSource(), invalidOptions, validProposal())
	if err != nil || len(issues) != 1 || issues[0].Code != issueInvalidOptions || issues[0].Path != "max_benchmarks" {
		t.Fatalf("invalid options: issues=%+v err=%v", issues, err)
	}
	invalidGoal := Options{MaxBenchmarks: 2, Strictness: StrictnessModerate}
	_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), invalidGoal, validProposal())
	if err != nil || len(issues) != 1 || issues[0].Path != "evaluation_goal" {
		t.Fatalf("invalid goal: issues=%+v err=%v", issues, err)
	}
	invalidStrictness := Options{Goal: "goal", MaxBenchmarks: 2, Strictness: "extreme"}
	_, issues, err = ValidateProposal(context.Background(), proposalTestSource(), invalidStrictness, validProposal())
	if err != nil || len(issues) != 1 || issues[0].Path != "strictness" {
		t.Fatalf("invalid strictness: issues=%+v err=%v", issues, err)
	}

	providerErr := errors.New("catalog unavailable")
	source := &fakeCatalogSource{providerErr: providerErr}
	_, issues, err = ValidateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if len(issues) != 0 || !errors.Is(err, providerErr) || !strings.Contains(err.Error(), "fetching provider catalog") {
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
			testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "accuracy", false, 0.9))},
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
	if !hasDecision(decisions, "benchmarks[0].pass_criteria.threshold", "trusted direction") {
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
	if !hasDecision(decisions, "benchmarks[0].primary_score", "no advertised default") || !hasDecision(decisions, "benchmarks[0].pass_criteria.threshold", "trusted direction") {
		t.Fatalf("missing reasons for invalid provider default: %+v", decisions)
	}
}

func TestCalibrateProposalMissingEvidenceAndWeightConsensus(t *testing.T) {
	t.Parallel()
	provider := testProvider("p1", "b1", []string{"accuracy"}, &api.PrimaryScore{Metric: "accuracy"}, nil)
	for _, test := range []struct {
		name             string
		refs             []api.CollectionResource
		wantWeight       float32
		wantThreshold    bool
		wantReasonPhrase string
	}{
		{name: "no references", wantReasonPhrase: "no finite comparable"},
		{name: "agreed weights but no thresholds", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.NaN()))),
			systemReference("b", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.Inf(1)))),
		}, wantWeight: 3, wantReasonPhrase: "no finite comparable"},
		{name: "conflicting weights", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.NaN()))),
			systemReference("b", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 4, "accuracy", false, float32(math.NaN()))),
		}, wantReasonPhrase: "do not agree"},
		{name: "omitted reference weight prevents consensus", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 3, "accuracy", false, float32(math.NaN()))),
			systemReference("b", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 0, "accuracy", false, float32(math.NaN()))),
		}, wantReasonPhrase: "do not agree"},
		{name: "direction mismatches", refs: []api.CollectionResource{
			systemReference("a", testBenchmarkWithWeightScoreAndThreshold("p1", "b1", 1, "accuracy", true, 0.4)),
		}, wantWeight: 1, wantReasonPhrase: "no finite comparable"},
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
			foundReason := false
			for _, decision := range decisions {
				if decision.Reason != "" && strings.Contains(decision.Reason, test.wantReasonPhrase) {
					foundReason = true
				}
			}
			if !foundReason {
				t.Fatalf("expected reason containing %q in %+v", test.wantReasonPhrase, decisions)
			}
		})
	}
}

func TestCalibrateProposalErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := CalibrateProposal(ctx, proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), validProposal()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled calibration error = %v", err)
	}

	_, _, err := CalibrateProposal(context.Background(), proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), api.CollectionConfig{})
	if err == nil || !strings.Contains(err.Error(), issueClassificationRequired) {
		t.Fatalf("invalid proposal error = %v", err)
	}

	collectionErr := errors.New("references unavailable")
	source := &fakeCatalogSource{providers: proposalTestSource().providers, collectionErr: collectionErr}
	_, _, err = CalibrateProposal(context.Background(), source, proposalTestOptions(t, "", 2, StrictnessModerate), validProposal())
	if !errors.Is(err, collectionErr) || !strings.Contains(err.Error(), "fetching calibration references") {
		t.Fatalf("reference read error = %v", err)
	}

	badCandidate := validProposal()
	badCandidate.Benchmarks[0].PrimaryScore = &api.PrimaryScore{Metric: "not-advertised"}
	_, _, err = CalibrateProposal(context.Background(), proposalTestSource(), proposalTestOptions(t, "", 2, StrictnessModerate), badCandidate)
	if err == nil || !strings.Contains(err.Error(), issueMetricNotAdvertised) {
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
	if len(got) != 1 || got[0].collectionID != "same" {
		t.Fatalf("matchingReferenceBenchmarks() = %+v, want only the unique system reference", got)
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

func TestCloneAnyValueHandlesNestedKindsAndNilValues(t *testing.T) {
	t.Parallel()

	if cloneAnyValue(reflect.Value{}).IsValid() {
		t.Fatal("invalid reflect.Value should remain invalid")
	}
	var nilInterface any
	clonedInterface := cloneAnyValue(reflect.ValueOf(&nilInterface).Elem())
	if clonedInterface.Kind() != reflect.Interface || !clonedInterface.IsNil() {
		t.Fatalf("nil interface clone = %#v, want a nil interface", clonedInterface.Interface())
	}

	var nilMap map[string][]int
	if cloned := cloneAnyValue(reflect.ValueOf(nilMap)); !cloned.IsNil() {
		t.Fatalf("nil map clone = %#v, want nil", cloned.Interface())
	}
	var nilSlice []int
	if cloned := cloneAnyValue(reflect.ValueOf(nilSlice)); !cloned.IsNil() {
		t.Fatalf("nil slice clone = %#v, want nil", cloned.Interface())
	}
	var nilPointer *int
	if cloned := cloneAnyValue(reflect.ValueOf(nilPointer)); !cloned.IsNil() {
		t.Fatalf("nil pointer clone = %#v, want nil", cloned.Interface())
	}

	type nested struct {
		Values []int
	}
	originalArray := [1]map[string][]int{{"values": {1}}}
	clonedArray := cloneAnyValue(reflect.ValueOf(originalArray)).Interface().([1]map[string][]int)
	clonedArray[0]["values"][0] = 2
	if originalArray[0]["values"][0] != 1 {
		t.Fatal("array clone shares nested map or slice data with the original")
	}

	originalPointer := &[]int{3}
	clonedPointer := cloneAnyValue(reflect.ValueOf(originalPointer)).Interface().(*[]int)
	(*clonedPointer)[0] = 4
	if (*originalPointer)[0] != 3 {
		t.Fatal("pointer clone shares nested slice data with the original")
	}

	originalStruct := nested{Values: []int{5}}
	clonedStruct := cloneAnyValue(reflect.ValueOf(originalStruct)).Interface().(nested)
	clonedStruct.Values[0] = 6
	if originalStruct.Values[0] != 5 {
		t.Fatal("struct clone shares nested slice data with the original")
	}

	interfaceValue := any(map[string][]int{"values": {7}})
	clonedValue := cloneAnyValue(reflect.ValueOf(&interfaceValue).Elem()).Interface().(map[string][]int)
	clonedValue["values"][0] = 8
	if interfaceValue.(map[string][]int)["values"][0] != 7 {
		t.Fatal("non-nil interface clone shares nested data with the original")
	}

	if got := cloneAnyValue(reflect.ValueOf("scalar")).Interface(); got != "scalar" {
		t.Fatalf("scalar clone = %v, want scalar", got)
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
