package orchestration

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/pkg/api"
	"sigs.k8s.io/yaml"
)

const maxConsideredBenchmarks = 20

type draft struct {
	Collection  api.CollectionConfig `json:"collection"`
	Rationale   string               `json:"rationale"`
	Explanation draftExplanation     `json:"explanation"`
}

// Runtime requirements are derived by the runner and cannot be authored by the model.
type draftExplanation struct {
	SelectedBenchmarks []api.CollectionGenerationBenchmarkReason `json:"selected_benchmarks"`
	RejectedBenchmarks []api.CollectionGenerationBenchmarkReason `json:"rejected_benchmarks"`
}

type finding struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

func parseDraft(text string) (draft, bool) {
	var proposal draft
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proposal) != nil || decoder.Decode(new(any)) != io.EOF {
		return draft{}, false
	}
	return proposal, true
}

func (d draft) validate(ctx context.Context, snapshot catalogSnapshot, options collectiondesign.Options, request api.CollectionGenerationRequest, eligible map[benchmarkKey]candidate) (api.CollectionConfig, []finding, error) {
	c := d.Collection
	if request.CollectionName != "" {
		c.Name = request.CollectionName
	}
	if request.CollectionDescription != "" {
		c.Description = request.CollectionDescription
	}
	if strings.TrimSpace(c.Description) == "" {
		return api.CollectionConfig{}, []finding{{Code: "required_field", Path: "collection.description"}}, nil
	}
	if c.Custom != nil || c.Agent != nil || c.PassCriteria != nil {
		return api.CollectionConfig{}, []finding{{Code: "unsupported_collection_fields", Path: "collection"}}, nil
	}
	// Validate the model's classification and metric claims before deriving
	// configuration defaults from catalog evidence.
	validated, issues, err := collectiondesign.ValidateProposal(ctx, snapshot, options, c)
	if err != nil || len(issues) > 0 {
		return api.CollectionConfig{}, proposalFindings(issues), err
	}
	for _, selected := range validated.Benchmarks {
		key := benchmarkKey{selected.ProviderID, selected.ID}
		entry, ok := eligible[key]
		if !ok {
			return api.CollectionConfig{}, []finding{{Code: "insufficient_catalog_evidence", Path: "collection.benchmarks"}}, nil
		}
		if score := selected.PrimaryScore; score != nil {
			if !slices.Contains(entry.benchmark.Metrics, score.Metric) {
				return api.CollectionConfig{}, []finding{{Code: "metric_not_advertised", Path: "collection.benchmarks"}}, nil
			}
			// A metric name alone does not establish its score direction.
			advertised := entry.benchmark.PrimaryScore
			if advertised == nil || advertised.Metric != score.Metric {
				return api.CollectionConfig{}, []finding{{Code: "unverified_score_direction", Path: "collection.benchmarks"}}, nil
			}
		}
		// A stageable data source is environment-specific. Provider metadata
		// never authorizes a model to choose a storage location.
		if selected.TestDataRef != nil {
			return api.CollectionConfig{}, []finding{{Code: "unsupported_test_data_reference", Path: "collection.benchmarks"}}, nil
		}
	}
	return validated, nil, nil
}

func proposalFindings(issues []collectiondesign.Issue) []finding {
	result := make([]finding, 0, min(len(issues), 8))
	for _, issue := range issues[:min(len(issues), 8)] {
		// Issue messages can include draft or catalog values. Corrections only
		// contain stable codes and schema paths.
		result = append(result, finding{Code: issue.Code, Path: clip(issue.Path, 96)})
	}
	return result
}

func (d draft) explanations(c api.CollectionConfig, eligible map[benchmarkKey]candidate) (api.CollectionGenerationExplanation, bool) {
	var result api.CollectionGenerationExplanation
	if len(d.Explanation.SelectedBenchmarks) != len(c.Benchmarks) || len(d.Explanation.RejectedBenchmarks) > maxConsideredBenchmarks {
		return result, false
	}
	seen := make(map[benchmarkKey]bool)
	for _, reason := range d.Explanation.SelectedBenchmarks {
		key := benchmarkKey{reason.ProviderID, reason.ID}
		if !validReason(reason, eligible, seen) {
			return result, false
		}
		seen[key] = true
	}
	selected := make(map[benchmarkKey]api.CollectionGenerationBenchmarkReason, len(seen))
	for _, reason := range d.Explanation.SelectedBenchmarks {
		selected[benchmarkKey{reason.ProviderID, reason.ID}] = reason
	}
	for _, benchmark := range c.Benchmarks {
		key := benchmarkKey{benchmark.ProviderID, benchmark.ID}
		reason, ok := selected[key]
		if !ok {
			return result, false
		}
		delete(selected, key)
		result.SelectedBenchmarks = append(result.SelectedBenchmarks, reason)
	}
	rejected := d.Explanation.RejectedBenchmarks
	for _, reason := range rejected {
		key := benchmarkKey{reason.ProviderID, reason.ID}
		if !validReason(reason, eligible, seen) {
			return result, false
		}
		seen[key] = true
	}
	slices.SortFunc(rejected, func(a, b api.CollectionGenerationBenchmarkReason) int {
		return compareCandidates(eligible[benchmarkKey{a.ProviderID, a.ID}], eligible[benchmarkKey{b.ProviderID, b.ID}])
	})
	result.RejectedBenchmarks = append([]api.CollectionGenerationBenchmarkReason{}, rejected[:min(len(rejected), 5)]...)
	return result, true
}

func validReason(reason api.CollectionGenerationBenchmarkReason, eligible map[benchmarkKey]candidate, seen map[benchmarkKey]bool) bool {
	key := benchmarkKey{reason.ProviderID, reason.ID}
	_, ok := eligible[key]
	return ok && !seen[key] && strings.TrimSpace(reason.Reason) != "" && utf8.RuneCountInString(reason.Reason) <= api.MaxCollectionGenerationReasonLength
}

// applyCatalogDefaults admits only safe values with unambiguous provenance.
// Runtime credentials and environment-specific inputs are never copied.
func applyCatalogDefaults(proposal *api.CollectionConfig, snapshot catalogSnapshot) ([]api.CollectionGenerationRuntimeRequirement, bool) {
	var requirements []api.CollectionGenerationRuntimeRequirement
	runtimes := make(map[string]*api.K8sRuntime, len(snapshot.providers))
	for _, provider := range snapshot.providers {
		if provider.Runtime != nil {
			runtimes[provider.Resource.ID] = provider.Runtime.K8s
		}
	}
	for i := range proposal.Benchmarks {
		benchmark := &proposal.Benchmarks[i]
		reference := systemDefaults(*benchmark, snapshot.collections)
		// Confirm any explicit parameters were actually advertised as defaults.
		for name, value := range benchmark.Parameters {
			if !reflect.DeepEqual(reference.parameters[name], value) {
				return nil, false
			}
			if _, found := reference.parameters[name]; !found {
				return nil, false
			}
		}
		benchmark.Parameters = reference.parameters
		// All thresholds and weights come from deterministic calibration.
		benchmark.Weight = 0
		benchmark.PassCriteria = nil
		addRequirement := func(kind, parameter, action string) {
			requirement := api.CollectionGenerationRuntimeRequirement{
				ProviderID: benchmark.ProviderID, ID: benchmark.ID,
				Kind: kind, Action: action, Parameter: parameter,
			}
			requirement.Message = generation.RuntimeRequirementMessage(requirement)
			requirements = append(requirements, requirement)
		}
		for _, need := range []struct{ parameter, kind string }{
			{"judge_model", api.GenerationRequirementJudgeModel},
			{"scorer_model", api.GenerationRequirementScorerModel},
			{"tokenizer", api.GenerationRequirementTokenizer},
			{"tokenizer_path", api.GenerationRequirementTokenizer},
		} {
			if reference.inputs[need.parameter] {
				action := api.GenerationRequirementVerify
				if need.kind == api.GenerationRequirementJudgeModel && benchmark.Parameters[need.parameter] == "" {
					action = api.GenerationRequirementConfigure
				}
				addRequirement(need.kind, need.parameter, action)
			}
		}
		if reference.inputs["test_data_ref"] {
			addRequirement(api.GenerationRequirementTestData, "", api.GenerationRequirementVerify)
		}
		if runtime := runtimes[benchmark.ProviderID]; runtime != nil {
			if runtime.GPU != nil && runtime.GPU.Count > 0 {
				addRequirement(api.GenerationRequirementGPU, "", api.GenerationRequirementVerify)
			}
			if runtime.DRA != nil {
				addRequirement(api.GenerationRequirementResourceClaimTemplate, "", api.GenerationRequirementVerify)
			}
		}
	}
	return requirements, true
}

type defaults struct {
	parameters map[string]any
	inputs     map[string]bool
}

func systemDefaults(selected api.CollectionBenchmarkConfig, collections []api.CollectionResource) defaults {
	out := defaults{inputs: make(map[string]bool)}
	values := make(map[string]any)
	conflicts := make(map[string]bool)
	for _, collection := range collections {
		if !collection.Resource.IsSystemResource() {
			continue
		}
		for _, example := range collection.Benchmarks {
			if example.ID != selected.ID || example.ProviderID != selected.ProviderID {
				continue
			}
			if example.TestDataRef != nil {
				out.inputs["test_data_ref"] = true
			}
			for name, value := range example.Parameters {
				switch name {
				case "judge_model", "scorer_model", "tokenizer", "tokenizer_path":
					out.inputs[name] = true
				}
				if !safeDefault(name, value) {
					conflicts[name] = true
					continue
				}
				if prior, ok := values[name]; ok && !reflect.DeepEqual(prior, value) {
					conflicts[name] = true
				}
				values[name] = value
			}
		}
	}
	for name := range conflicts {
		delete(values, name)
	}
	if len(values) != 0 {
		out.parameters = values
	}
	return out
}

func safeDefault(name string, value any) bool {
	switch name {
	case "judge_model":
		return value == ""
	case "apply_chat_template", "disable_cache", "fewshot_as_multiturn":
		_, ok := value.(bool)
		return ok
	case "batch_size", "max_concurrent", "max_tokens", "num_examples", "num_few_shot",
		"num_fewshot", "num_samples", "pass_at_k", "random_seed", "request_timeout", "temperature":
		switch value.(type) {
		case int, float64, json.Number:
			return true
		}
	}
	return false
}

func (d draft) result(proposal api.CollectionConfig, explanation api.CollectionGenerationExplanation, requirements []api.CollectionGenerationRuntimeRequirement) (api.CollectionGenerationResult, error) {
	data, err := yaml.Marshal(proposal)
	if err != nil {
		return api.CollectionGenerationResult{}, err
	}
	notes := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		notes = append(notes, requirement.ProviderID+"/"+requirement.ID+": "+requirement.Message)
	}
	explanation.UnresolvedRequirements = requirements[:min(len(requirements), api.MaxCollectionGenerationUnresolvedRequirements)]
	return api.CollectionGenerationResult{
		Collection: proposal, YAML: string(data), Rationale: boundedRationale(d.Rationale, notes),
		Explanation: explanation, Actions: []api.CollectionGenerationAction{},
	}, nil
}

func boundedRationale(modelRationale string, notes []string) string {
	const readiness = "\n\nExecution readiness depends on the target model and deployment. The catalog does not establish all runtime prerequisites."
	const moreNotes = "\n- Additional runtime guidance exceeded the size limit. Check the benchmark and provider configurations before running."
	const minModelSpace = 1024
	var guidance strings.Builder
	guidance.WriteString(readiness)
	if len(notes) > 0 {
		guidance.WriteString("\n\nRuntime inputs to check:")
	}
	// Keep complete runtime notes and reserve space for model rationale. Report
	// truncation only when the notes themselves exceed the available space.
	for _, note := range notes {
		if guidance.Len()+len("\n- ")+len(note)+len(moreNotes)+minModelSpace > generation.MaxRationaleBytes {
			guidance.WriteString(moreNotes)
			break
		}
		guidance.WriteString("\n- ")
		guidance.WriteString(note)
	}
	return clip(modelRationale, generation.MaxRationaleBytes-guidance.Len()) + guidance.String()
}
