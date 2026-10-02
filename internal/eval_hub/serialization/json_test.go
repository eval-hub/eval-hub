package serialization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

func TestUnmarshal_OneOfValidationErrorListsAllowedValues(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	logger := logging.FallbackLogger()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "user", "tenant")

	body := []byte(`{
		"name":"test-provider",
		"runtime":{"k8s":{"image":"quay.io/example/adapter:latest","entrypoint":["/bin/true"],"image_pull_policy":"random"}},
		"benchmarks":[{"id":"bench-1","name":"Bench 1"}]
	}`)
	cfg := &api.ProviderConfig{}

	err := Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var svcErr *serviceerrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	got := svcErr.Error()
	if !strings.Contains(got, "image_pull_policy must be one of: if_not_present, always") {
		t.Fatalf("error = %q", got)
	}
}

func TestUnmarshal_TestDataRefMutualExclusionValidationError(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	logger := logging.FallbackLogger()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "user", "tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"name":"m","url":"http://example.com"},
		"benchmarks":[{
			"id":"bench-1",
			"provider_id":"provider-1",
			"test_data_ref":{
				"s3":{"bucket":"b","key":"k","secret_ref":"s"},
				"pvc":{"claim_name":"my-pvc"}
			}
		}]
	}`)
	cfg := &api.EvaluationJobConfig{}

	err := Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var svcErr *serviceerrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	got := svcErr.Error()
	if !strings.Contains(got, "test_data_ref: exactly one of s3, pvc, git, or hf must be set") {
		t.Fatalf("error = %q", got)
	}
}

func TestUnmarshal_TestDataRefHFMutualExclusionValidationError(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	logger := logging.FallbackLogger()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "user", "tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"name":"m","url":"http://example.com"},
		"benchmarks":[{
			"id":"bench-1",
			"provider_id":"provider-1",
			"test_data_ref":{
				"hf":{"repo_id":"cais/mmlu"},
				"s3":{"bucket":"b","key":"k","secret_ref":"s"}
			}
		}]
	}`)
	cfg := &api.EvaluationJobConfig{}

	err := Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var svcErr *serviceerrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	got := svcErr.Error()
	if !strings.Contains(got, "test_data_ref: exactly one of s3, pvc, git, or hf must be set") {
		t.Fatalf("error = %q", got)
	}
}

func TestUnmarshal_TestDataRefRequiredValidationError(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	logger := logging.FallbackLogger()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "user", "tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"name":"m","url":"http://example.com"},
		"benchmarks":[{
			"id":"bench-1",
			"provider_id":"provider-1",
			"test_data_ref":{}
		}]
	}`)
	cfg := &api.EvaluationJobConfig{}

	err := Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var svcErr *serviceerrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	got := svcErr.Error()
	if !strings.Contains(got, "test_data_ref: one of s3, pvc, git, or hf must be set") {
		t.Fatalf("error = %q", got)
	}
}

func TestUnmarshal_HardwareConfigExclusiveValidationError(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	logger := logging.FallbackLogger()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "user", "tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"name":"m","url":"http://example.com"},
		"benchmarks":[{
			"id":"bench-1",
			"provider_id":"provider-1",
			"hardware_config":{
				"hardware_profile_name":"my-hw-spec",
				"cpu":{"request":"1","limit":"2"}
			}
		}]
	}`)
	cfg := &api.EvaluationJobConfig{}

	err := Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var svcErr *serviceerrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	got := svcErr.Error()
	if !strings.Contains(got, "hardware_config: hardware_profile_name cannot be combined with queue, cpu, memory, or gpu") {
		t.Fatalf("error = %q", got)
	}
}

func TestUnmarshal_PostProcessingValidationErrors(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-post-processing", logging.FallbackLogger(), "user", "tenant")
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "operation order must match operations",
			body: `{"operation_order":["unknown"],"operations":{"confidence_interval":{"results_data_ref":{"pvc":{"claim_name":"results-data"}},"primary_score":{"metric":"accuracy"},"calibration_data_ref":[{"pvc":{"claim_name":"calibration-data"},"data_config":{"format":"jsonl","columns":{"label":"label","prediction":"prediction"}}}],"significance_level":0.05}}}`,
			want: "operation_order must list each configured operation exactly once",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request api.StandalonePostProcessingRequest
			err := Unmarshal(validate, ctx, []byte(test.body), &request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Unmarshal() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestFormatValidationError_PostProcessingRules(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	tests := []struct {
		name     string
		value    any
		wantText string
	}{
		{
			name:     "at least one operation",
			value:    api.StandalonePostProcessingOperations{},
			wantText: "operations must contain at least one operation",
		},
		{
			name: "order lists configured operations",
			value: api.StandalonePostProcessingRequest{
				PostProcessingCommon: api.PostProcessingCommon{OperationOrder: []string{"other"}},
				Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
					ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
						CalibrationDataRef: []api.CalibrationDataRef{{
							PVC:        &api.PVCTestDataRef{ClaimName: "calibration-data"},
							DataConfig: api.CalibrationDataConfig{Format: "jsonl", Columns: api.CalibrationDataColumns{Label: "label", Prediction: "prediction"}},
						}},
						SignificanceLevel: 0.05,
					},
					ResultsDataRef: &api.PostProcessingResultsDataRef{PVC: &api.PVCTestDataRef{ClaimName: "results-data"}},
					PrimaryScore:   &api.PrimaryScore{Metric: "accuracy"},
				}},
			},
			wantText: "operation_order must list each configured operation exactly once",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate.Struct(test.value)
			validationErrors, ok := err.(validator.ValidationErrors)
			if !ok {
				t.Fatalf("validation error = %v, want validator.ValidationErrors", err)
			}
			if got := formatValidationError(validationErrors); got != test.wantText {
				t.Fatalf("formatValidationError() = %q, want %q", got, test.wantText)
			}
		})
	}
}

func TestUnmarshal_InvalidJSONReturnsServiceError(t *testing.T) {
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-invalid-json", logging.FallbackLogger(), "user", "tenant")
	var request api.StandalonePostProcessingRequest
	err := Unmarshal(validate, ctx, []byte("{"), &request)
	if err == nil {
		t.Fatal("Unmarshal() error = nil, want invalid JSON service error")
	}
	var serviceError *serviceerrors.ServiceError
	if !errors.As(err, &serviceError) || serviceError.MessageCode() != messages.InvalidJSONRequest {
		t.Fatalf("Unmarshal() error = %T %v, want InvalidJSONRequest", err, err)
	}
}
