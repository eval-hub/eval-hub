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
	for _, path := range []string{"inline", "collection", "post-processing"} {
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
				var scopedStorage abstractions.Storage = storage
				ppStorage := newPostProcessingHandlerStorage()
				body := map[string]any{
					"name": "evaluation", "model": map[string]any{"name": "model", "url": "http://model.test"},
				}
				switch path {
				case "inline":
					body["benchmarks"] = []any{map[string]any{"id": "benchmark-1", "provider_id": "provider-1"}}
				case "collection":
					body["collection"] = map[string]any{"id": "collection-1"}
				case "post-processing":
					scopedStorage = ppStorage
					if err := json.Unmarshal(marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil), &body); err != nil {
						t.Fatal(err)
					}
				}
				coordinates := map[string]any{"oci_host": "quay.io", "oci_repository": "team/results"}
				if !tt.omit {
					coordinates["oci_tag"] = tt.tag
				}
				body["exports"] = map[string]any{"oci": map[string]any{"coordinates": coordinates}}
				if path == "post-processing" {
					// This path inherits exports from the source evaluation.
					delete(body, "exports")
					ppStorage.source = &api.EvaluationJobResource{
						Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
						Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
						EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
							Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results", OCITag: tt.tag},
						}}},
					}
				}
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
				h := handlers.New(scopedStorage, testhelpers.NewValidator(t), activeRuntime, nil, nil, nil, nil, nil)
				recorder := httptest.NewRecorder()
				req := &bodyRequest{MockRequest: createMockRequest(http.MethodPost, "/api/v1/evaluations/jobs"), body: encoded}
				ctx := executioncontext.NewExecutionContext(context.Background(), "req-oci-tag", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")
				if path == "post-processing" {
					h.HandleCreatePostProcessing(ctx, req, MockResponseWrapper{recorder: recorder})
				} else {
					h.HandleCreateEvaluation(ctx, req, MockResponseWrapper{recorder: recorder})
				}
				created := storage.createdJob
				if path == "post-processing" {
					created = ppStorage.createdJob
				}
				if tt.invalid {
					var response api.Error
					if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if recorder.Code != http.StatusBadRequest || response.MessageCode != messages.RequestValidationFailed.GetCode() || !strings.Contains(response.Message, "exports.oci.coordinates.oci_tag") {
						t.Fatalf("expected tag request validation error, got %d: %s", recorder.Code, recorder.Body.String())
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
				want := created.Resource.ID
				if tt.tag != "" {
					prefix := tt.tag
					if len(prefix) > 91 {
						prefix = prefix[:91]
					}
					want = prefix + "-" + want
				}
				if tag != want {
					t.Fatalf("final tag = %q, want %q", tag, want)
				}
			})
		}
	}
}
