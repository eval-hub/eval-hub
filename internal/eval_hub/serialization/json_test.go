package serialization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestUnmarshal_OneOfValidationErrorListsAllowedValues(t *testing.T) {
	validate := validation.NewValidator()
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
