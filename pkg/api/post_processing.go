package api

// PostProcessingCommon contains fields shared by standalone and job-scoped
// post-processing requests and resources.
type PostProcessingCommon struct {
	Name           string                   `json:"name,omitempty" validate:"omitempty,notblank"`
	HardwareConfig *BenchmarkHardwareConfig `json:"hardware_config,omitempty"`
	OperationOrder []string                 `json:"operation_order,omitempty"` // Operation keys in execution order.
}

// StandalonePostProcessingRequest submits one or more post-processing
// operations against explicitly referenced result data.
type StandalonePostProcessingRequest struct {
	PostProcessingCommon
	Operations StandalonePostProcessingOperations `json:"operations" validate:"required"`
}

// StandalonePostProcessingOperations contains at most one operation of each
// supported kind. The operation key identifies its kind.
type StandalonePostProcessingOperations struct {
	ConfidenceInterval *StandaloneConfidenceIntervalConfig `json:"confidence_interval,omitempty" validate:"required"`
}

// HasOperation reports whether at least one supported standalone operation is set.
func (operations StandalonePostProcessingOperations) HasOperation() bool {
	return operations.ConfidenceInterval != nil
}

// OperationNames returns the names of the configured operation kinds.
func (operations StandalonePostProcessingOperations) OperationNames() []string {
	if operations.ConfidenceInterval == nil {
		return nil
	}
	return []string{"confidence_interval"}
}

// ConfidenceIntervalPostProcessingRequest submits operations against the
// evaluation job identified by the request path.
type ConfidenceIntervalPostProcessingRequest struct {
	PostProcessingCommon
	Operations JobPostProcessingOperations `json:"operations" validate:"required"`
}

// JobPostProcessingOperations contains at most one operation of each
// supported kind. The operation key identifies its kind.
type JobPostProcessingOperations struct {
	ConfidenceInterval *ConfidenceIntervalConfig `json:"confidence_interval,omitempty" validate:"required"`
}

// HasOperation reports whether at least one supported job-scoped operation is set.
func (operations JobPostProcessingOperations) HasOperation() bool {
	return operations.ConfidenceInterval != nil
}

// OperationNames returns the names of the configured operation kinds.
func (operations JobPostProcessingOperations) OperationNames() []string {
	if operations.ConfidenceInterval == nil {
		return nil
	}
	return []string{"confidence_interval"}
}

// ConfidenceIntervalConfigCommon holds calibration settings shared by
// standalone and job-scoped confidence interval operations.
type ConfidenceIntervalConfigCommon struct {
	CalibrationDataRef []CalibrationDataRef `json:"calibration_data_ref" validate:"required,min=1,dive"`
	SignificanceLevel  float64              `json:"significance_level" validate:"gt=0,lt=1"`
}

// ConfidenceIntervalConfig is the job-scoped operation configuration. The
// evaluation job reference is supplied by the URL and is added to the adapter
// job spec when the operation is mapped to a runtime job.
type ConfidenceIntervalConfig struct {
	ConfidenceIntervalConfigCommon
	NumParallelThreads *int `json:"num_parallel_threads,omitempty" validate:"omitempty,min=1"`
}

// StandaloneConfidenceIntervalConfig describes result and calibration data for
// a standalone confidence interval computation.
type StandaloneConfidenceIntervalConfig struct {
	ConfidenceIntervalConfigCommon
	ResultsDataRef *PostProcessingResultsDataRef `json:"results_data_ref" validate:"required"`
	PrimaryScore   *PrimaryScore                 `json:"primary_score,omitempty" validate:"required_without=ResultsDataRef.EvalJob,excluded_with=ResultsDataRef.EvalJob"`
}

// PostProcessingResultsDataRef accepts exactly one source. Calibration data
// uses CalibrationDataRef instead, excluding evaluation jobs, MLflow, and OCI.
type PostProcessingResultsDataRef struct {
	EvalJob *EvaluationJobDataRef `json:"eval_job,omitempty" validate:"required_without_all=S3 PVC Git HF MLFlow OCI,excluded_with=S3 PVC Git HF MLFlow OCI"`
	S3      *S3TestDataRef        `json:"s3,omitempty" validate:"required_without_all=EvalJob PVC Git HF MLFlow OCI,excluded_with=EvalJob PVC Git HF MLFlow OCI"`
	PVC     *PVCTestDataRef       `json:"pvc,omitempty" validate:"required_without_all=EvalJob S3 Git HF MLFlow OCI,excluded_with=EvalJob S3 Git HF MLFlow OCI"`
	Git     *GitTestDataRef       `json:"git,omitempty" validate:"required_without_all=EvalJob S3 PVC HF MLFlow OCI,excluded_with=EvalJob S3 PVC HF MLFlow OCI"`
	HF      *HFTestDataRef        `json:"hf,omitempty" validate:"required_without_all=EvalJob S3 PVC Git MLFlow OCI,excluded_with=EvalJob S3 PVC Git MLFlow OCI"`
	MLFlow  *MLflowDataRef        `json:"mlflow,omitempty" validate:"required_without_all=EvalJob S3 PVC Git HF OCI,excluded_with=EvalJob S3 PVC Git HF OCI"`
	OCI     *OCIDataRef           `json:"oci,omitempty" validate:"required_without_all=EvalJob S3 PVC Git HF MLFlow,excluded_with=EvalJob S3 PVC Git HF MLFlow"`
}

// CalibrationDataRef describes one labeled calibration dataset and its
// logical-to-physical column mapping.
type CalibrationDataRef struct {
	S3         *S3TestDataRef        `json:"s3,omitempty" validate:"required_without_all=PVC Git HF,excluded_with=PVC Git HF"`
	PVC        *PVCTestDataRef       `json:"pvc,omitempty" validate:"required_without_all=S3 Git HF,excluded_with=S3 Git HF"`
	Git        *GitTestDataRef       `json:"git,omitempty" validate:"required_without_all=S3 PVC HF,excluded_with=S3 PVC HF"`
	HF         *HFTestDataRef        `json:"hf,omitempty" validate:"required_without_all=S3 PVC Git,excluded_with=S3 PVC Git"`
	DataConfig CalibrationDataConfig `json:"data_config" validate:"required"`
}

type CalibrationDataConfig struct {
	Format    string                    `json:"format" validate:"required"`
	Columns   CalibrationDataColumns    `json:"columns" validate:"required"`
	Selection *CalibrationDataSelection `json:"selection,omitempty"`
}

type CalibrationDataColumns struct {
	SampleID    string `json:"sample_id,omitempty"`
	Label       string `json:"label" validate:"required"`
	Prediction  string `json:"prediction" validate:"required"`
	BenchmarkID string `json:"benchmark_id,omitempty"`
	ProviderID  string `json:"provider_id,omitempty"`
}

type CalibrationDataSelection struct {
	Metric string `json:"metric" validate:"required"`
}

type EvaluationJobDataRef struct {
	ID                 string `json:"id" validate:"notblank"`
	NumParallelThreads *int   `json:"num_parallel_threads,omitempty" validate:"omitempty,min=1"`
}

type MLflowDataRef struct {
	RunID        string `json:"run_id" validate:"notblank"`
	ArtifactPath string `json:"artifact_path" validate:"notblank"`
}

type OCIDataRef struct {
	Coordinates  OCICoordinates       `json:"coordinates" validate:"required"`
	Digest       string               `json:"digest,omitempty" validate:"omitempty,sha256_digest"`
	ArtifactPath string               `json:"artifact_path" validate:"notblank"`
	K8s          *OCIConnectionConfig `json:"k8s,omitempty"`
}

// PostProcessingResource is the standalone computation resource.
type PostProcessingResource struct {
	Resource Resource `json:"resource"`
	PostProcessingCommon
	Operations StandalonePostProcessingOperations `json:"operations"`
	Status     PostProcessingStatus               `json:"status"`
	Results    *PostProcessingResults             `json:"results,omitempty"`
}

// JobPostProcessingResource is a computation associated with a source
// evaluation job.
type JobPostProcessingResource struct {
	Resource Resource `json:"resource"`
	PostProcessingCommon
	Operations JobPostProcessingOperations `json:"operations"`
	Status     PostProcessingStatus        `json:"status"`
	Results    *PostProcessingResults      `json:"results,omitempty"`
}

type PostProcessingStatus struct {
	State          State             `json:"state"`
	ErrorMessage   *MessageInfo      `json:"error_message,omitempty"`
	WarningMessage *MessageInfo      `json:"warning_message,omitempty"`
	StartedAt      DateTime          `json:"started_at,omitempty"`
	CompletedAt    DateTime          `json:"completed_at,omitempty"`
	Benchmarks     []BenchmarkStatus `json:"benchmarks,omitempty"`
}

// PostProcessingResults contains per-benchmark results for evaluation-job
// inputs or an aggregate interval for external inputs.
type PostProcessingResults struct {
	Benchmarks         []PostProcessingBenchmarkResult `json:"benchmarks,omitempty"`
	ConfidenceInterval *ConfidenceInterval             `json:"confidence_interval,omitempty"`
}

type PostProcessingBenchmarkResult struct {
	ID                 string             `json:"id"`
	ProviderID         string             `json:"provider_id"`
	BenchmarkIndex     int                `json:"benchmark_index"`
	ConfidenceInterval ConfidenceInterval `json:"confidence_interval"`
}

type ConfidenceInterval struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
}

type PostProcessingRef struct {
	ID string `json:"id"`
}
