package api

import (
	"fmt"
	"time"
)

// State represents the evaluation state enum
type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// IsBenchmarkTerminalState reports whether a benchmark state is terminal
// (completed, failed, or cancelled) and should not be overwritten.
func IsBenchmarkTerminalState(s State) bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

type JobPhase string

const (
	JobPhaseInitializing        JobPhase = "initializing"
	JobPhaseLoadingData         JobPhase = "loading_data"
	JobPhaseRunningEvaluation   JobPhase = "running_evaluation"
	JobPhasePostProcessing      JobPhase = "post_processing"
	JobPhasePersistingArtifacts JobPhase = "persisting_artifacts"
	JobPhaseCompleted           JobPhase = "completed"
)

type OverallState string

const (
	OverallStatePending         OverallState = OverallState(StatePending)
	OverallStateRunning         OverallState = OverallState(StateRunning)
	OverallStateCompleted       OverallState = OverallState(StateCompleted)
	OverallStateFailed          OverallState = OverallState(StateFailed)
	OverallStateCancelled       OverallState = OverallState(StateCancelled)
	OverallStatePartiallyFailed OverallState = "partially_failed"
)

func (o OverallState) String() string {
	return string(o)
}

func (o OverallState) IsTerminalState() bool {
	return o == OverallStateCompleted || o == OverallStateFailed || o == OverallStateCancelled || o == OverallStatePartiallyFailed
}

func GetOverallState(s string) (OverallState, error) {
	switch s {
	case string(OverallStatePending):
		return OverallStatePending, nil
	case string(OverallStateRunning):
		return OverallStateRunning, nil
	case string(OverallStateCompleted):
		return OverallStateCompleted, nil
	case string(OverallStateFailed):
		return OverallStateFailed, nil
	case string(OverallStateCancelled):
		return OverallStateCancelled, nil
	case string(OverallStatePartiallyFailed):
		return OverallStatePartiallyFailed, nil
	default:
		return OverallState(s), fmt.Errorf("invalid overall state: %s", s)
	}
}

// ModelRef represents model specification for evaluation requests
type ModelRef struct {
	URL        string         `json:"url" validate:"required"`
	Name       string         `json:"name" validate:"required"`
	Auth       *ModelAuth     `json:"auth,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
	CardURL    string         `json:"card_url,omitempty"`
}

type ModelAuth struct {
	SecretRef string `json:"secret_ref" validate:"required"`
}

// MessageOrigin represents the origin of a status or error message.
type MessageOrigin string

const (
	MessageOriginServer  MessageOrigin = "server"
	MessageOriginRuntime MessageOrigin = "runtime"
)

// MessageInfo represents a message from a downstream service
type MessageInfo struct {
	Message       string        `json:"message" validate:"required"`
	MessageCode   string        `json:"message_code" validate:"required"`
	MessageOrigin MessageOrigin `json:"message_origin,omitempty"`
}

// WithMessageOrigin sets origin on message and returns it (nil-safe).
func WithMessageOrigin(m *MessageInfo, origin MessageOrigin) *MessageInfo {
	if m != nil {
		m.MessageOrigin = origin
	}
	return m
}

// StampRuntimeMessageOrigins sets runtime origin on benchmark error and warning messages.
func (e *BenchmarkStatusEvent) StampRuntimeMessageOrigins() {
	if e == nil {
		return
	}
	WithMessageOrigin(e.ErrorMessage, MessageOriginRuntime)
	WithMessageOrigin(e.WarningMessage, MessageOriginRuntime)
}

type PrimaryScore struct {
	Metric        string `mapstructure:"metric" json:"metric" validate:"required"`
	LowerIsBetter bool   `mapstructure:"lower_is_better" json:"lower_is_better,omitempty" validate:"omitempty,boolean"`
}

type PassCriteria struct {
	// The *float32 is a hack to avoid validation failure when threshold=0
	Threshold *float32 `mapstructure:"threshold" json:"threshold" validate:"required"`
}

// S3TestDataRef represents S3 source for test data.
type S3TestDataRef struct {
	Bucket    string `json:"bucket" validate:"required"`
	Key       string `json:"key" validate:"required"`
	SecretRef string `json:"secret_ref" validate:"required"`
}

// PVCTestDataRef represents a PersistentVolumeClaim source for test data.
// The PVC must exist in the same namespace as the evaluation job and is mounted
// read-only at /test_data in the adapter container. No init container is used.
type PVCTestDataRef struct {
	ClaimName string `json:"claim_name" mapstructure:"claim_name" validate:"required,rfc1123_dns_label"`
	SubPath   string `json:"sub_path,omitempty" mapstructure:"sub_path,omitempty"`
}

// TestDataRef represents external test data sources.
// Exactly one of s3 or pvc must be set.
type TestDataRef struct {
	S3  *S3TestDataRef  `mapstructure:"s3" json:"s3,omitempty"`
	PVC *PVCTestDataRef `mapstructure:"pvc" json:"pvc,omitempty"`
}

type HardwareProfileRef struct {
	Name      string `mapstructure:"name" json:"name" validate:"required,rfc1123_dns_label"`
	Namespace string `mapstructure:"namespace" json:"namespace,omitempty" validate:"omitempty,rfc1123_dns_label"`
}

type BenchmarkHardwareConfig struct {
	HardwareProfileRef HardwareProfileRef `mapstructure:"hardware_profile_ref" json:"hardware_profile_ref,omitempty"`
}

// EvaluationBenchmarkConfig represents a benchmark reference in an evaluation job request or persisted job config.
type EvaluationBenchmarkConfig struct {
	Ref            `mapstructure:",squash"`
	ProviderID     string                   `mapstructure:"provider_id" json:"provider_id" validate:"required"`
	Weight         float32                  `mapstructure:"weight" json:"weight,omitempty" validate:"omitempty,min=0"`
	PrimaryScore   *PrimaryScore            `mapstructure:"primary_score" json:"primary_score,omitempty"`
	PassCriteria   *PassCriteria            `mapstructure:"pass_criteria" json:"pass_criteria,omitempty"`
	HardwareConfig *BenchmarkHardwareConfig `mapstructure:"hardware_config" json:"hardware_config,omitempty"`
	Parameters     map[string]any           `mapstructure:"parameters" json:"parameters,omitempty"`
	TestDataRef    *TestDataRef             `mapstructure:"test_data_ref" json:"test_data_ref,omitempty"`
}

// ExperimentTag represents a tag on an experiment
type ExperimentTag struct {
	Key   string `json:"key" validate:"required,max=250"`    // Keys can be up to 250 bytes in size (not characters) in mlflow experiments
	Value string `json:"value" validate:"required,max=5000"` // Values can be up to 5000 bytes in size (not characters) in mlflow experiments
}

// ExperimentConfig represents configuration for MLFlow experiment tracking
type ExperimentConfig struct {
	Name             string          `json:"name,omitempty" validate:"notblank"`
	Tags             []ExperimentTag `json:"tags,omitempty" validate:"omitempty,max=20,dive"`
	ArtifactLocation string          `json:"artifact_location,omitempty"`
}

// for marshalling and unmarshalling
type DateTime string

func DateTimeToString(date time.Time) DateTime {
	return DateTime(date.Format("2006-01-02T15:04:05Z07:00"))
}

func DateTimeFromString(date DateTime) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05Z07:00", string(date))
}

// BenchmarkStatus represents status of individual benchmark in evaluation
type BenchmarkStatus struct {
	ProviderID     string       `json:"provider_id"`
	ID             string       `json:"id"`
	BenchmarkIndex int          `json:"benchmark_index"`
	Status         State        `json:"status,omitempty"`
	Phase          JobPhase     `json:"phase,omitempty"`
	ErrorMessage   *MessageInfo `json:"error_message,omitempty"`
	WarningMessage *MessageInfo `json:"warning_message,omitempty"`
	StartedAt      DateTime     `json:"started_at,omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	CompletedAt    DateTime     `json:"completed_at,omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
}

// BenchmarkStatusEvent is used when the job runtime needs to update the status of a benchmark
type BenchmarkStatusEvent struct {
	ProviderID     string         `json:"provider_id" validate:"required"`
	ID             string         `json:"id" validate:"required"`
	BenchmarkIndex int            `json:"benchmark_index"`
	Status         State          `json:"status" validate:"required,oneof=pending running completed failed"`
	Phase          JobPhase       `json:"phase,omitempty" validate:"omitempty,oneof=initializing loading_data running_evaluation post_processing persisting_artifacts completed"`
	Metrics        map[string]any `json:"metrics,omitempty"`
	AdditionalInfo map[string]any `json:"additional_info,omitempty"`
	Artifacts      map[string]any `json:"artifacts,omitempty"`
	ErrorMessage   *MessageInfo   `json:"error_message,omitempty"`
	WarningMessage *MessageInfo   `json:"warning_message,omitempty"`
	StartedAt      DateTime       `json:"started_at,omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	CompletedAt    DateTime       `json:"completed_at,omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	MLFlowRunID    string         `json:"mlflow_run_id,omitempty"`
	LogsPath       string         `json:"logs_path,omitempty"`
}

type EvaluationJobState struct {
	State   OverallState `json:"state" validate:"required,oneof=pending running completed failed cancelled partially_failed"`
	Message *MessageInfo `json:"message" validate:"required"`
}

type StatusEvent struct {
	BenchmarkStatusEvent *BenchmarkStatusEvent `json:"benchmark_status_event" validate:"required"`
}

type BenchmarkResult struct {
	ID             string         `json:"id"`
	ProviderID     string         `json:"provider_id"`
	Contacts       []string       `json:"contacts,omitempty"`
	BenchmarkIndex int            `json:"benchmark_index"`
	Metrics        map[string]any `json:"metrics,omitempty"`
	AdditionalInfo map[string]any `json:"additional_info,omitempty"`
	Artifacts      map[string]any `json:"artifacts,omitempty"`
	MLFlowRunID    string         `json:"mlflow_run_id,omitempty"`
	LogsPath       string         `json:"logs_path,omitempty"`
	Test           *BenchmarkTest `json:"test,omitempty"`
}

// EvaluationJobResults represents results section for EvaluationJobResource
type EvaluationJobResults struct {
	Test                *EvaluationTest   `json:"test,omitempty"`
	Benchmarks          []BenchmarkResult `json:"benchmarks,omitempty" validate:"omitempty,dive"`
	MLFlowExperimentURL string            `json:"mlflow_experiment_url,omitempty"`
	CardURL             string            `json:"evaluation_card_url,omitempty"`
}

// OCICoordinates represents OCI artifact coordinates for persistence
type OCICoordinates struct {
	OCIHost       string            `json:"oci_host" validate:"required"`
	OCIRepository string            `json:"oci_repository" validate:"required"`
	OCITag        string            `json:"oci_tag,omitempty"`
	OCISubject    string            `json:"oci_subject,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// OCIConnectionConfig represents K8s connection configuration for OCI operations.
// Connection must reference a Kubernetes Secret containing a ".dockerconfigjson" entry,
// which provides standard Docker registry credentials for authenticating to the OCI registry.
type OCIConnectionConfig struct {
	// Connection is the name of a Kubernetes Secret (type kubernetes.io/dockerconfigjson)
	// with a ".dockerconfigjson" entry used for OCI registry authentication.
	Connection string `json:"connection" validate:"required"`
}

// EvaluationExportsOCI represents OCI export configuration
type EvaluationExportsOCI struct {
	Coordinates OCICoordinates       `json:"coordinates" validate:"required"`
	K8s         *OCIConnectionConfig `json:"k8s,omitempty"`
}

// EvaluationExports represents optional exports configuration for an evaluation job
type EvaluationExports struct {
	OCI *EvaluationExportsOCI `json:"oci,omitempty"`
}

type CollectionRef struct {
	ID         string                      `mapstructure:"id" json:"id" validate:"required"`
	Benchmarks []EvaluationBenchmarkConfig `json:"benchmarks,omitempty" validate:"omitempty,dive"`
}

// QueueConfig represents an optional scheduling queue for evaluation jobs.
// When Kind is empty, the evaluation job API handler normalizes it to "kueue" before persist/runtime.
type QueueConfig struct {
	Kind string `json:"kind,omitempty" validate:"omitempty,oneof=kueue"`
	Name string `json:"name" validate:"required,rfc1123_dns_label"`
}

// EvaluationJobConfig represents evaluation job request schema
type EvaluationJobConfig struct {
	Name         string                      `json:"name" validate:"required"`
	Description  *string                     `json:"description,omitempty"`
	Tags         []string                    `json:"tags,omitempty" validate:"omitempty,dive,tagname"`
	Model        ModelRef                    `json:"model" validate:"required"`
	PassCriteria *PassCriteria               `json:"pass_criteria,omitempty"`
	Benchmarks   []EvaluationBenchmarkConfig `json:"benchmarks,omitempty" validate:"omitempty,required_without=Collection,dive"`
	Collection   *CollectionRef              `json:"collection,omitempty" validate:"omitempty,required_without=Benchmarks"`
	Experiment   *ExperimentConfig           `json:"experiment,omitempty"`
	Custom       *map[string]any             `json:"custom,omitempty"`
	Exports      *EvaluationExports          `json:"exports,omitempty"`
	Queue        *QueueConfig                `json:"queue,omitempty"`
}

type EvaluationResource struct {
	Resource
	MLFlowExperimentID string `json:"mlflow_experiment_id,omitempty"`
}

type EvaluationJobStatus struct {
	EvaluationJobState
	Benchmarks []BenchmarkStatus `json:"benchmarks,omitempty"`
}

// EvaluationJobResource represents evaluation job resource response
type EvaluationJobResource struct {
	Resource EvaluationResource    `json:"resource"`
	Status   *EvaluationJobStatus  `json:"status,omitempty"`
	Results  *EvaluationJobResults `json:"results,omitempty"`
	EvaluationJobConfig
}

// EvaluationJobResourceList represents list of evaluation job resources with pagination
type EvaluationJobResourceList struct {
	Page
	Items  []EvaluationJobResource `json:"items"`
	Errors []string                `json:"errors,omitempty"`
}

type EvaluationTest struct {
	Score     float32 `json:"score"`
	Threshold float32 `json:"threshold"`
	Pass      bool    `json:"pass"`
}

type BenchmarkTest struct {
	PrimaryScore       float32 `json:"primary_score"`
	PrimaryScoreMetric string  `json:"primary_score_metric"`
	Threshold          float32 `json:"threshold"`
	Pass               bool    `json:"pass"`
}
