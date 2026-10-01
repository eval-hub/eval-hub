package mlflowclient

import (
	"errors"
	"strings"
	"testing"
	"time"

	mlflow "github.com/opendatahub-io/mlflow-go/mlflow"
	"github.com/opendatahub-io/mlflow-go/mlflow/tracking"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	err := &APIError{
		StatusCode:   404,
		ResponseBody: `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`,
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "RESOURCE_DOES_NOT_EXIST") {
		t.Fatalf("Error() = %q", msg)
	}
}

func TestIsResourceDoesNotExistError(t *testing.T) {
	t.Parallel()

	t.Run("structured error", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode: 404,
			MLFlowError: &MLFlowError{
				ErrorCode: "RESOURCE_DOES_NOT_EXIST",
			},
		}
		if !IsResourceDoesNotExistError(err) {
			t.Fatal("expected true")
		}
	})

	t.Run("response body fallback", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   404,
			ResponseBody: `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`,
		}
		if !IsResourceDoesNotExistError(err) {
			t.Fatal("expected true")
		}
	})

	t.Run("wrong status", func(t *testing.T) {
		t.Parallel()
		err := &APIError{StatusCode: 500, MLFlowError: &MLFlowError{ErrorCode: "RESOURCE_DOES_NOT_EXIST"}}
		if IsResourceDoesNotExistError(err) {
			t.Fatal("expected false for non-404")
		}
	})

	t.Run("wrapped", func(t *testing.T) {
		t.Parallel()
		inner := &APIError{StatusCode: 404, MLFlowError: &MLFlowError{ErrorCode: "RESOURCE_DOES_NOT_EXIST"}}
		if !IsResourceDoesNotExistError(errors.Join(inner)) {
			t.Fatal("expected errors.As to match wrapped APIError")
		}
	})
}

func TestIsResourceAlreadyExistsError(t *testing.T) {
	t.Parallel()

	t.Run("structured error", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode: 400,
			MLFlowError: &MLFlowError{
				ErrorCode: "RESOURCE_ALREADY_EXISTS",
			},
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true")
		}
	})

	t.Run("response body fallback", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   400,
			ResponseBody: `{"error_code":"RESOURCE_ALREADY_EXISTS"}`,
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true")
		}
	})
}

func TestMapError(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()
		if err := mapError(nil); err != nil {
			t.Fatalf("mapError(nil) = %v, want nil", err)
		}
	})

	t.Run("non-API error is returned unchanged", func(t *testing.T) {
		t.Parallel()
		plain := errors.New("network unreachable")
		if got := mapError(plain); !errors.Is(got, plain) {
			t.Fatalf("mapError(plain) = %v, want the original error", got)
		}
	})

	t.Run("mlflow API error is mapped onto *APIError", func(t *testing.T) {
		t.Parallel()
		src := &mlflow.APIError{
			StatusCode: 400,
			Code:       "RESOURCE_ALREADY_EXISTS",
			Message:    "experiment exists",
		}
		got := mapError(src)
		var mapped *APIError
		if !errors.As(got, &mapped) {
			t.Fatalf("mapError() = %T, want *APIError", got)
		}
		if mapped.StatusCode != 400 {
			t.Fatalf("StatusCode = %d, want 400", mapped.StatusCode)
		}
		if mapped.MLFlowError == nil || mapped.MLFlowError.ErrorCode != "RESOURCE_ALREADY_EXISTS" {
			t.Fatalf("MLFlowError = %+v, want RESOURCE_ALREADY_EXISTS", mapped.MLFlowError)
		}
		if !strings.Contains(mapped.ResponseBody, "RESOURCE_ALREADY_EXISTS") {
			t.Fatalf("ResponseBody = %q, want it to embed the error code", mapped.ResponseBody)
		}
		// The mapped error must still satisfy the historical predicate.
		if !IsResourceAlreadyExistsError(got) {
			t.Fatal("expected IsResourceAlreadyExistsError to match the mapped error")
		}
	})
}

func TestCreateExperimentOptions(t *testing.T) {
	t.Parallel()

	if opts := createExperimentOptions(nil); opts != nil {
		t.Fatalf("createExperimentOptions(nil) = %v, want nil", opts)
	}

	if opts := createExperimentOptions(&CreateExperimentRequest{Name: "demo"}); len(opts) != 0 {
		t.Fatalf("options for bare request = %d, want 0", len(opts))
	}

	opts := createExperimentOptions(&CreateExperimentRequest{
		Name:             "demo",
		ArtifactLocation: "s3://bucket/path",
		Tags:             []api.ExperimentTag{{Key: "team", Value: "eval-hub"}},
	})
	if len(opts) != 2 {
		t.Fatalf("options for full request = %d, want 2 (artifact location + tags)", len(opts))
	}
}

func TestExperimentTagsRoundTrip(t *testing.T) {
	t.Parallel()

	if m := experimentTagsToMap(nil); m != nil {
		t.Fatalf("experimentTagsToMap(nil) = %v, want nil", m)
	}
	if tags := experimentTagsFromMap(nil); tags != nil {
		t.Fatalf("experimentTagsFromMap(nil) = %v, want nil", tags)
	}

	m := experimentTagsToMap([]api.ExperimentTag{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}})
	if len(m) != 2 || m["a"] != "1" || m["b"] != "2" {
		t.Fatalf("experimentTagsToMap = %v", m)
	}

	tags := experimentTagsFromMap(m)
	if len(tags) != 2 {
		t.Fatalf("experimentTagsFromMap = %v, want 2 tags", tags)
	}
	got := experimentTagsToMap(tags)
	if got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("round-tripped tags = %v", got)
	}
}

func TestExperimentFromSDK(t *testing.T) {
	t.Parallel()

	if got := experimentFromSDK(nil); got.ExperimentID != "" || got.Name != "" || got.Tags != nil {
		t.Fatalf("experimentFromSDK(nil) = %+v, want zero Experiment", got)
	}

	created := time.UnixMilli(1_000)
	updated := time.UnixMilli(2_000)
	got := experimentFromSDK(&tracking.Experiment{
		ID:               "exp-1",
		Name:             "demo",
		ArtifactLocation: "s3://bucket",
		LifecycleStage:   "active",
		Tags:             map[string]string{"team": "eval-hub"},
		CreationTime:     created,
		LastUpdateTime:   updated,
	})
	if got.ExperimentID != "exp-1" || got.Name != "demo" || got.ArtifactLocation != "s3://bucket" {
		t.Fatalf("experimentFromSDK = %+v", got)
	}
	if got.LifecycleStage != "active" {
		t.Fatalf("LifecycleStage = %q, want active", got.LifecycleStage)
	}
	if got.CreationTime != created.UnixMilli() || got.LastUpdateTime != updated.UnixMilli() {
		t.Fatalf("times = (%d, %d), want (%d, %d)", got.CreationTime, got.LastUpdateTime, created.UnixMilli(), updated.UnixMilli())
	}
	if len(got.Tags) != 1 || got.Tags[0].Key != "team" || got.Tags[0].Value != "eval-hub" {
		t.Fatalf("Tags = %+v", got.Tags)
	}
}
