package serialization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
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
	if !strings.Contains(got, "test_data_ref: s3 and pvc are mutually exclusive") {
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
	if !strings.Contains(got, "test_data_ref: one of s3 or pvc must be set") {
		t.Fatalf("error = %q", got)
	}
}
