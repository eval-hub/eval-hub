// Package generation owns the durable collection-generation job representation and its
// internal runtime workload registration. It deliberately does not expose public REST routes.
package generation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
	yamlv3 "go.yaml.in/yaml/v3"
	"sigs.k8s.io/yaml"
)

const (
	ProviderID            = "evalhub-collection-generation"
	BenchmarkID           = "collection-generation"
	ConfigParameter       = "collection-generation-config"
	legacyConfigParameter = "collection_generation_config"
	RunnerEntrypoint      = "/app/eval-generation-worker"
	maxConfigBytes        = 64 * 1024
	maxResultBytes        = 512 * 1024
	LaunchFailureMessage  = "The collection-generation worker could not be started."
)

func init() {
	workloads.Register(workloads.Workload{
		Type:                  workloads.CollectionGeneration,
		ProviderID:            ProviderID,
		BenchmarkID:           BenchmarkID,
		Internal:              true,
		MatchesJob:            IsJob,
		RuntimeProviderForJob: RuntimeProviderForJob,
	})
}

// JobConfig is the admission-time immutable runner snapshot persisted with the job.
// It contains only the normalized request and trusted controls, never Secret contents.
type JobConfig struct {
	Request         api.CollectionGenerationRequest `json:"request"`
	Limits          loop.Limits                     `json:"limits"`
	ReasoningEffort string                          `json:"reasoning_effort,omitempty"`
	RunnerImage     string                          `json:"runner_image"`
	Retention       time.Duration                   `json:"retention"`
}

// NormalizeRequest applies API defaults and removes model fields that are not part of
// the generation connection contract (including caller-controlled model parameters).
func NormalizeRequest(request api.CollectionGenerationRequest) (api.CollectionGenerationRequest, error) {
	request.EvaluationGoal = strings.TrimSpace(request.EvaluationGoal)
	if request.EvaluationGoal == "" {
		return api.CollectionGenerationRequest{}, errors.New("evaluation_goal is required")
	}
	if utf8.RuneCountInString(request.EvaluationGoal) > api.MaxCollectionGenerationGoalLength {
		return api.CollectionGenerationRequest{}, fmt.Errorf("evaluation_goal exceeds %d characters", api.MaxCollectionGenerationGoalLength)
	}
	request.CollectionName = strings.TrimSpace(request.CollectionName)
	request.CollectionDescription = strings.TrimSpace(request.CollectionDescription)
	if utf8.RuneCountInString(request.CollectionDescription) > 1024 {
		return api.CollectionGenerationRequest{}, errors.New("collection_description exceeds 1024 characters")
	}
	request.ProviderFilter = strings.TrimSpace(request.ProviderFilter)
	if modelParametersContainCredentials(request.Model.Parameters) {
		return api.CollectionGenerationRequest{}, errors.New("model parameters must not contain credentials; use model.auth.secret_ref")
	}
	modelAuth := request.Model.Auth
	request.Model = api.ModelRef{
		URL:  strings.TrimSpace(request.Model.URL),
		Name: strings.TrimSpace(request.Model.Name),
	}
	if request.Model.Name == "" || request.Model.URL == "" {
		return api.CollectionGenerationRequest{}, errors.New("generator model name and URL are required")
	}
	parsedModelURL, err := url.Parse(request.Model.URL)
	if err != nil || (parsedModelURL.Scheme != "http" && parsedModelURL.Scheme != "https") || parsedModelURL.Host == "" || parsedModelURL.User != nil || parsedModelURL.Fragment != "" {
		return api.CollectionGenerationRequest{}, errors.New("generator model URL must be HTTP(S) without embedded credentials")
	}
	query, err := url.ParseQuery(parsedModelURL.RawQuery)
	if err != nil {
		return api.CollectionGenerationRequest{}, errors.New("generator model URL query is invalid")
	}
	for key := range query {
		if isCredentialKey(key) {
			return api.CollectionGenerationRequest{}, errors.New("generator model URL must not contain credential query parameters")
		}
	}
	if request.ProviderFilter == "," || strings.HasPrefix(request.ProviderFilter, ",") || strings.HasSuffix(request.ProviderFilter, ",") || strings.Contains(request.ProviderFilter, ",,") {
		return api.CollectionGenerationRequest{}, errors.New("provider_filter must contain comma-separated provider IDs")
	}
	if request.ProviderFilter != "" {
		providers := strings.Split(request.ProviderFilter, ",")
		for i := range providers {
			providers[i] = strings.TrimSpace(providers[i])
			if providers[i] == "" {
				return api.CollectionGenerationRequest{}, errors.New("provider_filter must contain comma-separated provider IDs")
			}
		}
		request.ProviderFilter = strings.Join(providers, ",")
	}
	// Preserve only the Secret reference. Model parameters and card URLs are not part of
	// the generation control surface and must not become model-controlled runner settings.
	if auth := modelAuth; auth != nil {
		secretRef := strings.TrimSpace(auth.SecretRef)
		if secretRef == "" {
			return api.CollectionGenerationRequest{}, errors.New("model.auth.secret_ref must not be blank")
		}
		request.Model.Auth = &api.ModelAuth{SecretRef: secretRef}
	}
	if request.MaxBenchmarks == 0 {
		request.MaxBenchmarks = api.DefaultCollectionGenerationBenchmarks
	}
	if request.Strictness == "" {
		request.Strictness = "moderate"
	}
	if request.GenerationEffort == "" {
		request.GenerationEffort = string(loop.EffortStandard)
	}
	if request.MaxBenchmarks < 1 || request.MaxBenchmarks > api.MaxCollectionGenerationBenchmarks {
		return api.CollectionGenerationRequest{}, fmt.Errorf("max_benchmarks must be between 1 and %d", api.MaxCollectionGenerationBenchmarks)
	}
	if request.Strictness != "lenient" && request.Strictness != "moderate" && request.Strictness != "strict" {
		return api.CollectionGenerationRequest{}, errors.New("strictness must be lenient, moderate, or strict")
	}
	if request.GenerationEffort != string(loop.EffortLow) && request.GenerationEffort != string(loop.EffortStandard) && request.GenerationEffort != string(loop.EffortHigh) {
		return api.CollectionGenerationRequest{}, errors.New("generation_effort must be low, standard, or high")
	}
	return request, nil
}

func isCredentialKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	return normalized == "auth" || normalized == "authorization" || normalized == "cookie" ||
		normalized == "key" || normalized == "proxyauthorization" || normalized == "sig" ||
		strings.Contains(normalized, "apikey") || strings.Contains(normalized, "accesskey") ||
		strings.Contains(normalized, "privatekey") || strings.Contains(normalized, "signature") ||
		strings.HasSuffix(normalized, "token") || strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "password") || strings.Contains(normalized, "credential")
}

func modelParametersContainCredentials(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if isCredentialKey(key) || modelParametersContainCredentials(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if modelParametersContainCredentials(nested) {
				return true
			}
		}
	}
	return false
}

// ValidateResult enforces the bounded dry-run proposal contract before a worker
// result is persisted or returned to a tenant.
func ValidateResult(request api.CollectionGenerationRequest, result api.CollectionGenerationResult) error {
	if strings.TrimSpace(result.Collection.Name) == "" || strings.TrimSpace(result.YAML) == "" || strings.TrimSpace(result.Rationale) == "" {
		return errors.New("proposal requires a named collection, YAML, and rationale")
	}
	if request.CollectionName != "" && result.Collection.Name != request.CollectionName {
		return errors.New("proposal collection name does not match the requested name")
	}
	if request.CollectionDescription != "" && result.Collection.Description != request.CollectionDescription {
		return errors.New("proposal collection description does not match the requested description")
	}
	if result.Collection.CurationOrder != 0 {
		return errors.New("proposal cannot set the server-managed curation order")
	}
	if len(result.YAML) > 256*1024 || len(result.Rationale) > 10_000 || len(result.Collection.Benchmarks) == 0 || len(result.Collection.Benchmarks) > request.MaxBenchmarks {
		return errors.New("proposal exceeds size or benchmark limits")
	}
	if len(result.Explanation.SelectedBenchmarks) != len(result.Collection.Benchmarks) || len(result.Explanation.RejectedBenchmarks) > 5 || len(result.Actions) > 50 {
		return errors.New("proposal explanations or actions do not match the configured limits")
	}
	validator, err := validation.NewValidator()
	if err != nil {
		return errors.New("collection schema validator is unavailable")
	}
	if err := validator.Struct(result.Collection); err != nil {
		return errors.New("proposal collection does not satisfy the collection schema")
	}
	if err := validateYAMLMatchesCollection(result.YAML, result.Collection); err != nil {
		return err
	}
	selected := make(map[string]struct{}, len(result.Collection.Benchmarks))
	for _, benchmark := range result.Collection.Benchmarks {
		if strings.TrimSpace(benchmark.ID) == "" || strings.TrimSpace(benchmark.ProviderID) == "" || benchmark.URL != "" {
			return errors.New("proposal contains an incomplete benchmark reference")
		}
		if !providerAllowed(request.ProviderFilter, benchmark.ProviderID) {
			return errors.New("proposal contains a benchmark outside the requested provider filter")
		}
		key := benchmark.ProviderID + "\x00" + benchmark.ID
		if _, exists := selected[key]; exists {
			return errors.New("proposal contains a duplicate benchmark")
		}
		selected[key] = struct{}{}
	}
	for i, reason := range result.Explanation.SelectedBenchmarks {
		benchmark := result.Collection.Benchmarks[i]
		key := reason.ProviderID + "\x00" + reason.ID
		if key != benchmark.ProviderID+"\x00"+benchmark.ID || strings.TrimSpace(reason.Reason) == "" || len(reason.Reason) > 300 {
			return errors.New("selected benchmark explanation does not match the proposal")
		}
	}
	rejected := make(map[string]struct{}, len(result.Explanation.RejectedBenchmarks))
	for _, reason := range result.Explanation.RejectedBenchmarks {
		if strings.TrimSpace(reason.ProviderID) == "" || strings.TrimSpace(reason.ID) == "" || strings.TrimSpace(reason.Reason) == "" || len(reason.Reason) > 300 {
			return errors.New("proposal contains an invalid rejected benchmark explanation")
		}
		if !providerAllowed(request.ProviderFilter, reason.ProviderID) {
			return errors.New("proposal rejects a benchmark outside the requested provider filter")
		}
		key := reason.ProviderID + "\x00" + reason.ID
		if _, exists := rejected[key]; exists {
			return errors.New("proposal contains a duplicate rejected benchmark explanation")
		}
		if _, exists := selected[key]; exists {
			return errors.New("a benchmark cannot be both selected and rejected")
		}
		rejected[key] = struct{}{}
	}
	for _, action := range result.Actions {
		if !isAllowedAction(action.Name) || (action.Outcome != "success" && action.Outcome != "error") || strings.TrimSpace(action.Summary) == "" || len(action.Summary) > 300 {
			return errors.New("proposal contains an invalid action summary")
		}
	}
	return nil
}

func providerAllowed(filter, providerID string) bool {
	if filter == "" {
		return true
	}
	for _, candidate := range strings.Split(filter, ",") {
		if strings.TrimSpace(candidate) == providerID {
			return true
		}
	}
	return false
}

func validateYAMLMatchesCollection(content string, collection api.CollectionConfig) error {
	documents := yamlv3.NewDecoder(strings.NewReader(content))
	var firstDocument yamlv3.Node
	if err := documents.Decode(&firstDocument); err != nil {
		return errors.New("proposal YAML is invalid")
	}
	var extraDocument yamlv3.Node
	if err := documents.Decode(&extraDocument); err != io.EOF {
		return errors.New("proposal YAML must contain exactly one collection configuration")
	}
	yamlJSON, err := yaml.YAMLToJSONStrict([]byte(content))
	if err != nil {
		return errors.New("proposal YAML is invalid")
	}
	var fromYAML api.CollectionConfig
	decoder := json.NewDecoder(bytes.NewReader(yamlJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fromYAML); err != nil {
		return errors.New("proposal YAML does not contain a collection configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("proposal YAML must contain exactly one collection configuration")
	}
	jsonConfig, err := json.Marshal(collection)
	if err != nil {
		return errors.New("proposal collection cannot be serialized")
	}
	yamlConfig, err := json.Marshal(fromYAML)
	if err != nil {
		return errors.New("proposal YAML collection cannot be serialized")
	}
	if !bytes.Equal(jsonConfig, yamlConfig) {
		return errors.New("proposal YAML does not match the collection configuration")
	}
	return nil
}

// SafeFailure constructs a tenant-visible failure from a runner code. The runner's
// free-form message is ignored so model output, prompts, and credentials cannot leak.
func SafeFailure(code, category string) (api.CollectionGenerationError, error) {
	messages := map[string]string{
		"guardrails_rejected":    "The request was rejected by generation safety checks.",
		"guardrails_unavailable": "Generation safety checks are temporarily unavailable.",
		"model_failed":           "The generation model request failed.",
		"tool_failed":            "The generation service encountered a tool error.",
		"validation_failed":      "The generated proposal did not pass validation.",
		"no_relevant_benchmarks": "No relevant benchmarks were found in the visible catalog.",
		"timeout":                "The generation job exceeded its configured time limit.",
		"launch_failed":          LaunchFailureMessage,
		"workload_failed":        "The collection-generation worker failed.",
	}
	message, ok := messages[code]
	if !ok {
		return api.CollectionGenerationError{}, fmt.Errorf("unsupported collection-generation failure code %q", code)
	}
	if code == "guardrails_rejected" && category == "" {
		return api.CollectionGenerationError{}, errors.New("guardrail rejections require a failure category")
	}
	if code != "guardrails_rejected" && category != "" {
		return api.CollectionGenerationError{}, errors.New("only guardrail rejections can include a failure category")
	}
	if category != "" && category != "prompt_injection" && category != "exfiltration" && category != "topic_relevance" && category != "unsupported_capability" {
		return api.CollectionGenerationError{}, errors.New("unsupported collection-generation failure category")
	}
	return api.CollectionGenerationError{Code: code, Message: message, Category: category}, nil
}

// ResultFromAdditionalInfo decodes and validates the one typed result accepted
// from a completed generation worker event.
func ResultFromAdditionalInfo(request api.CollectionGenerationRequest, info map[string]any) (api.CollectionGenerationResult, error) {
	rawValue, ok := info["collection_generation_result"]
	if !ok {
		return api.CollectionGenerationResult{}, errors.New("completed generation event has no proposal result")
	}
	raw, err := json.Marshal(rawValue)
	if err != nil || len(raw) > maxResultBytes {
		return api.CollectionGenerationResult{}, errors.New("generation proposal is malformed or exceeds the size limit")
	}
	var result api.CollectionGenerationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return api.CollectionGenerationResult{}, errors.New("generation proposal is malformed")
	}
	// These fields are required arrays in the public result schema. Normalize absent
	// optional collections so JSON encodes [] rather than null.
	if result.Explanation.RejectedBenchmarks == nil {
		result.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{}
	}
	if result.Actions == nil {
		result.Actions = []api.CollectionGenerationAction{}
	}
	if err := ValidateResult(request, result); err != nil {
		return api.CollectionGenerationResult{}, err
	}
	for i := range result.Actions {
		result.Actions[i].Summary = safeActionSummary(result.Actions[i].Name, result.Actions[i].Outcome)
	}
	normalized, err := json.Marshal(result)
	if err != nil || len(normalized) > maxResultBytes {
		return api.CollectionGenerationResult{}, errors.New("generation proposal is malformed or exceeds the size limit")
	}
	return result, nil
}

func safeActionSummary(name, outcome string) string {
	verb := "completed"
	if outcome == "error" {
		verb = "could not be completed"
	}
	switch name {
	case "guardrails_check":
		return "Generation safety checks " + verb + "."
	case "search_benchmarks":
		return "Visible benchmark catalog search " + verb + "."
	case "get_provider_details":
		return "Visible provider details lookup " + verb + "."
	case "list_collections":
		return "Visible collection catalog lookup " + verb + "."
	case "validate_proposal":
		return "Collection proposal validation " + verb + "."
	case "calibrate_proposal":
		return "Collection proposal calibration " + verb + "."
	case "finalize_proposal":
		return "Collection proposal finalization " + verb + "."
	default:
		return "Generation action " + verb + "."
	}
}

// FailureFromAdditionalInfo accepts only a failure code/category and replaces
// free-form text with a server-owned safe message.
func FailureFromAdditionalInfo(info map[string]any) (api.CollectionGenerationError, error) {
	rawValue, ok := info["collection_generation_error"]
	if !ok {
		return api.CollectionGenerationError{}, errors.New("failed generation event has no structured error")
	}
	raw, err := json.Marshal(rawValue)
	if err != nil || len(raw) > 4096 {
		return api.CollectionGenerationError{}, errors.New("generation error is malformed")
	}
	var supplied struct {
		Code     string `json:"code"`
		Category string `json:"category,omitempty"`
	}
	if err := json.Unmarshal(raw, &supplied); err != nil {
		return api.CollectionGenerationError{}, errors.New("generation error is malformed")
	}
	return SafeFailure(supplied.Code, supplied.Category)
}

func isAllowedAction(name string) bool {
	switch name {
	case "guardrails_check", "search_benchmarks", "get_provider_details", "list_collections", "validate_proposal", "calibrate_proposal", "finalize_proposal":
		return true
	default:
		return false
	}
}

// ValidateProgressEvent accepts only server-supported monotonic phases and actions.
func ValidateProgressEvent(event api.CollectionGenerationProgressEvent, previous *api.CollectionGenerationProgress) error {
	phaseOrder := map[string]int{
		"guardrails": 0, "catalog_discovery": 1, "model_generation": 2, "validation": 3, "finalization": 4,
	}
	phase, ok := phaseOrder[event.Phase]
	if event.Sequence == 0 || !ok {
		return errors.New("progress sequence and phase are invalid")
	}
	if event.LastCompletedAction != "" && !isAllowedAction(event.LastCompletedAction) {
		return errors.New("progress action is not allowlisted")
	}
	if previous != nil {
		previousPhase, exists := phaseOrder[previous.Phase]
		correctionCycle := previous.Phase == "validation" && event.Phase == "model_generation"
		if event.Sequence <= previous.Sequence || !exists || (phase < previousPhase && !correctionCycle) {
			return errors.New("progress sequence must increase and phases must not move backward")
		}
	}
	return nil
}

// ProgressFromEvent creates progress with server-owned timestamps.
func ProgressFromEvent(event api.CollectionGenerationProgressEvent, previous *api.CollectionGenerationProgress, now time.Time) api.CollectionGenerationProgress {
	progress := api.CollectionGenerationProgress{
		Sequence: event.Sequence,
		CollectionGenerationStatusProgress: api.CollectionGenerationStatusProgress{
			Phase: event.Phase, UpdatedAt: now.UTC(),
		},
	}
	if event.LastCompletedAction != "" {
		progress.LastCompletedAction = &api.CollectionGenerationProgressAction{Name: event.LastCompletedAction, CompletedAt: now.UTC()}
	} else if previous != nil {
		progress.LastCompletedAction = previous.LastCompletedAction
	}
	return progress
}

// NewJob creates the one-row backing evaluation job for an admitted generation.
func NewJob(id string, tenant api.Tenant, owner api.User, request api.CollectionGenerationRequest, runnerImage string, budget loop.Budget, reasoningEffort string, retention time.Duration, now time.Time) (*api.EvaluationJobResource, error) {
	if id == "" || tenant.IsEmpty() || owner == "" {
		return nil, errors.New("job ID, tenant, and owner are required")
	}
	if runnerImage == "" {
		return nil, errors.New("collection-generation runner image is not configured")
	}
	request, err := NormalizeRequest(request)
	if err != nil {
		return nil, err
	}
	if retention == 0 {
		retention = config.DefaultGenerationRetention
	}
	if retention < 0 {
		return nil, errors.New("collection-generation retention must not be negative")
	}
	jobConfig := JobConfig{Request: request, Limits: budget.Limits, ReasoningEffort: reasoningEffort, RunnerImage: runnerImage, Retention: retention}
	encoded, err := json.Marshal(jobConfig)
	if err != nil {
		return nil, fmt.Errorf("encode generation job snapshot: %w", err)
	}
	if len(encoded) > maxConfigBytes {
		return nil, errors.New("collection-generation job snapshot exceeds size limit")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	benchmark := api.EvaluationBenchmarkConfig{
		Ref:        api.Ref{ID: BenchmarkID},
		ProviderID: ProviderID,
		Parameters: map[string]any{ConfigParameter: string(encoded)},
	}
	message := api.WithMessageOrigin(&api.MessageInfo{Message: "Collection generation job created", MessageCode: "evaluation_job_created"}, api.MessageOriginServer)
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: id, Tenant: tenant, Owner: owner, CreatedAt: now, UpdatedAt: now}},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending, Message: message},
			Benchmarks:         []api.BenchmarkStatus{{ID: BenchmarkID, ProviderID: ProviderID, BenchmarkIndex: 0, Status: api.StatePending}},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Name:       "collection-generation",
			Model:      &request.Model,
			Benchmarks: []api.EvaluationBenchmarkConfig{benchmark},
		},
	}, nil
}

// IsJob recognizes only the internal generation workload's reserved benchmark pair.
func IsJob(job *api.EvaluationJobConfig) bool {
	return job != nil && job.Collection == nil && len(job.Benchmarks) == 1 &&
		job.Benchmarks[0].ProviderID == ProviderID && job.Benchmarks[0].ID == BenchmarkID
}

// ConfigFromJob decodes the immutable runner snapshot from a stored generation job.
func ConfigFromJob(job *api.EvaluationJobConfig) (JobConfig, error) {
	if !IsJob(job) {
		return JobConfig{}, errors.New("job is not a collection-generation workload")
	}
	return ConfigFromParameters(job.Benchmarks[0].Parameters)
}

// ConfigFromParameters reads the snapshot from a persisted job or mounted job spec.
func ConfigFromParameters(parameters map[string]any) (JobConfig, error) {
	value, exists := parameters[ConfigParameter]
	if !exists {
		// Read snapshots written by earlier builds of the generation workload.
		value = parameters[legacyConfigParameter]
	}
	raw, ok := value.(string)
	if !ok {
		return JobConfig{}, errors.New("collection-generation snapshot is missing or invalid")
	}
	return DecodeJobConfig(raw)
}

// ResourceFromJob validates a stored generation workload and maps it to the
// stable public representation without exposing evaluation-job implementation fields.
func ResourceFromJob(job *api.EvaluationJobResource) (*api.CollectionGenerationResource, error) {
	if job == nil || !IsJob(&job.EvaluationJobConfig) || job.Status == nil {
		return nil, errors.New("stored collection-generation job is invalid")
	}
	snapshot, err := ConfigFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, errors.New("stored collection-generation request is invalid")
	}
	state := job.Status.State
	switch state {
	case api.OverallStatePending, api.OverallStateRunning, api.OverallStateCompleted, api.OverallStateFailed, api.OverallStateCancelled:
	default:
		return nil, errors.New("stored collection-generation state is invalid")
	}
	resource := &api.CollectionGenerationResource{
		Resource: job.Resource.Resource,
		Request:  snapshot.Request,
		Status:   api.CollectionGenerationStatus{State: state},
	}
	for _, benchmark := range job.Status.Benchmarks {
		if benchmark.ProviderID != ProviderID || benchmark.ID != BenchmarkID || benchmark.BenchmarkIndex != 0 {
			continue
		}
		resource.Status.StartedAt = benchmark.StartedAt
		resource.Status.CompletedAt = benchmark.CompletedAt
		break
	}
	if state == api.OverallStateRunning && job.Status.CollectionGenerationProgress != nil {
		progress := job.Status.CollectionGenerationProgress.CollectionGenerationStatusProgress
		resource.Status.Progress = &progress
	}
	additionalInfo := generationAdditionalInfo(job)
	switch state {
	case api.OverallStateCompleted:
		result, err := ResultFromAdditionalInfo(snapshot.Request, additionalInfo)
		if err != nil {
			return nil, errors.New("stored collection-generation result is invalid")
		}
		resource.Result = &result
	case api.OverallStateFailed:
		failure, err := FailureFromAdditionalInfo(additionalInfo)
		if err != nil {
			return nil, errors.New("stored collection-generation failure is invalid")
		}
		resource.Status.Error = &failure
	}
	return resource, nil
}

func generationAdditionalInfo(job *api.EvaluationJobResource) map[string]any {
	if job.Results == nil {
		return nil
	}
	for _, result := range job.Results.Benchmarks {
		if result.ProviderID == ProviderID && result.ID == BenchmarkID && result.BenchmarkIndex == 0 {
			return result.AdditionalInfo
		}
	}
	return nil
}

// DecodeJobConfig validates the serialized immutable workload snapshot before the worker uses it.
func DecodeJobConfig(raw string) (JobConfig, error) {
	if raw == "" || len(raw) > maxConfigBytes {
		return JobConfig{}, errors.New("collection-generation snapshot is missing or exceeds the size limit")
	}
	var snapshot JobConfig
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return JobConfig{}, errors.New("collection-generation snapshot is malformed")
	}
	if snapshot.RunnerImage == "" || snapshot.Request.Model.Name == "" || snapshot.Request.Model.URL == "" || snapshot.Retention <= 0 {
		return JobConfig{}, errors.New("collection-generation snapshot is incomplete")
	}
	if err := (loop.Budget{Limits: snapshot.Limits}).ValidateAgainst(loop.DefaultHardLimits()); err != nil {
		return JobConfig{}, fmt.Errorf("collection-generation snapshot limits are invalid: %w", err)
	}
	return snapshot, nil
}

// RetentionForJob returns the admission-time retention snapshot, falling back to the
// configured default only for malformed data so terminal rows remain bounded.
func RetentionForJob(job *api.EvaluationJobConfig) time.Duration {
	snapshot, err := ConfigFromJob(job)
	if err != nil {
		return config.DefaultGenerationRetention
	}
	return snapshot.Retention
}

// RuntimeProviderForJob returns the exact EvalHub image and worker command admitted with the job.
func RuntimeProviderForJob(job *api.EvaluationJobConfig) *api.ProviderResource {
	config, err := ConfigFromJob(job)
	if err != nil {
		return nil
	}
	return &api.ProviderResource{
		Resource: api.Resource{ID: ProviderID},
		ProviderConfig: api.ProviderConfig{
			Name: "EvalHub Collection Generation", Title: "EvalHub Collection Generation",
			Runtime: &api.Runtime{K8s: &api.K8sRuntime{
				Image: config.RunnerImage, Entrypoint: []string{RunnerEntrypoint},
				CPURequest: "250m", MemoryRequest: "512Mi", CPULimit: "1", MemoryLimit: "1Gi",
				ImagePullPolicy: "if_not_present",
			}},
			Benchmarks: []api.BenchmarkResource{{ID: BenchmarkID}},
		},
	}
}
