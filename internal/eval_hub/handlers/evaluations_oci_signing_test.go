package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCreateEvaluationOCISigning(t *testing.T) {
	for _, path := range []string{"inline", "collection", "post-processing"} {
		for _, tt := range []struct {
			name, signing  string
			local, invalid bool
		}{
			{"unsigned local", `null`, true, false},
			{"unsigned cluster", `null`, false, false},
			{"local file without accessible keys", `{"type":"key-pair","secret_ref":"file:///nonexistent-evalhub-signing/keys"}`, true, false},
			{"local once-encoded path", `{"type":"key-pair","secret_ref":"file:///keys%252Fwith%20spaces"}`, true, false},
			{"cluster file rejected", `{"type":"key-pair","secret_ref":"file:///nonexistent-evalhub-signing/keys"}`, false, true},
			{"cluster Secret name", `{"type":"key-pair","secret_ref":"signing-key"}`, false, false},
			{"local Secret syntax retained", `{"type":"key-pair","secret_ref":"signing-key"}`, true, false},
			{"missing reference", `{"type":"key-pair"}`, true, true},
			{"null reference", `{"type":"key-pair","secret_ref":null}`, true, true},
			{"empty reference", `{"type":"key-pair","secret_ref":""}`, true, true},
			{"invalid inherited reference", `{"type":"key-pair","secret_ref":"Bad_Name"}`, true, true},
			{"unsupported method", `{"type":"keyless","secret_ref":"signing-key"}`, true, true},
			{"authority", `{"type":"key-pair","secret_ref":"file://localhost/keys"}`, true, true},
			{"query", `{"type":"key-pair","secret_ref":"file:///keys?x"}`, true, true},
			{"fragment", `{"type":"key-pair","secret_ref":"file:///keys#x"}`, true, true},
		} {
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
				var signing *api.OCISigningConfig
				if err := json.Unmarshal([]byte(tt.signing), &signing); err != nil {
					t.Fatal(err)
				}
				exports := &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{OCIHost: "localhost:5001", OCIRepository: "results"}, Signing: signing,
				}}
				body := map[string]any{"name": "evaluation", "model": map[string]any{"name": "model", "url": "http://model.test"}, "exports": exports}
				var scopedStorage abstractions.Storage = storage
				ppStorage := newPostProcessingHandlerStorage()
				switch path {
				case "inline":
					body["benchmarks"] = []any{map[string]any{"id": "benchmark-1", "provider_id": "provider-1"}}
				case "collection":
					body["collection"] = map[string]any{"id": "collection-1"}
				case "post-processing":
					scopedStorage = ppStorage
					body = nil
					if err := json.Unmarshal(marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil), &body); err != nil {
						t.Fatal(err)
					}
					ppStorage.source = &api.EvaluationJobResource{
						Resource:            api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
						Status:              &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
						EvaluationJobConfig: api.EvaluationJobConfig{Exports: exports},
					}
				}
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				runtime := &fakeRuntime{}
				var activeRuntime abstractions.Runtime
				if tt.invalid {
					activeRuntime = runtime
				}
				h := handlers.New(scopedStorage, testhelpers.NewValidator(t), activeRuntime, nil, nil,
					&config.Config{Service: &config.ServiceConfig{LocalMode: tt.local}}, nil, nil)
				recorder := httptest.NewRecorder()
				req := &bodyRequest{MockRequest: createMockRequest(http.MethodPost, "/api/v1/evaluations/jobs"), body: encoded}
				ctx := executioncontext.NewExecutionContext(context.Background(), "req-oci-signing", slog.New(slog.NewTextHandler(io.Discard, nil)), "user", "tenant")
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
					if recorder.Code != http.StatusBadRequest || response.MessageCode != messages.RequestValidationFailed.GetCode() {
						t.Fatalf("expected signing validation failure, got %d: %s", recorder.Code, recorder.Body.String())
					}
					if created != nil || storage.atomicCalls != 0 || runtime.called || runtime.validateHWCalled {
						t.Fatal("invalid signing reached persistence or runtime")
					}
					return
				}
				if recorder.Code != http.StatusAccepted || created == nil {
					t.Fatalf("expected persisted job and 202, got %d: %s", recorder.Code, recorder.Body.String())
				}
				if signing != nil && (created.Exports.OCI.Signing == nil || *created.Exports.OCI.Signing.SecretRef != *signing.SecretRef) {
					t.Fatal("signing reference changed during creation")
				}
			})
		}
	}
}
