package generation

import (
	"errors"

	"github.com/eval-hub/eval-hub/pkg/api"
)

type runtimeRequirementType struct{ kind, action, parameter string }

const tokenizerGuidance = "verify the default tokenizer and supply a compatible tokenizer if needed."

var runtimeRequirementMessages = map[runtimeRequirementType]string{
	{api.GenerationRequirementJudgeModel, api.GenerationRequirementConfigure, "judge_model"}: "supply a judge model name and configure its connection before running.",
	{api.GenerationRequirementJudgeModel, api.GenerationRequirementVerify, "judge_model"}:    "verify the judge configuration and supply a judge model name if needed.",
	{api.GenerationRequirementScorerModel, api.GenerationRequirementVerify, "scorer_model"}:  "verify the scorer configuration and supply a scorer model name if needed.",
	{api.GenerationRequirementTokenizer, api.GenerationRequirementVerify, "tokenizer"}:       tokenizerGuidance,
	{api.GenerationRequirementTokenizer, api.GenerationRequirementVerify, "tokenizer_path"}:  tokenizerGuidance,
	{api.GenerationRequirementTestData, api.GenerationRequirementVerify, ""}:                 "check the referenced dataset requirements and configure a reachable data source if needed.",
	{api.GenerationRequirementGPU, api.GenerationRequirementVerify, ""}:                      "verify that the cluster can schedule the adapter's required GPU resources.",
	{api.GenerationRequirementResourceClaimTemplate, api.GenerationRequirementVerify, ""}:    "verify that the adapter's required resource claim template is configured in the execution namespace.",
}

// RuntimeRequirementMessage returns server-owned guidance for a supported
// kind, action, and parameter combination. An empty message means unsupported.
func RuntimeRequirementMessage(requirement api.CollectionGenerationRuntimeRequirement) string {
	return runtimeRequirementMessages[runtimeRequirementType{requirement.Kind, requirement.Action, requirement.Parameter}]
}

func validateRuntimeRequirements(benchmarks []api.CollectionBenchmarkConfig, requirements []api.CollectionGenerationRuntimeRequirement) error {
	if len(requirements) == 0 {
		return nil
	}
	if len(requirements) > api.MaxCollectionGenerationUnresolvedRequirements {
		return errors.New("proposal exceeds the unresolved runtime requirements limit")
	}
	type benchmarkKey struct{ provider, id string }
	type requirementKey struct {
		benchmarkKey
		kind, parameter string
	}
	selected := make(map[benchmarkKey]api.CollectionBenchmarkConfig, len(benchmarks))
	for _, benchmark := range benchmarks {
		selected[benchmarkKey{benchmark.ProviderID, benchmark.ID}] = benchmark
	}
	seen := make(map[requirementKey]struct{}, len(requirements))
	for _, requirement := range requirements {
		pair := benchmarkKey{requirement.ProviderID, requirement.ID}
		benchmark, ok := selected[pair]
		if !ok {
			return errors.New("unresolved runtime requirement does not match a selected benchmark")
		}
		key := requirementKey{pair, requirement.Kind, requirement.Parameter}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("proposal contains a duplicate unresolved runtime requirement")
		}
		seen[key] = struct{}{}
		if message := RuntimeRequirementMessage(requirement); message == "" || message != requirement.Message {
			return errors.New("proposal contains invalid unresolved runtime guidance")
		}
		if requirement.Action == api.GenerationRequirementConfigure && benchmark.Parameters["judge_model"] != "" {
			return errors.New("judge configuration guidance requires a declared blank judge model")
		}
	}
	return nil
}
