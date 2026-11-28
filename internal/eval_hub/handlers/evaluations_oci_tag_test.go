package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

func TestCreateEvaluationOCITag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		omit    bool
		invalid bool
	}{
		{name: "omitted", omit: true},
		{name: "empty"},
		{name: "custom", tag: "final"},
		{name: "reserved card prefix", tag: "evaluation-card", invalid: true},
		{name: "custom card prefix", tag: "evaluation-card-custom"},
		{name: "allowed characters", tag: "_AZaz09.-_"},
		{name: "91 characters", tag: strings.Repeat("a", 91)},
		{name: "92 characters", tag: strings.Repeat("a", 92)},
		{name: "129 characters", tag: strings.Repeat("a", 129)},
		{name: "128 characters", tag: strings.Repeat("a", 128)},
		{name: "invalid past truncation", tag: strings.Repeat("a", 150) + "/", invalid: true},
		{name: "whitespace only", tag: " ", invalid: true},
		{name: "leading whitespace", tag: " final", invalid: true},
		{name: "trailing whitespace", tag: "final ", invalid: true},
		{name: "internal whitespace", tag: "fi nal", invalid: true},
		{name: "newline", tag: "final\n", invalid: true},
		{name: "slash", tag: "a/b", invalid: true},
		{name: "colon", tag: "a:b", invalid: true},
		{name: "non ASCII", tag: "éval", invalid: true},
		{name: "leading dot", tag: ".final", invalid: true},
		{name: "leading hyphen", tag: "-final", invalid: true},
	}
	for _, path := range []string{"inline", "collection"} {
		for _, tt := range tests {
			t.Run(path+"/"+tt.name, func(t *testing.T) {
				storage := &collectionRunCountStorage{
					fakeStorage: &fakeStorage{providerConfigs: map[string]api.ProviderResource{
						"provider-1": {Resource: api.Resource{ID: "provider-1"}, ProviderConfig: api.ProviderConfig{
							Benchmarks: []api.BenchmarkResource{{ID: "benchmark-1"}},
						}},
					}},
					collection: &api.CollectionResource{Resource: api.Resource{ID: "collection-1"}, CollectionConfig: api.CollectionConfig{
						Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "benchmark-1"}, ProviderID: "provider-1"}},
					}},
				}
				body := map[string]any{
					"name": "evaluation", "model": map[string]any{"name": "model", "url": "http://model.test"},
				}
				switch path {
				case "inline":
					body["benchmarks"] = []any{map[string]any{"id": "benchmark-1", "provider_id": "provider-1"}}
				case "collection":
					body["collection"] = map[string]any{"id": "collection-1"}
				}
				coordinates := map[string]any{"oci_host": "quay.io", "oci_repository": "team/results"}
				if !tt.omit {
					coordinates["oci_tag"] = tt.tag
				}
				body["exports"] = map[string]any{"oci": map[string]any{"coordinates": coordinates}}
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				// Invalid requests must never reach runtime validation or scheduling.
				runtime := &fakeRuntime{}
				var activeRuntime abstractions.Runtime
				if tt.invalid {
					activeRuntime = runtime
				}
				h := handlers.New(storage, testhelpers.NewValidator(t), activeRuntime, nil, nil, nil, nil, nil)
				recorder := httptest.NewRecorder()
				req := &bodyRequest{MockRequest: createMockRequest(http.MethodPost, "/api/v1/evaluations/jobs"), body: encoded}
				ctx := executioncontext.NewExecutionContext(context.Background(), "req-oci-tag", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")
				h.HandleCreateEvaluation(ctx, req, MockResponseWrapper{recorder: recorder})
				created := storage.createdJob
				if tt.invalid {
					var response api.Error
					if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if recorder.Code != http.StatusBadRequest || response.MessageCode != messages.RequestValidationFailed.GetCode() || !strings.Contains(response.Message, "exports.oci.coordinates.oci_tag") {
						t.Fatalf("expected tag request validation error, got %d: %s", recorder.Code, recorder.Body.String())
					}
					if tt.tag == "evaluation-card" && !strings.Contains(response.Message, `"evaluation-card" is reserved for evaluation card artifacts`) {
						t.Fatalf("expected reserved prefix explanation, got %s", response.Message)
					}
					if created != nil || storage.atomicCalls != 0 || runtime.called || runtime.validateHWCalled {
						t.Fatal("invalid tag reached persistence or runtime")
					}
					return
				}
				if recorder.Code != http.StatusAccepted || created == nil {
					t.Fatalf("expected persisted job and 202, got %d: %s", recorder.Code, recorder.Body.String())
				}
				if created.Exports.OCI.Coordinates.OCITag != tt.tag {
					t.Fatalf("stored tag was rewritten: %q", created.Exports.OCI.Coordinates.OCITag)
				}
				if len(created.Resource.ID) != 36 {
					t.Fatalf("expected UUID job ID, got %q", created.Resource.ID)
				}
				tag, err := ociclient.EvaluationBundleManifestTag(created.Resource.ID, tt.tag)
				if err != nil {
					t.Fatal(err)
				}
				want := "evaluation-bundle-" + created.Resource.ID
				if tt.tag != "" {
					prefix := tt.tag
					if len(prefix) > 91 {
						prefix = prefix[:91]
					}
					want = prefix + "-" + created.Resource.ID
				}
				if tag != want {
					t.Fatalf("final tag = %q, want %q", tag, want)
				}
			})
		}
	}
}

func TestCreatePostProcessingPreservesOCITag(t *testing.T) {
	for _, source := range []string{"literal", "inherited"} {
		for _, tag := range []string{"", "final", "evaluation-card", "existing/tag", " literal ", strings.Repeat("a", 129)} {
			t.Run(source+"/"+tag, func(t *testing.T) {
				storage := newPostProcessingHandlerStorage()
				coordinates := api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results", OCITag: tag}
				var payload api.StandalonePostProcessingRequest
				var evalJob *api.EvaluationJobDataRef
				if source == "inherited" {
					evalJob = &api.EvaluationJobDataRef{ID: "source-job"}
					storage.source = &api.EvaluationJobResource{
						Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
						Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
						EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
							Coordinates: coordinates,
						}}},
					}
				}
				if err := json.Unmarshal(marshalPostProcessingRequest(t, evalJob, nil), &payload); err != nil {
					t.Fatal(err)
				}
				if source == "literal" {
					payload.Operations.ConfidenceInterval.ResultsDataRef = &api.PostProcessingResultsDataRef{
						OCI: &api.OCIDataRef{Coordinates: coordinates, ArtifactPath: "results.json"},
					}
				}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, nil, nil, nil)
				recorder := httptest.NewRecorder()
				req := &bodyRequest{MockRequest: createMockRequest(http.MethodPost, "/api/v1/evaluations/post-processing"), body: body}
				ctx := executioncontext.NewExecutionContext(context.Background(), "req-oci-input-tag", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")
				h.HandleCreatePostProcessing(ctx, req, MockResponseWrapper{recorder: recorder})
				if recorder.Code != http.StatusAccepted || storage.createdJob == nil {
					t.Fatalf("expected persisted job and 202, got %d: %s", recorder.Code, recorder.Body.String())
				}
				// Post-processing reuses exports for input access, not bundle publication.
				// Preserve input and legacy stored tags without applying export-prefix rules.
				oci := storage.createdJob.Exports
				if oci == nil || oci.OCI == nil || oci.OCI.Coordinates.OCITag != tag {
					t.Fatalf("OCI input coordinates were rewritten: %+v", oci)
				}
			})
		}
	}
}
