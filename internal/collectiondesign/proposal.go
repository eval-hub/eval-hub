package collectiondesign

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
	validator "github.com/go-playground/validator/v10"
)

const (
	issueSchemaInvalid          = "schema_invalid"
	issueRequiredField          = "required_field"
	issueClassificationRequired = "classification_required"
	issueUnknownProvider        = "unknown_provider"
	issueUnknownBenchmark       = "unknown_benchmark"
	issueDuplicateBenchmark     = "duplicate_benchmark"
	issueProviderFilter         = "provider_filter_violation"
	issueBenchmarkCap           = "benchmark_cap_exceeded"
	issueMetricNotAdvertised    = "metric_not_advertised"
	issueDirectionConflict      = "direction_conflict"
	issueInvalidNumber          = "invalid_numeric_value"
	issueAdminOnlyField         = "admin_only_field"
	issueInvalidOptions         = "invalid_options"
)

// Issue is a stable, field-addressable collection finding. Catalog transport and
// cancellation failures are returned as errors instead of Issues.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// CalibrationDecision records a value selected from catalog evidence or why
// the value could not be justified. ReferenceCollectionIDs identify evidence.
type CalibrationDecision struct {
	ProviderID             string   `json:"provider_id"`
	BenchmarkID            string   `json:"benchmark_id"`
	Field                  string   `json:"field"`
	Value                  any      `json:"value,omitempty"`
	ReferenceCollectionIDs []string `json:"reference_collection_ids,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
}

type benchmarkKey struct {
	providerID  string
	benchmarkID string
}

type providerCatalog struct {
	providers  map[string]api.ProviderResource
	benchmarks map[benchmarkKey]api.BenchmarkResource
}

// collectionValidator is reused because its registrations are static and the
// validator supports concurrent use after initialization.
var collectionValidator = sync.OnceValues(validation.NewValidator)

// ValidateCollection checks the registered collection request rules and
// provider/benchmark consistency against a tenant-scoped catalog. It does not
// apply collection-generation options or mutate the candidate. Callers must
// supply a catalog source scoped to the current tenant.
func ValidateCollection(ctx context.Context, source CatalogSource, candidate api.CollectionConfig) ([]Issue, error) {
	_, issues, err := validateCollectionWithCatalog(ctx, source, candidate, nil)
	return issues, err
}

// ValidateProposal adds generation-specific checks to ValidateCollection.
// The returned config is a deep copy with server-enriched benchmark URLs
// removed. If issues are returned, the config must not be submitted.
func ValidateProposal(ctx context.Context, source CatalogSource, options Options, candidate api.CollectionConfig) (api.CollectionConfig, []Issue, error) {
	proposal, err := cloneCollectionConfig(candidate)
	if err != nil {
		return api.CollectionConfig{}, nil, fmt.Errorf("copy collection proposal: %w", err)
	}
	normalized, optionIssues := validateProposalOptions(options)
	preflightIssues := append(optionIssues, validateProposalFields(proposal)...)
	catalog, issues, err := validateCollectionWithCatalog(ctx, source, proposal, preflightIssues)
	if err != nil {
		return proposal, nil, err
	}
	if catalog != nil {
		issues = append(issues, validateProposalAgainstCatalog(proposal, normalized, catalog)...)
	}
	return proposal, sortedIssues(issues), nil
}

// CalibrateProposal fills only absent primary score, threshold, and weight
// values. It uses matching system-owned collections as evidence, never writes
// to storage, and returns explicit decisions for selected and unavailable
// values.
func CalibrateProposal(ctx context.Context, source CatalogSource, options Options, validated api.CollectionConfig) (api.CollectionConfig, []CalibrationDecision, error) {
	proposal, err := cloneCollectionConfig(validated)
	if err != nil {
		return api.CollectionConfig{}, nil, fmt.Errorf("copy collection proposal: %w", err)
	}
	normalized, optionIssues := validateProposalOptions(options)
	preflightIssues := append(optionIssues, validateProposalFields(proposal)...)
	catalog, issues, err := validateCollectionWithCatalog(ctx, source, proposal, preflightIssues)
	if err != nil {
		return proposal, nil, err
	}
	if catalog != nil {
		issues = append(issues, validateProposalAgainstCatalog(proposal, normalized, catalog)...)
	}
	if len(issues) != 0 {
		return proposal, nil, proposalIssuesError(sortedIssues(issues))
	}

	collections, err := readAllPages(ctx, source.ListCollections)
	if err != nil {
		return proposal, nil, fmt.Errorf("fetching calibration references: %w", err)
	}
	decisions := make([]CalibrationDecision, 0, len(proposal.Benchmarks)*3)
	for i := range proposal.Benchmarks {
		benchmark := &proposal.Benchmarks[i]
		key := benchmarkKey{providerID: benchmark.ProviderID, benchmarkID: benchmark.ID}
		providerBenchmark := catalog.benchmarks[key]
		pathPrefix := fmt.Sprintf("benchmarks[%d]", i)

		if benchmark.PrimaryScore == nil {
			defaultScore := advertisedDefaultScore(providerBenchmark)
			if defaultScore == nil {
				decisions = append(decisions, noDecision(*benchmark, pathPrefix+".primary_score", "provider has no advertised default primary score"))
			} else {
				copyScore := *defaultScore
				benchmark.PrimaryScore = &copyScore
				decisions = append(decisions, CalibrationDecision{
					ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID,
					Field: pathPrefix + ".primary_score", Value: copyScore,
					Reason: "copied the provider-advertised primary score",
				})
			}
		} else {
			decisions = append(decisions, CalibrationDecision{
				ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID,
				Field: pathPrefix + ".primary_score", Value: *benchmark.PrimaryScore,
				Reason: "preserved the explicit primary score",
			})
		}

		references := matchingReferenceBenchmarks(collections, key)
		if benchmark.Weight == 0 {
			value, ids := consensusReferenceWeight(references)
			if value == nil {
				decisions = append(decisions, noDecision(*benchmark, pathPrefix+".weight", "comparable system references do not agree on a positive weight"))
			} else {
				benchmark.Weight = *value
				decisions = append(decisions, CalibrationDecision{
					ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID,
					Field: pathPrefix + ".weight", Value: *value,
					ReferenceCollectionIDs: ids,
					Reason:                 "all comparable system references agree on this positive weight",
				})
			}
		} else {
			decisions = append(decisions, CalibrationDecision{
				ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID,
				Field: pathPrefix + ".weight", Value: benchmark.Weight,
				Reason: "preserved the explicit weight",
			})
		}

		if benchmark.PassCriteria != nil && benchmark.PassCriteria.Threshold != nil {
			decisions = append(decisions, CalibrationDecision{
				ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID,
				Field: pathPrefix + ".pass_criteria.threshold", Value: *benchmark.PassCriteria.Threshold,
				Reason: "preserved the explicit threshold",
			})
		} else {
			thresholdReferences := comparableThresholdReferences(references, *benchmark)
			threshold, ids := calibratedThreshold(thresholdReferences, providerBenchmark, benchmark.PrimaryScore, normalized.Strictness)
			if threshold == nil {
				decisions = append(decisions, noDecision(*benchmark, pathPrefix+".pass_criteria.threshold", "no finite comparable system-reference thresholds with a trusted direction"))
			} else {
				if benchmark.PassCriteria == nil {
					benchmark.PassCriteria = &api.PassCriteria{}
				}
				benchmark.PassCriteria.Threshold = threshold
				decisions = append(decisions, CalibrationDecision{
					ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID,
					Field: pathPrefix + ".pass_criteria.threshold", Value: *threshold,
					ReferenceCollectionIDs: ids,
					Reason:                 "derived from comparable system-reference thresholds using the requested strictness",
				})
			}
		}
	}

	issues, err = validateCollectionConfig(proposal)
	if err != nil {
		return proposal, decisions, err
	}
	if len(issues) != 0 {
		return proposal, decisions, proposalIssuesError(sortedIssues(issues))
	}
	return proposal, decisions, nil
}

func validateProposalOptions(options Options) (Options, []Issue) {
	issues := make([]Issue, 0, 3)
	if strings.TrimSpace(options.Goal) == "" {
		issues = append(issues, Issue{Code: issueInvalidOptions, Path: "evaluation_goal", Message: "evaluation_goal is required"})
	}
	if options.MaxBenchmarks <= 0 {
		issues = append(issues, Issue{Code: issueInvalidOptions, Path: "max_benchmarks", Message: "max_benchmarks must be a positive integer"})
	}
	if !isValidStrictness(options.Strictness) {
		issues = append(issues, Issue{Code: issueInvalidOptions, Path: "strictness", Message: fmt.Sprintf("invalid strictness %q; valid values: %s", options.Strictness, strings.Join(ValidStrictness(), ", "))})
	}
	return options, issues
}

// validateCollectionWithCatalog runs the shared rules once and returns the
// catalog so proposal-only checks and calibration can reuse the same snapshot.
// Preflight findings prevent catalog reads but are reported alongside field
// validation findings.
func validateCollectionWithCatalog(ctx context.Context, source CatalogSource, candidate api.CollectionConfig, preflightIssues []Issue) (*providerCatalog, []Issue, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if source == nil {
		return nil, nil, fmt.Errorf("catalog source is nil")
	}
	configIssues, err := validateCollectionConfig(candidate)
	if err != nil {
		return nil, nil, err
	}
	issues := append(configIssues, preflightIssues...)
	if len(issues) != 0 {
		return nil, sortedIssues(issues), nil
	}

	catalog, err := readProviderCatalog(ctx, source)
	if err != nil {
		return nil, nil, err
	}
	return catalog, sortedIssues(validateAgainstCatalog(candidate, catalog)), nil
}

func validateCollectionConfig(candidate api.CollectionConfig) ([]Issue, error) {
	instance, err := collectionValidator()
	if err != nil {
		return nil, fmt.Errorf("initialize collection validator: %w", err)
	}
	issues := make([]Issue, 0)
	if err := instance.Struct(candidate); err != nil {
		validationErrors, ok := err.(validator.ValidationErrors)
		if !ok {
			issues = append(issues, Issue{Code: issueSchemaInvalid, Path: "", Message: "collection failed request validation"})
		} else {
			for _, validationError := range validationErrors {
				code := issueSchemaInvalid
				message := "field failed request validation"
				if validationError.Tag() == "required" {
					code = issueRequiredField
					message = "required field is missing"
				} else if validationError.Tag() == "category_or_domains" {
					code = issueClassificationRequired
					message = "provide category or at least one domain"
				}
				issues = append(issues, Issue{Code: code, Path: jsonPath(validationError.Namespace()), Message: message})
			}
		}
	}
	for i, benchmark := range candidate.Benchmarks {
		if !finite(float64(benchmark.Weight)) {
			issues = append(issues, Issue{Code: issueInvalidNumber, Path: fmt.Sprintf("benchmarks[%d].weight", i), Message: "weight must be finite"})
		}
		if benchmark.PassCriteria != nil && benchmark.PassCriteria.Threshold != nil && !finite(float64(*benchmark.PassCriteria.Threshold)) {
			issues = append(issues, Issue{Code: issueInvalidNumber, Path: fmt.Sprintf("benchmarks[%d].pass_criteria.threshold", i), Message: "threshold must be finite"})
		}
	}
	if candidate.PassCriteria != nil && candidate.PassCriteria.Threshold != nil && !finite(float64(*candidate.PassCriteria.Threshold)) {
		issues = append(issues, Issue{Code: issueInvalidNumber, Path: "pass_criteria.threshold", Message: "threshold must be finite"})
	}
	return issues, nil
}

func validateProposalFields(candidate api.CollectionConfig) []Issue {
	if candidate.CurationOrder > 0 {
		return []Issue{{Code: issueAdminOnlyField, Path: "curation_order", Message: "curation_order is server-managed and cannot be set in a generated collection"}}
	}
	return nil
}

func readProviderCatalog(ctx context.Context, source CatalogSource) (*providerCatalog, error) {
	providers, err := readAllPages(ctx, source.ListProviders)
	if err != nil {
		return nil, fmt.Errorf("fetching provider catalog: %w", err)
	}
	result := &providerCatalog{
		providers:  make(map[string]api.ProviderResource, len(providers)),
		benchmarks: make(map[benchmarkKey]api.BenchmarkResource),
	}
	for _, provider := range providers {
		id := provider.Resource.ID
		if _, exists := result.providers[id]; exists {
			return nil, fmt.Errorf("provider catalog contains duplicate provider id %q", id)
		}
		result.providers[id] = provider
		for _, benchmark := range provider.Benchmarks {
			key := benchmarkKey{providerID: id, benchmarkID: benchmark.ID}
			if _, exists := result.benchmarks[key]; exists {
				return nil, fmt.Errorf("provider catalog contains duplicate benchmark pair (%q, %q)", id, benchmark.ID)
			}
			result.benchmarks[key] = benchmark
		}
	}
	return result, nil
}

func validateAgainstCatalog(candidate api.CollectionConfig, catalog *providerCatalog) []Issue {
	issues := make([]Issue, 0)
	seen := make(map[benchmarkKey]struct{}, len(candidate.Benchmarks))
	for i, selected := range candidate.Benchmarks {
		path := fmt.Sprintf("benchmarks[%d]", i)
		key := benchmarkKey{providerID: selected.ProviderID, benchmarkID: selected.ID}
		if _, exists := seen[key]; exists {
			issues = append(issues, Issue{Code: issueDuplicateBenchmark, Path: path, Message: "provider/benchmark pair is duplicated"})
		}
		seen[key] = struct{}{}

		_, providerExists := catalog.providers[selected.ProviderID]
		if !providerExists {
			issues = append(issues, Issue{Code: issueUnknownProvider, Path: path + ".provider_id", Message: fmt.Sprintf("provider %q is not present in the tenant catalog", selected.ProviderID)})
			continue
		}
		metadata, benchmarkExists := catalog.benchmarks[key]
		if !benchmarkExists {
			issues = append(issues, Issue{Code: issueUnknownBenchmark, Path: path + ".id", Message: fmt.Sprintf("benchmark %q is not advertised by provider %q", selected.ID, selected.ProviderID)})
			continue
		}
		if selected.PrimaryScore != nil && !contains(metadata.Metrics, selected.PrimaryScore.Metric) {
			issues = append(issues, Issue{Code: issueMetricNotAdvertised, Path: path + ".primary_score.metric", Message: fmt.Sprintf("metric %q is not advertised by provider %q for this benchmark", selected.PrimaryScore.Metric, selected.ProviderID)})
		}
		defaultScore := advertisedDefaultScore(metadata)
		// LowerIsBetter is a plain bool, so an omitted false cannot be
		// distinguished from an explicit false when the provider default is true.
		if selected.PrimaryScore != nil && defaultScore != nil && selected.PrimaryScore.Metric == defaultScore.Metric && selected.PrimaryScore.LowerIsBetter != defaultScore.LowerIsBetter {
			issues = append(issues, Issue{Code: issueDirectionConflict, Path: path + ".primary_score.lower_is_better", Message: fmt.Sprintf("lower_is_better must match the provider-advertised default (expected %t)", defaultScore.LowerIsBetter)})
		}
		effectiveScore := selected.PrimaryScore
		if effectiveScore == nil {
			effectiveScore = defaultScore
		}
		if selected.PassCriteria != nil && selected.PassCriteria.Threshold != nil && effectiveScore == nil {
			issues = append(issues, Issue{Code: issueMetricNotAdvertised, Path: path + ".pass_criteria.threshold", Message: "a threshold requires a verifiable advertised primary-score metric"})
		}
	}
	return issues
}

func validateProposalAgainstCatalog(candidate api.CollectionConfig, options Options, catalog *providerCatalog) []Issue {
	issues := make([]Issue, 0)
	requestedProviderIDs := sortedMapKeys(options.ProviderIDs)
	if options.ProviderFilter != "" && len(requestedProviderIDs) == 0 {
		issues = append(issues, Issue{Code: issueUnknownProvider, Path: "provider_filter", Message: "provider filter contains no provider IDs"})
	}
	for _, id := range requestedProviderIDs {
		if _, exists := catalog.providers[id]; !exists {
			issues = append(issues, Issue{Code: issueUnknownProvider, Path: "provider_filter", Message: fmt.Sprintf("provider %q is not present in the tenant catalog", id)})
		}
	}
	if len(candidate.Benchmarks) > options.MaxBenchmarks {
		issues = append(issues, Issue{Code: issueBenchmarkCap, Path: "benchmarks", Message: fmt.Sprintf("proposal contains %d benchmarks; maximum is %d", len(candidate.Benchmarks), options.MaxBenchmarks)})
	}
	if options.ProviderFilter != "" {
		for i, selected := range candidate.Benchmarks {
			if _, allowed := options.ProviderIDs[selected.ProviderID]; !allowed {
				if _, exists := catalog.benchmarks[benchmarkKey{providerID: selected.ProviderID, benchmarkID: selected.ID}]; exists {
					issues = append(issues, Issue{Code: issueProviderFilter, Path: fmt.Sprintf("benchmarks[%d].provider_id", i), Message: "benchmark provider is outside provider_filter"})
				}
			}
		}
	}
	return issues
}

func advertisedDefaultScore(benchmark api.BenchmarkResource) *api.PrimaryScore {
	if benchmark.PrimaryScore == nil || !contains(benchmark.Metrics, benchmark.PrimaryScore.Metric) {
		return nil
	}
	return benchmark.PrimaryScore
}

func matchingReferenceBenchmarks(collections []api.CollectionResource, key benchmarkKey) []referenceBenchmark {
	references := make([]referenceBenchmark, 0)
	for _, collection := range collections {
		if !collection.Resource.IsSystemResource() {
			continue
		}
		var match api.CollectionBenchmarkConfig
		matchCount := 0
		for _, benchmark := range collection.Benchmarks {
			if benchmark.ProviderID == key.providerID && benchmark.ID == key.benchmarkID {
				match = benchmark
				matchCount++
			}
		}
		if matchCount == 1 {
			references = append(references, referenceBenchmark{collectionID: collection.Resource.ID, benchmark: match})
		}
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].collectionID == references[j].collectionID {
			return references[i].benchmark.ID < references[j].benchmark.ID
		}
		return references[i].collectionID < references[j].collectionID
	})
	unique := references[:0]
	for _, reference := range references {
		if len(unique) == 0 || reference.collectionID != unique[len(unique)-1].collectionID {
			unique = append(unique, reference)
		}
	}
	return unique
}

// comparableThresholdReferences limits threshold evidence to system benchmarks
// with the same configured parameters and test-data identity as the candidate.
// Provider metadata does not expose parameter defaults, and mutable test-data
// sources need a resolved content identity, so unknown equivalence is excluded.
func comparableThresholdReferences(references []referenceBenchmark, candidate api.CollectionBenchmarkConfig) []referenceBenchmark {
	comparable := make([]referenceBenchmark, 0, len(references))
	for _, reference := range references {
		if !sameConfiguredParameters(candidate.Parameters, reference.benchmark.Parameters) {
			continue
		}
		if !sameTestDataIdentity(candidate.TestDataRef, reference.benchmark.TestDataRef) {
			continue
		}
		comparable = append(comparable, reference)
	}
	return comparable
}

func sameConfiguredParameters(left, right map[string]any) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	return reflect.DeepEqual(left, right)
}

func sameTestDataIdentity(left, right *api.TestDataRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.ResolvedSHA == "" || left.ResolvedSHA != right.ResolvedSHA {
		return false
	}
	leftSource := *left
	rightSource := *right
	leftSource.ResolvedSHA = ""
	rightSource.ResolvedSHA = ""
	return reflect.DeepEqual(leftSource, rightSource)
}

type referenceBenchmark struct {
	collectionID string
	benchmark    api.CollectionBenchmarkConfig
}

func consensusReferenceWeight(references []referenceBenchmark) (*float32, []string) {
	if len(references) == 0 {
		return nil, nil
	}
	var value *float32
	ids := make([]string, 0)
	for _, reference := range references {
		weight := reference.benchmark.Weight
		if weight <= 0 || !finite(float64(weight)) {
			return nil, nil
		}
		if value == nil {
			copyWeight := weight
			value = &copyWeight
			ids = append(ids, reference.collectionID)
			continue
		}
		if *value != weight {
			return nil, nil
		}
		ids = append(ids, reference.collectionID)
	}
	return value, uniqueSorted(ids)
}

func calibratedThreshold(references []referenceBenchmark, providerBenchmark api.BenchmarkResource, candidateScore *api.PrimaryScore, strictness string) (*float32, []string) {
	defaultScore := advertisedDefaultScore(providerBenchmark)
	if defaultScore == nil || candidateScore == nil || candidateScore.Metric != defaultScore.Metric || candidateScore.LowerIsBetter != defaultScore.LowerIsBetter {
		return nil, nil
	}
	values := make([]float64, 0)
	ids := make([]string, 0)
	for _, reference := range references {
		refScore := reference.benchmark.PrimaryScore
		if refScore == nil {
			refScore = defaultScore
		}
		if refScore.Metric != defaultScore.Metric || refScore.LowerIsBetter != defaultScore.LowerIsBetter {
			continue
		}
		if reference.benchmark.PassCriteria == nil || reference.benchmark.PassCriteria.Threshold == nil {
			continue
		}
		threshold := float64(*reference.benchmark.PassCriteria.Threshold)
		if !finite(threshold) {
			continue
		}
		values = append(values, threshold)
		ids = append(ids, reference.collectionID)
	}
	if len(values) == 0 {
		return nil, nil
	}
	sort.Float64s(values)
	var selected float64
	switch strictness {
	case StrictnessLenient:
		if defaultScore.LowerIsBetter {
			selected = values[len(values)-1]
		} else {
			selected = values[0]
		}
	case StrictnessStrict:
		if defaultScore.LowerIsBetter {
			selected = values[0]
		} else {
			selected = values[len(values)-1]
		}
	default:
		middle := len(values) / 2
		if len(values)%2 == 0 {
			selected = (values[middle-1] + values[middle]) / 2
		} else {
			selected = values[middle]
		}
	}
	result := float32(selected)
	if !finite(float64(result)) {
		return nil, nil
	}
	return &result, uniqueSorted(ids)
}

func noDecision(benchmark api.CollectionBenchmarkConfig, field, reason string) CalibrationDecision {
	return CalibrationDecision{ProviderID: benchmark.ProviderID, BenchmarkID: benchmark.ID, Field: field, Reason: reason}
}

func proposalIssuesError(issues []Issue) error {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s: %s", issue.Path, issue.Code))
	}
	return fmt.Errorf("proposal has validation issues: %s", strings.Join(parts, "; "))
}

func jsonPath(namespace string) string {
	if index := strings.IndexByte(namespace, '.'); index >= 0 {
		return namespace[index+1:]
	}
	return namespace
}

func sortedIssues(issues []Issue) []Issue {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			if issues[i].Code == issues[j].Code {
				return issues[i].Message < issues[j].Message
			}
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}

func sortedMapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func cloneCollectionConfig(candidate api.CollectionConfig) (api.CollectionConfig, error) {
	copy := candidate
	copy.Tags = cloneStrings(candidate.Tags)
	copy.Domains = cloneStrings(candidate.Domains)
	copy.Tasks = cloneStrings(candidate.Tasks)
	copy.Modalities = cloneStrings(candidate.Modalities)
	copy.Industries = cloneStrings(candidate.Industries)
	copy.EvaluationTargets = cloneStrings(candidate.EvaluationTargets)
	copy.PassCriteria = clonePassCriteria(candidate.PassCriteria)
	copy.Agent = cloneCollectionAgent(candidate.Agent)
	if candidate.Custom != nil {
		custom, err := cloneJSONMap(*candidate.Custom)
		if err != nil {
			return api.CollectionConfig{}, fmt.Errorf("custom: %w", err)
		}
		copy.Custom = &custom
	}
	copy.Benchmarks = make([]api.CollectionBenchmarkConfig, len(candidate.Benchmarks))
	for i, benchmark := range candidate.Benchmarks {
		copy.Benchmarks[i] = benchmark
		copy.Benchmarks[i].URL = ""
		copy.Benchmarks[i].PrimaryScore = clonePrimaryScore(benchmark.PrimaryScore)
		copy.Benchmarks[i].PassCriteria = clonePassCriteria(benchmark.PassCriteria)
		parameters, err := cloneJSONMap(benchmark.Parameters)
		if err != nil {
			return api.CollectionConfig{}, fmt.Errorf("benchmarks[%d].parameters: %w", i, err)
		}
		copy.Benchmarks[i].Parameters = parameters
		copy.Benchmarks[i].TestDataRef = cloneTestDataRef(benchmark.TestDataRef)
	}
	return copy, nil
}

func cloneCollectionAgent(agent *api.CollectionAgentMetadata) *api.CollectionAgentMetadata {
	if agent == nil {
		return nil
	}
	copy := *agent
	copy.Evaluates = cloneStrings(agent.Evaluates)
	copy.RecommendedWhen = cloneStrings(agent.RecommendedWhen)
	copy.Complements = cloneStrings(agent.Complements)
	copy.Hints = cloneStrings(agent.Hints)
	copy.ResultInterpretation = cloneStrings(agent.ResultInterpretation)
	return &copy
}

func clonePrimaryScore(score *api.PrimaryScore) *api.PrimaryScore {
	if score == nil {
		return nil
	}
	copy := *score
	return &copy
}

func clonePassCriteria(criteria *api.PassCriteria) *api.PassCriteria {
	if criteria == nil {
		return nil
	}
	copy := *criteria
	if criteria.Threshold != nil {
		threshold := *criteria.Threshold
		copy.Threshold = &threshold
	}
	return &copy
}

func cloneTestDataRef(ref *api.TestDataRef) *api.TestDataRef {
	if ref == nil {
		return nil
	}
	copy := *ref
	if ref.S3 != nil {
		item := *ref.S3
		copy.S3 = &item
	}
	if ref.PVC != nil {
		item := *ref.PVC
		copy.PVC = &item
	}
	if ref.Git != nil {
		item := *ref.Git
		copy.Git = &item
	}
	if ref.HF != nil {
		item := *ref.HF
		copy.HF = &item
	}
	return &copy
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

// cloneJSONMap copies a JSON-shaped object through the same representation the
// API uses. Concrete Go numeric/slice types are normalized by encoding/json.
func cloneJSONMap(values map[string]any) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON object: %w", err)
	}
	var copy map[string]any
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil, fmt.Errorf("unmarshal JSON object: %w", err)
	}
	return copy, nil
}
