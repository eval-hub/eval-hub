package mlflowclient

import (
	"encoding/json"
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

// A nonstandard body carries no response body of its own, only the decoded
// message, which must still appear in the rendered error.
func TestAPIError_ErrorFallsBackToDecodedMessage(t *testing.T) {
	t.Parallel()
	err := &APIError{
		StatusCode:  403,
		MLFlowError: &MLFlowError{Message: "Forbidden: response body \"denied\""},
	}
	msg := err.Error()
	if msg == "" || !strings.Contains(msg, "403") || !strings.Contains(msg, "denied") {
		t.Fatalf("Error() = %q", msg)
	}
	if got := (&APIError{StatusCode: 403}).Error(); got == "" {
		t.Fatal("Error() = \"\", want a diagnostic even with no detail")
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

	// RHAI-17156: a workspace provider can answer 404 without MLflow's error code.
	t.Run("not found without error code", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   404,
			ResponseBody: "Not Found: response body \"workspace new-project not found\"",
			MLFlowError:  &MLFlowError{Message: "Not Found: response body \"workspace new-project not found\""},
		}
		if !IsResourceDoesNotExistError(err) {
			t.Fatal("expected true when the body has no MLflow error code")
		}
	})

	t.Run("explicit other error code is respected", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:  404,
			MLFlowError: &MLFlowError{ErrorCode: "ENDPOINT_NOT_FOUND", Message: "no such endpoint"},
		}
		if IsResourceDoesNotExistError(err) {
			t.Fatal("expected false when a different error code is reported")
		}
	})

	t.Run("explicit code wins over the body excerpt", func(t *testing.T) {
		t.Parallel()
		// mapError renders the code and message into ResponseBody, so a provider
		// message can mention RESOURCE_DOES_NOT_EXIST while the decoded code is
		// something else. The explicit code must decide.
		got := mapError(&mlflow.APIError{
			StatusCode: 404,
			Code:       "ENDPOINT_NOT_FOUND",
			Message:    `upstream said {"error_code":"RESOURCE_DOES_NOT_EXIST"}`,
		})
		var mapped *APIError
		if !errors.As(got, &mapped) {
			t.Fatalf("mapError() = %T, want *APIError", got)
		}
		if !strings.Contains(mapped.ResponseBody, "RESOURCE_DOES_NOT_EXIST") {
			t.Fatalf("ResponseBody = %q, want the body excerpt to contain the substring", mapped.ResponseBody)
		}
		if IsResourceDoesNotExistError(got) {
			t.Fatalf("expected false for ENDPOINT_NOT_FOUND: %v", got)
		}
	})

	// APIError is exported and its legacy shape carries only a raw body, so an
	// explicit code in the body must decide just like a decoded one.
	t.Run("body-only error with another explicit code is respected", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   404,
			ResponseBody: `{"error_code":"ENDPOINT_NOT_FOUND","message":"no such endpoint"}`,
		}
		if IsResourceDoesNotExistError(err) {
			t.Fatal("expected false when the body carries a different error code")
		}
	})

	t.Run("body-only error without a code falls back to the status", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{
			`{"detail":"workspace new-project not found"}`,
			`{"error_code":"","message":"not found"}`,
			`{"error_code":`,
			"Not Found: workspace new-project not found",
		} {
			if !IsResourceDoesNotExistError(&APIError{StatusCode: 404, ResponseBody: body}) {
				t.Fatalf("expected true for body %q without an error code", body)
			}
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

	// mlflow-go treats HTTP 409 as already-exists, so the adapter must too:
	// a provider may report the conflict without MLflow's error code.
	t.Run("conflict status", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:  409,
			MLFlowError: &MLFlowError{Message: "Conflict: response body \"workspace exists\""},
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true for HTTP 409")
		}
	})

	t.Run("error code on a non-400 status", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:  409,
			MLFlowError: &MLFlowError{ErrorCode: "RESOURCE_ALREADY_EXISTS", Message: "exists"},
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true when the MLflow error code is reported")
		}
	})

	t.Run("explicit code wins over the body excerpt", func(t *testing.T) {
		t.Parallel()
		// mapError renders the code and message into ResponseBody, so a provider
		// message can quote RESOURCE_ALREADY_EXISTS while the decoded code is
		// something else. The explicit code must decide.
		got := mapError(&mlflow.APIError{
			StatusCode: 403,
			Code:       "PERMISSION_DENIED",
			Message:    `upstream said {"error_code":"RESOURCE_ALREADY_EXISTS"}`,
		})
		var mapped *APIError
		if !errors.As(got, &mapped) {
			t.Fatalf("mapError() = %T, want *APIError", got)
		}
		if !strings.Contains(mapped.ResponseBody, "RESOURCE_ALREADY_EXISTS") {
			t.Fatalf("ResponseBody = %q, want the body excerpt to contain the substring", mapped.ResponseBody)
		}
		if IsResourceAlreadyExistsError(got) {
			t.Fatalf("expected false for PERMISSION_DENIED: %v", got)
		}
	})

	// APIError is exported and its legacy shape carries only a raw body, so an
	// explicit code in the body must decide just like a decoded one.
	t.Run("body-only error with another explicit code is respected", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   400,
			ResponseBody: `{"error_code":"PERMISSION_DENIED","message":"upstream said RESOURCE_ALREADY_EXISTS"}`,
		}
		if IsResourceAlreadyExistsError(err) {
			t.Fatal("expected false when the body carries a different error code")
		}
	})

	// HTTP 409 stays decisive on its own, matching mlflow-go's IsAlreadyExists.
	t.Run("conflict status wins over an explicit other code", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:  409,
			MLFlowError: &MLFlowError{ErrorCode: "PERMISSION_DENIED", Message: "denied"},
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true for HTTP 409 regardless of the error code")
		}
	})

	t.Run("body-only error without a code falls back to the substring", func(t *testing.T) {
		t.Parallel()
		err := &APIError{
			StatusCode:   400,
			ResponseBody: "Bad Request: RESOURCE_ALREADY_EXISTS experiment exists",
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected true for a raw body naming the error code")
		}
	})

	t.Run("non-API error", func(t *testing.T) {
		t.Parallel()
		if IsResourceAlreadyExistsError(errors.New("network unreachable")) {
			t.Fatal("expected false for a non-API error")
		}
	})

	t.Run("unrelated status", func(t *testing.T) {
		t.Parallel()
		if IsResourceAlreadyExistsError(&APIError{StatusCode: 403, MLFlowError: &MLFlowError{Message: "Forbidden"}}) {
			t.Fatal("expected false for HTTP 403")
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

	// RHAI-17156: a workspace provider answered 403 with {"detail": "..."}, which
	// is outside MLflow's error_code/message schema. mlflow-go now keeps the status
	// and synthesizes a message carrying a body excerpt; the adapter must pass it
	// through instead of wrapping it in an empty error_code envelope.
	t.Run("nonstandard body keeps the server detail", func(t *testing.T) {
		t.Parallel()
		const detail = "Namespace creation is not supported by this provider"
		message := `Forbidden: response body "{\"detail\":\"` + detail + `\"}"`
		got := mapError(&mlflow.APIError{StatusCode: 403, Message: message})
		var mapped *APIError
		if !errors.As(got, &mapped) {
			t.Fatalf("mapError() = %T, want *APIError", got)
		}
		if mapped.StatusCode != 403 {
			t.Fatalf("StatusCode = %d, want 403", mapped.StatusCode)
		}
		if mapped.MLFlowError == nil {
			t.Fatal("MLFlowError = nil, want the synthesized message preserved")
		}
		if mapped.MLFlowError.ErrorCode != "" {
			t.Fatalf("ErrorCode = %q, want empty for a nonstandard body", mapped.MLFlowError.ErrorCode)
		}
		if mapped.MLFlowError.Message != message {
			t.Fatalf("Message = %q, want %q", mapped.MLFlowError.Message, message)
		}
		if mapped.ResponseBody != message {
			t.Fatalf("ResponseBody = %q, want the plain message %q", mapped.ResponseBody, message)
		}
		if strings.Contains(mapped.ResponseBody, "error_code") {
			t.Fatalf("ResponseBody = %q, want no empty error_code envelope", mapped.ResponseBody)
		}
		if !strings.Contains(mapped.Error(), detail) {
			t.Fatalf("Error() = %q, want it to contain %q", mapped.Error(), detail)
		}
		if IsResourceAlreadyExistsError(got) || IsResourceDoesNotExistError(got) {
			t.Fatalf("403 must not be classified as already-exists or not-found: %v", got)
		}
	})

	t.Run("empty message falls back to the status text", func(t *testing.T) {
		t.Parallel()
		got := mapError(&mlflow.APIError{StatusCode: 403})
		var mapped *APIError
		if !errors.As(got, &mapped) {
			t.Fatalf("mapError() = %T, want *APIError", got)
		}
		if mapped.MLFlowError == nil || mapped.MLFlowError.Message != "Forbidden" {
			t.Fatalf("MLFlowError = %+v, want message %q", mapped.MLFlowError, "Forbidden")
		}
		if mapped.ResponseBody != "Forbidden" {
			t.Fatalf("ResponseBody = %q, want %q", mapped.ResponseBody, "Forbidden")
		}
	})

	t.Run("unknown status without message still yields a diagnostic", func(t *testing.T) {
		t.Parallel()
		got := mapError(&mlflow.APIError{StatusCode: 599})
		if got == nil || got.Error() == "" {
			t.Fatalf("mapError() = %v, want a non-empty diagnostic", got)
		}
		if !strings.Contains(got.Error(), "599") {
			t.Fatalf("Error() = %q, want it to contain the status", got.Error())
		}
	})

	t.Run("structured response body is valid JSON", func(t *testing.T) {
		t.Parallel()
		got := mapError(&mlflow.APIError{
			StatusCode: 400,
			Code:       "INVALID_PARAMETER_VALUE",
			Message:    "bad \"quoted\" value",
		})
		var mapped *APIError
		if !errors.As(got, &mapped) {
			t.Fatalf("mapError() = %T, want *APIError", got)
		}
		var decoded MLFlowError
		if err := json.Unmarshal([]byte(mapped.ResponseBody), &decoded); err != nil {
			t.Fatalf("ResponseBody %q is not valid JSON: %v", mapped.ResponseBody, err)
		}
		if decoded.ErrorCode != "INVALID_PARAMETER_VALUE" || decoded.Message != `bad "quoted" value` {
			t.Fatalf("decoded ResponseBody = %+v", decoded)
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
