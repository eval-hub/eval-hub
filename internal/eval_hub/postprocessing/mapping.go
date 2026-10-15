// Package postprocessing maps the post-processing API to ordinary evaluation jobs.
package postprocessing

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	ProviderID          = "evalhub-internal"
	BenchmarkID         = "evaluation-post-processor"
	workloadType        = workloads.PostProcessing
	defaultAdapterImage = "quay.io/evalhub/evalhub-post-processor:latest"
	adapterImageEnv     = "EVALHUB_POST_PROCESSING_IMAGE"
	localAdapterDir     = "../eval-hub-contrib/adapters/evalhub-post-processor"
	defaultLocalCommand = localAdapterDir + "/.venv/bin/python " + localAdapterDir + "/main.py"
	localCommandEnv     = "EVALHUB_POST_PROCESSING_LOCAL_COMMAND"
)

func init() {
	workloads.Register(workloads.Workload{
		Type:            workloadType,
		ProviderID:      ProviderID,
		BenchmarkID:     BenchmarkID,
		Internal:        true,
		MatchesJob:      IsPostProcessingJob,
		RuntimeProvider: RuntimeProvider,
	})
}

// RuntimeProvider builds the in-memory execution descriptor used by runtimes
// for post-processing. The image comes from the environment or the built-in
// latest image; Kubernetes job creation supplies default resource values. The
// local command runs the adapter from a sibling eval-hub-contrib checkout unless
// overridden for a different local layout.
func RuntimeProvider() *api.ProviderResource {
	image := strings.TrimSpace(os.Getenv(adapterImageEnv))
	if image == "" {
		image = defaultAdapterImage
	}
	pullPolicy := ""
	if strings.HasSuffix(image, ":latest") {
		pullPolicy = "always"
	}
	localCommand := strings.TrimSpace(os.Getenv(localCommandEnv))
	if localCommand == "" {
		localCommand = defaultLocalCommand
	}

	return &api.ProviderResource{
		Resource: api.Resource{ID: ProviderID},
		ProviderConfig: api.ProviderConfig{
			Name:  "EvalHub Internal",
			Title: "EvalHub Internal",
			Runtime: &api.Runtime{
				K8s:   &api.K8sRuntime{Image: image, ImagePullPolicy: pullPolicy},
				Local: &api.LocalRuntime{Command: localCommand},
			},
			Benchmarks: []api.BenchmarkResource{{
				ID: BenchmarkID,
			}},
		},
	}
}

// IsPostProcessingJob recognizes the single benchmark used for post-processing.
func IsPostProcessingJob(cfg *api.EvaluationJobConfig) bool {
	return cfg != nil && cfg.Collection == nil && len(cfg.Benchmarks) == 1 &&
		cfg.Benchmarks[0].ProviderID == ProviderID && cfg.Benchmarks[0].ID == BenchmarkID
}

// ToEvaluationJob maps a validated standalone request, preserving the
// operations object and optional operation order for the adapter.
func ToEvaluationJob(request *api.StandalonePostProcessingRequest) *api.EvaluationJobConfig {
	name := request.Name
	if name == "" {
		name = "post-processing"
	}
	parameters := map[string]any{
		"operations": request.Operations,
	}
	if request.OperationOrder != nil {
		// Preserve whether the caller specified an order so the public resource
		// does not imply an order when none was requested.
		parameters["operation_order"] = request.OperationOrder
	}
	return &api.EvaluationJobConfig{
		Name:           name,
		Model:          &api.ModelRef{Name: BenchmarkID},
		HardwareConfig: request.HardwareConfig,
		Benchmarks: []api.EvaluationBenchmarkConfig{{
			Ref:        api.Ref{ID: BenchmarkID},
			ProviderID: ProviderID,
			Parameters: parameters,
		}},
	}
}

// OperationsFromJob decodes the operations object stored in the adapter
// parameters of an evaluation job.
func OperationsFromJob(job *api.EvaluationJobConfig) (api.StandalonePostProcessingOperations, error) {
	if !IsPostProcessingJob(job) {
		return api.StandalonePostProcessingOperations{}, fmt.Errorf("job is not a post-processing computation")
	}
	parameters := job.Benchmarks[0].Parameters
	rawOperations, ok := parameters["operations"]
	if !ok {
		return api.StandalonePostProcessingOperations{}, fmt.Errorf("post-processing job has no operations")
	}
	data, err := json.Marshal(rawOperations)
	if err != nil {
		return api.StandalonePostProcessingOperations{}, fmt.Errorf("marshal post-processing operations: %w", err)
	}
	var operations api.StandalonePostProcessingOperations
	if err := json.Unmarshal(data, &operations); err != nil {
		return api.StandalonePostProcessingOperations{}, fmt.Errorf("decode post-processing operations: %w", err)
	}
	if !operations.HasOperation() {
		return api.StandalonePostProcessingOperations{}, fmt.Errorf("post-processing job has no supported operations")
	}
	return operations, nil
}

// OperationOrderFromJob restores the optional operation order saved with the
// adapter parameters.
func OperationOrderFromJob(job *api.EvaluationJobConfig) ([]string, error) {
	if !IsPostProcessingJob(job) {
		return nil, fmt.Errorf("job is not a post-processing computation")
	}
	parameters := job.Benchmarks[0].Parameters
	rawOrder, ok := parameters["operation_order"]
	if !ok {
		return nil, nil
	}
	data, err := json.Marshal(rawOrder)
	if err != nil {
		return nil, fmt.Errorf("marshal post-processing operation order: %w", err)
	}
	var order []string
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("decode post-processing operation order: %w", err)
	}
	return order, nil
}

// ResourceFromJob builds the submission response without exposing the internal
// model or treating the execution benchmark as a benchmark of the source job.
func ResourceFromJob(job *api.EvaluationJobResource) (*api.PostProcessingResource, error) {
	operations, err := OperationsFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, err
	}
	operationOrder, err := OperationOrderFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, err
	}
	results, err := resultsFromJob(job, operations.ConfidenceInterval)
	if err != nil {
		return nil, err
	}
	resource := &api.PostProcessingResource{
		Resource: job.Resource.Resource,
		PostProcessingCommon: api.PostProcessingCommon{
			Name:           job.Name,
			HardwareConfig: job.HardwareConfig,
			OperationOrder: operationOrder,
		},
		Operations: operations,
		Status:     api.PostProcessingStatus{State: api.StatePending},
		Results:    results,
	}
	resource.Status = postProcessingStatusFromJob(job)
	return resource, nil
}

// JobResourceFromJob builds the job-scoped response, omitting the source
// reference because it is represented by the request path.
func JobResourceFromJob(job *api.EvaluationJobResource) (*api.JobPostProcessingResource, error) {
	operations, err := OperationsFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, err
	}
	operation := operations.ConfidenceInterval
	if operation == nil || operation.ResultsDataRef == nil || operation.ResultsDataRef.EvalJob == nil {
		return nil, fmt.Errorf("post-processing job does not reference an evaluation job")
	}
	operationOrder, err := OperationOrderFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, err
	}
	results, err := resultsFromJob(job, operation)
	if err != nil {
		return nil, err
	}
	return &api.JobPostProcessingResource{
		Resource: job.Resource.Resource,
		PostProcessingCommon: api.PostProcessingCommon{
			Name:           job.Name,
			HardwareConfig: job.HardwareConfig,
			OperationOrder: operationOrder,
		},
		Operations: api.JobPostProcessingOperations{
			ConfidenceInterval: &api.ConfidenceIntervalConfig{
				ConfidenceIntervalConfigCommon: operation.ConfidenceIntervalConfigCommon,
				ResultsDataConfig:              operation.ResultsDataRef.DataConfig,
				NumParallelThreads:             operation.ResultsDataRef.EvalJob.NumParallelThreads,
			},
		},
		Status:  postProcessingStatusFromJob(job),
		Results: results,
	}, nil
}

func postProcessingStatusFromJob(job *api.EvaluationJobResource) api.PostProcessingStatus {
	status := api.PostProcessingStatus{State: api.StatePending}
	if job.Status == nil {
		return status
	}
	status.State = api.State(job.Status.State)
	if len(job.Status.Benchmarks) == 1 {
		benchmark := job.Status.Benchmarks[0]
		status.ErrorMessage = benchmark.ErrorMessage
		status.WarningMessage = benchmark.WarningMessage
		status.StartedAt = benchmark.StartedAt
		status.CompletedAt = benchmark.CompletedAt
	}
	return status
}

// resultsFromJob maps the post-processor's operation output from the backing
// evaluation job into the result shape selected by the standalone request.
func resultsFromJob(job *api.EvaluationJobResource, operation *api.StandaloneConfidenceIntervalConfig) (*api.PostProcessingResults, error) {
	if job.Results == nil || operation == nil || operation.ResultsDataRef == nil {
		return nil, nil
	}

	usesEvaluationJobResults := operation.ResultsDataRef.EvalJob != nil
	results := &api.PostProcessingResults{}
	for _, benchmark := range job.Results.Benchmarks {
		raw, ok := benchmark.AdditionalInfo["confidence_interval"]
		if !ok {
			continue
		}

		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("marshal post-processing confidence interval results: %w", err)
		}
		var output struct {
			Benchmarks         []api.PostProcessingBenchmarkResult `json:"benchmarks"`
			ConfidenceInterval *api.ConfidenceInterval             `json:"confidence_interval"`
		}
		if err := json.Unmarshal(data, &output); err != nil {
			return nil, fmt.Errorf("decode post-processing confidence interval results: %w", err)
		}

		if usesEvaluationJobResults {
			results.Benchmarks = append(results.Benchmarks, output.Benchmarks...)
		} else if output.ConfidenceInterval != nil {
			results.ConfidenceInterval = output.ConfidenceInterval
		}
	}

	if usesEvaluationJobResults && len(results.Benchmarks) > 0 {
		return results, nil
	}
	if !usesEvaluationJobResults && results.ConfidenceInterval != nil {
		return results, nil
	}
	return nil, nil
}
