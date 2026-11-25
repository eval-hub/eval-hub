package orchestration

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/pkg/api"
	"sigs.k8s.io/yaml"
)

const maxConsideredBenchmarks = 20

type draft struct {
	Collection  api.CollectionConfig                `json:"collection"`
	Rationale   string                              `json:"rationale"`
	Explanation api.CollectionGenerationExplanation `json:"explanation"`
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
	return ok && !seen[key] && strings.TrimSpace(reason.Reason) != "" && len(reason.Reason) <= 300
}

// applyCatalogDefaults admits only safe values with unambiguous provenance.
// Runtime credentials and environment-specific inputs are never copied.
func applyCatalogDefaults(proposal *api.CollectionConfig, snapshot catalogSnapshot) ([]string, bool) {
	var notes []string
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
		id := benchmark.ProviderID + "/" + benchmark.ID
		for _, need := range []struct{ parameter, guidance string }{
			{"judge_model", "supply a judge model name and configure its connection before running"},
			{"scorer_model", "supply a scorer model name and configure its connection before running"},
			{"tokenizer", "verify the default tokenizer and supply a compatible tokenizer if needed"},
		} {
			if reference.inputs[need.parameter] {
				notes = append(notes, id+": "+need.guidance+".")
			}
		}
		if reference.inputs["test_data_ref"] {
			notes = append(notes, id+": check the referenced dataset requirements and configure a reachable data source if needed.")
		}
	}
	return notes, true
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
				case "judge_model", "scorer_model", "tokenizer":
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

func (d draft) result(proposal api.CollectionConfig, explanation api.CollectionGenerationExplanation, notes []string) (api.CollectionGenerationResult, error) {
	data, err := yaml.Marshal(proposal)
	if err != nil {
		return api.CollectionGenerationResult{}, err
	}
	rationale := strings.TrimSpace(d.Rationale) + "\n\nExecution readiness depends on the target model and deployment. The catalog does not establish all runtime prerequisites."
	if len(notes) > 0 {
		rationale += "\n\nRuntime inputs to check:\n- " + strings.Join(notes, "\n- ")
	}
	return api.CollectionGenerationResult{
		Collection: proposal, YAML: string(data), Rationale: rationale,
		Explanation: explanation, Actions: []api.CollectionGenerationAction{},
	}, nil
}
