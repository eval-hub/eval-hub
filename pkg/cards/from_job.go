package cards

import (
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// NewEvaluationCard builds an evaluation card from a persisted evaluation job resource.
func NewEvaluationCard(job *api.EvaluationJobResource) *EvaluationCard {
	if job == nil {
		return nil
	}

	card := &EvaluationCard{
		CardVersion:   CardVersion,
		SchemaVersion: SchemaVersion,
		Metadata:      newEvaluationCardMetadata(job),
		Context:       newEvaluationCardContext(job),
	}

	if results := newEvaluationCardResults(job); results != nil {
		card.Results = results
	}

	return card
}

func newEvaluationCardMetadata(job *api.EvaluationJobResource) EvaluationCardMetadata {
	return EvaluationCardMetadata{
		EvaluationJobID: job.Resource.ID,
		CreatedAt:       api.DateTimeToString(job.Resource.CreatedAt),
		UpdatedAt:       api.DateTimeToString(job.Resource.UpdatedAt),
	}
}

func newEvaluationCardContext(job *api.EvaluationJobResource) EvaluationCardContext {
	context := EvaluationCardContext{
		Model: CardModelRef{
			URL:          job.Model.URL,
			Name:         job.Model.Name,
			ModelCardURL: job.Model.CardURL,
		},
	}

	if job.Collection != nil && job.Collection.ID != "" {
		context.CollectionID = job.Collection.ID
		return context
	}

	for _, benchmark := range job.Benchmarks {
		context.Benchmarks = append(context.Benchmarks, toCardBenchmarkConfig(benchmark))
	}

	return context
}

func toCardBenchmarkConfig(benchmark api.EvaluationBenchmarkConfig) CardBenchmarkConfig {
	return CardBenchmarkConfig{
		ID:           benchmark.ID,
		ProviderID:   benchmark.ProviderID,
		Parameters:   benchmark.Parameters,
		PrimaryScore: benchmark.PrimaryScore,
		PassCriteria: benchmark.PassCriteria,
		Weight:       benchmark.Weight,
	}
}

func newEvaluationCardResults(job *api.EvaluationJobResource) *EvaluationCardResults {
	if job.Status == nil && (job.Results == nil || len(job.Results.Benchmarks) == 0) {
		return nil
	}

	results := &EvaluationCardResults{
		Status:     toCardJobStatus(job.Status),
		Benchmarks: buildCardBenchmarkResults(job),
	}

	if job.Collection != nil && job.Collection.ID != "" && job.Results != nil && job.Results.Test != nil {
		results.Collection = &CardCollectionResult{
			Test: &CardCollectionTest{
				Score:     job.Results.Test.Score,
				Threshold: job.Results.Test.Threshold,
				Pass:      job.Results.Test.Pass,
			},
		}
	}

	if results.Status == nil && len(results.Benchmarks) == 0 && results.Collection == nil {
		return nil
	}

	return results
}

func toCardJobStatus(status *api.EvaluationJobStatus) *CardJobStatus {
	if status == nil || status.State == "" {
		return nil
	}
	return &CardJobStatus{
		State:   status.State,
		Message: status.Message,
	}
}

func buildCardBenchmarkResults(job *api.EvaluationJobResource) []CardBenchmarkResult {
	resultByKey := map[string]api.BenchmarkResult{}
	if job.Results != nil {
		for _, result := range job.Results.Benchmarks {
			resultByKey[benchmarkKey(result.ID, result.ProviderID, result.BenchmarkIndex)] = result
		}
	}

	if job.Status == nil || len(job.Status.Benchmarks) == 0 {
		if job.Results == nil {
			return nil
		}
		cardResults := make([]CardBenchmarkResult, 0, len(job.Results.Benchmarks))
		for _, result := range job.Results.Benchmarks {
			cardResults = append(cardResults, toCardBenchmarkResult(result, ""))
		}
		return cardResults
	}

	cardResults := make([]CardBenchmarkResult, 0, len(job.Status.Benchmarks))
	for _, status := range job.Status.Benchmarks {
		result, ok := resultByKey[benchmarkKey(status.ID, status.ProviderID, status.BenchmarkIndex)]
		cardResult := CardBenchmarkResult{
			ID:             status.ID,
			ProviderID:     status.ProviderID,
			Status:         status.Status,
			ErrorMessage:   status.ErrorMessage,
			WarningMessage: status.WarningMessage,
		}
		if ok {
			cardResult.Contacts = result.Contacts
			cardResult.Metrics = result.Metrics
			cardResult.AdditionalInfo = result.AdditionalInfo
			cardResult.Artifacts = result.Artifacts
			cardResult.MLFlowRunID = result.MLFlowRunID
			cardResult.LogsPath = result.LogsPath
			cardResult.Test = toCardBenchmarkTest(result.Test)
		}
		cardResults = append(cardResults, cardResult)
	}

	return cardResults
}

func toCardBenchmarkResult(result api.BenchmarkResult, status api.State) CardBenchmarkResult {
	return CardBenchmarkResult{
		ID:             result.ID,
		ProviderID:     result.ProviderID,
		Contacts:       result.Contacts,
		Status:         status,
		Metrics:        result.Metrics,
		AdditionalInfo: result.AdditionalInfo,
		Artifacts:      result.Artifacts,
		MLFlowRunID:    result.MLFlowRunID,
		LogsPath:       result.LogsPath,
		Test:           toCardBenchmarkTest(result.Test),
	}
}

func toCardBenchmarkTest(test *api.BenchmarkTest) *CardBenchmarkTest {
	if test == nil {
		return nil
	}
	return &CardBenchmarkTest{
		PrimaryScore: formatCardScore(test.PrimaryScore),
		Threshold:    formatCardScore(test.Threshold),
		Pass:         test.Pass,
	}
}

func formatCardScore(value float32) string {
	return fmt.Sprintf("%g", value)
}

func benchmarkKey(id, providerID string, benchmarkIndex int) string {
	return fmt.Sprintf("%s/%s/%d", providerID, id, benchmarkIndex)
}
