package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

type ociReportingStorage struct {
	noopStorage
	job         *api.EvaluationJobResource
	calls       []api.OCIProcessingState
	cards       []*api.OCIArtifactReference
	bundles     []*api.OCIArtifactReference
	codes       []string
	failAt      int
	failFrom    int
	unavailable bool
}

func (s *ociReportingStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.job, nil
}
func (s *ociReportingStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card, bundle *api.OCIArtifactReference) error {
	if id != s.job.Resource.ID {
		return errors.New("wrong job")
	}
	s.calls = append(s.calls, status.State)
	s.cards = append(s.cards, card)
	s.bundles = append(s.bundles, bundle)
	if s.unavailable || len(s.calls) == s.failAt || (s.failFrom > 0 && len(s.calls) >= s.failFrom) {
		return errors.New("database unavailable")
	}
	if status.Message == nil || status.Message.Message == "" || status.Message.MessageCode == "" || status.Message.MessageOrigin != api.MessageOriginServer {
		return errors.New("incomplete message")
	}
	s.codes = append(s.codes, status.Message.MessageCode)
	s.job.Results = api.UpdateOCIResults(s.job.Results, status, card, bundle)
	return nil
}

type ociOutcomeExporter struct {
	result     evalcards.ExportResult
	err        error
	calls      int
	ociEnabled bool
}

func (e *ociOutcomeExporter) Export(_ context.Context, job *api.EvaluationJobResource, _ *cards.EvaluationCard) (evalcards.ExportResult, error) {
	e.calls++
	e.ociEnabled = job.Exports != nil && job.Exports.OCI != nil
	return e.result, e.err
}
func completedOCIJob() *api.EvaluationJobResource {
	job := testEvaluationJob()
	job.Resource.Tenant = "tenant-a"
	job.Status.Message = &api.MessageInfo{Message: "Evaluation finished", MessageCode: "JOB_COMPLETED"}
	job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results", OCITag: "custom"}}}
	job.Results = &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{{ID: "bench", Metrics: map[string]any{"accuracy": 0.7}}}}
	return job
}
func publishedCard() *api.OCIArtifactReference {
	return &api.OCIArtifactReference{OCIDigest: "sha256:" + strings.Repeat("a", 64), OCIReference: "quay.io/team/results:evaluation-card-job-1@sha256:" + strings.Repeat("a", 64)}
}

// publishedBundle returns an index reference distinct from the card to detect output mix-ups.
func publishedBundle() *api.OCIArtifactReference {
	return &api.OCIArtifactReference{OCIDigest: "sha256:" + strings.Repeat("b", 64), OCIReference: "quay.io/team/results:custom-job-1@sha256:" + strings.Repeat("b", 64)}
}

type stubBundleFactory struct {
	ref          *api.OCIArtifactReference
	factoryError error
	publishError error
	created      int
	published    int
	job          *api.EvaluationJobResource
	card         *api.OCIArtifactReference
}

// NewPublisher records the job scope and optionally simulates initialization failure.
func (f *stubBundleFactory) NewPublisher(_ context.Context, job *api.EvaluationJobResource) (oci.EvaluationBundlePublisher, error) {
	f.created++
	f.job = job
	return f, f.factoryError
}

// Publish checks that bundling uses the same job after running progress has been saved.
func (f *stubBundleFactory) Publish(_ context.Context, job *api.EvaluationJobResource, card *api.OCIArtifactReference) (*api.OCIArtifactReference, error) {
	f.published++
	f.card = card
	if job != f.job || job.Results.OCI.Status.State != api.OCIProcessingRunning {
		return nil, errors.New("bundle must run with the recorded job while OCI is running")
	}
	return f.ref, f.publishError
}

// TestOCIReportingSuccessAndUnchangedTerminalCallback verifies saved outputs and skips sequential duplicate exports.
func TestOCIReportingSuccessAndUnchangedTerminalCallback(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStateCompleted, api.OverallStatePartiallyFailed} {
		t.Run(string(state), func(t *testing.T) {
			job := completedOCIJob()
			job.Status.State = state
			before, _ := json.Marshal(job.Status)
			exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}, err: errors.New("MLflow upload failed")}
			storage := &ociReportingStorage{job: job}
			h := &Handlers{resultsExporter: exporter, bundlePublisherFactory: &stubBundleFactory{ref: publishedBundle()}}
			h.onEvaluationJobUpdated(context.Background(), storage, func() (*api.EvaluationJobResource, error) { return job, nil }, api.OverallStateRunning, nil)
			want := []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted}
			if !reflect.DeepEqual(storage.calls, want) {
				t.Fatalf("progress = %v", storage.calls)
			}
			wantCodes := []string{"oci_processing_running", "oci_processing_completed"}
			if !reflect.DeepEqual(storage.codes, wantCodes) {
				t.Fatalf("message codes = %v, want %v", storage.codes, wantCodes)
			}
			if !reflect.DeepEqual(storage.cards, []*api.OCIArtifactReference{nil, publishedCard()}) {
				t.Fatalf("expected card reference only in final completion update: %v", storage.cards)
			}
			if !reflect.DeepEqual(storage.bundles, []*api.OCIArtifactReference{nil, publishedBundle()}) {
				t.Fatalf("unexpected bundle references: %v", storage.bundles)
			}
			if !reflect.DeepEqual(job.Results.OCI.EvaluationCard, publishedCard()) || !reflect.DeepEqual(job.Results.OCI.EvaluationBundle, publishedBundle()) || job.Results.OCI.Signing != nil {
				t.Fatalf("OCI outputs = %#v", job.Results.OCI)
			}
			after, _ := json.Marshal(job.Status)
			if !bytes.Equal(before, after) || job.Results.Benchmarks[0].Metrics["accuracy"] != 0.7 {
				t.Fatal("evaluation status or benchmark results changed")
			}
			h.onEvaluationJobUpdated(context.Background(), storage, func() (*api.EvaluationJobResource, error) { return job, nil }, state, nil)
			if exporter.calls != 1 || !reflect.DeepEqual(storage.calls, want) {
				t.Fatal("unchanged callback repeated exports or regressed progress")
			}
		})
	}
}

func TestOCIReportingPreservesRegistryFailure(t *testing.T) {
	const message = "registry policy denied publication"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(message))
	}))
	defer srv.Close()
	job := completedOCIJob()
	job.Exports.OCI.Coordinates.OCIHost = srv.URL
	storage := &ociReportingStorage{job: job}
	exporter := evalcards.NewManager(nil, evalcards.ManagerConfig{
		OCIPublisherFactory: evalcards.NewOCIPublisherFactory(oci.NewLocalCredentialResolver(), srv.Client()),
	})
	var logs bytes.Buffer
	(&Handlers{resultsExporter: exporter}).exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
	status := job.Results.OCI.Status
	if status.State != api.OCIProcessingFailed || !strings.Contains(status.Message.Message, "HTTP status 403") || !strings.Contains(status.Message.Message, message) {
		t.Fatalf("registry diagnostics not persisted: %#v", status)
	}
	if !strings.Contains(logs.String(), message) || !strings.Contains(logs.String(), "job_id=job-1") {
		t.Fatalf("registry diagnostics not logged: %s", logs.String())
	}
	if job.Status.State != api.OverallStateCompleted || job.Results.OCI.EvaluationCard != nil {
		t.Fatal("registry failure changed evaluation completion or recorded publication success")
	}
}

func TestOCIReportingFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failAt      int
		failFrom    int
		unavailable bool
		exportError error
		emptyResult bool
		result      *evalcards.ExportResult
		wantCode    string
		wantMessage string
		wantCard    bool
		wantExport  bool
	}{
		{name: "publisher failure", exportError: &oci.OperationError{Operation: "resolve credentials", Reason: "tenant connection Secret was not found"}, wantCode: "oci_processing_failed", wantExport: true},
		{name: "missing published reference", emptyResult: true, wantCode: "oci_processing_failed", wantExport: true},
		{name: "untyped publisher error", result: &evalcards.ExportResult{OCIError: errors.New("publisher failed"), OCIArtifact: publishedCard()}, wantCode: "oci_processing_failed", wantMessage: "Publish evaluation card: OCI publisher failed", wantExport: true},
		{name: "missing digest", result: &evalcards.ExportResult{OCIArtifact: &api.OCIArtifactReference{OCIReference: publishedCard().OCIReference}}, wantCode: "oci_processing_failed", wantMessage: "Publish evaluation card: no published manifest reference is available", wantExport: true},
		{name: "missing reference", result: &evalcards.ExportResult{OCIArtifact: &api.OCIArtifactReference{OCIDigest: publishedCard().OCIDigest}}, wantCode: "oci_processing_failed", wantMessage: "Publish evaluation card: no published manifest reference is available", wantExport: true},
		{name: "running persistence", failAt: 1, wantCode: "oci_processing_failed"},
		{name: "completion persistence", failAt: 2, wantCode: "oci_processing_failed", wantCard: true, wantExport: true},
		{name: "unavailable storage", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := completedOCIJob()
			storage := &ociReportingStorage{job: job, failAt: tc.failAt, unavailable: tc.unavailable}
			exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard(), OCIError: tc.exportError}, err: tc.exportError}
			if tc.emptyResult {
				exporter.result = evalcards.ExportResult{}
			}
			if tc.result != nil {
				exporter.result = *tc.result
			}
			h := &Handlers{resultsExporter: exporter, bundlePublisherFactory: &stubBundleFactory{ref: publishedBundle()}}
			var logs bytes.Buffer
			h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
			if exporter.calls != 1 || exporter.ociEnabled != tc.wantExport {
				t.Fatalf("independent exporter calls=%d OCI enabled=%v", exporter.calls, exporter.ociEnabled)
			}
			wantStates := []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingFailed}
			if tc.failAt == 2 {
				wantStates = []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted, api.OCIProcessingFailed}
			}
			if tc.failAt == 2 && !reflect.DeepEqual(storage.bundles, []*api.OCIArtifactReference{nil, publishedBundle(), publishedBundle()}) {
				t.Fatalf("completion fallback lost bundle: %v", storage.bundles)
			}
			if !reflect.DeepEqual(storage.calls, wantStates) {
				t.Fatalf("progress = %v, want %v", storage.calls, wantStates)
			}
			if job.Status.State != api.OverallStateCompleted || job.Status.Message.MessageCode != "JOB_COMPLETED" {
				t.Fatal("evaluation completion changed")
			}
			if tc.unavailable {
				if job.Results.OCI != nil {
					t.Fatal("claimed API update while storage unavailable")
				}
			} else {
				if job.Results.OCI.Status.State != api.OCIProcessingFailed || job.Results.OCI.Status.Message.MessageCode != tc.wantCode {
					t.Fatalf("status = %#v", job.Results.OCI.Status)
				}
				wantMessage := tc.wantMessage
				if tc.exportError != nil {
					wantMessage = tc.exportError.Error()
				}
				if wantMessage != "" && job.Results.OCI.Status.Message.Message != wantMessage {
					t.Fatalf("export failure message = %q, want %q", job.Results.OCI.Status.Message.Message, wantMessage)
				}
				if (job.Results.OCI.EvaluationCard != nil) != tc.wantCard {
					t.Fatalf("artifact = %#v", job.Results.OCI.EvaluationCard)
				}
				if tc.failAt == 2 {
					if !reflect.DeepEqual(storage.cards, []*api.OCIArtifactReference{nil, publishedCard(), publishedCard()}) {
						t.Fatalf("completion failure did not save the published reference with failed status: states=%v cards=%v", storage.calls, storage.cards)
					}
				}
			}
			if !strings.Contains(logs.String(), "job_id=job-1") {
				t.Fatal("failure logs omit job identity")
			}
		})
	}
}

func TestOCIReportingEligibility(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStatePending, api.OverallStateRunning, api.OverallStateFailed, api.OverallStateCancelled, api.OverallStateCompleted} {
		t.Run(string(state), func(t *testing.T) {
			job := completedOCIJob()
			job.Status.State = state
			if state == api.OverallStateCompleted {
				job.Exports = nil
			}
			storage := &ociReportingStorage{job: job}
			exporter := &ociOutcomeExporter{}
			(&Handlers{resultsExporter: exporter}).exportEvaluationResults(context.Background(), storage, job, nil)
			if len(storage.calls) != 0 || job.Results.OCI != nil || exporter.calls != 1 {
				t.Fatal("changed export eligibility or added synthetic OCI output")
			}
		})
	}
}

func TestOCIReportingMissingExporterAndInvalidCard(t *testing.T) {
	for _, invalidCard := range []bool{false, true} {
		job := completedOCIJob()
		if invalidCard {
			job.Resource.ID = ""
		}
		storage := &ociReportingStorage{job: job}
		(&Handlers{}).exportEvaluationResults(context.Background(), storage, job, nil)
		if job.Results.OCI.Status.Message.MessageCode != "oci_processing_failed" {
			t.Fatalf("message = %#v", job.Results.OCI.Status.Message)
		}
	}
}

func TestOCIReportingWithoutStoragePreservesJobAndIndependentExports(t *testing.T) {
	job := completedOCIJob()
	before, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	exporter := &ociOutcomeExporter{result: evalcards.ExportResult{CardURL: "https://mlflow/card"}}
	var logs bytes.Buffer
	(&Handlers{resultsExporter: exporter}).exportEvaluationResults(context.Background(), nil, job, slog.New(slog.NewTextHandler(&logs, nil)))
	after, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || exporter.calls != 1 || exporter.ociEnabled {
		t.Fatalf("storage failure changed job or blocked independent exports: calls=%d OCI enabled=%v", exporter.calls, exporter.ociEnabled)
	}
	if !strings.Contains(logs.String(), "storage is not configured") {
		t.Fatalf("missing storage diagnostic: %s", logs.String())
	}
}

// TestOCIBundleWorkflow covers retained outputs, failure reporting, and persistence fallback for both eligible outcomes.
func TestOCIBundleWorkflow(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStateCompleted, api.OverallStatePartiallyFailed} {
		for _, scenario := range []string{"success", "factory failure", "missing factory", "publish failure", "missing output", "missing digest", "missing reference", "card failure", "completion save failure", "fallback save failure"} {
			t.Run(string(state)+"/"+scenario, func(t *testing.T) {
				job := completedOCIJob()
				job.Status.State = state
				before, _ := json.Marshal(job.Status)
				storage := &ociReportingStorage{job: job}
				factory := &stubBundleFactory{ref: publishedBundle()}
				exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}}
				h := &Handlers{resultsExporter: exporter, bundlePublisherFactory: factory}
				switch scenario {
				case "factory failure":
					factory.factoryError = errors.New("tenant Secret unavailable")
				case "missing factory":
					h.bundlePublisherFactory = nil
				case "publish failure":
					factory.publishError = errors.New("verify benchmark 3 manifest: registry denied")
				case "missing output":
					factory.ref = nil
				case "missing digest":
					factory.ref = &api.OCIArtifactReference{OCIReference: publishedBundle().OCIReference}
				case "missing reference":
					factory.ref = &api.OCIArtifactReference{OCIDigest: publishedBundle().OCIDigest}
				case "card failure":
					exporter.result.OCIError = errors.New("card failed")
				case "completion save failure", "fallback save failure":
					storage.failAt = 2
				}
				// Fail completion and fallback independently, after the running write.
				if scenario == "fallback save failure" {
					factory.publishError = nil
					storage.failFrom = 2
				}
				var logs bytes.Buffer
				h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
				if scenario == "factory failure" || scenario == "publish failure" {
					if count := strings.Count(logs.String(), "level=ERROR"); count != 1 {
						t.Fatalf("bundle failure logged %d times, want once: %s", count, logs.String())
					}
				}
				after, _ := json.Marshal(job.Status)
				if !bytes.Equal(before, after) {
					t.Fatal("evaluation outcome changed")
				}
				want := api.OCIProcessingFailed
				switch scenario {
				case "success":
					want = api.OCIProcessingCompleted
				case "fallback save failure":
					want = api.OCIProcessingRunning
				}
				if job.Results.OCI.Status.State != want {
					t.Fatalf("OCI state = %s, want %s", job.Results.OCI.Status.State, want)
				}
				wantCard := scenario != "card failure" && scenario != "fallback save failure"
				wantBundle := scenario == "success" || scenario == "completion save failure"
				if (job.Results.OCI.EvaluationCard != nil) != wantCard || (job.Results.OCI.EvaluationBundle != nil) != wantBundle {
					t.Fatalf("incorrect retained outputs: %+v", job.Results.OCI)
				}
				if scenario == "card failure" && factory.created != 0 {
					t.Fatal("card failure entered bundle stage")
				}
				if factory.published > 0 && !reflect.DeepEqual(factory.card, publishedCard()) {
					t.Fatal("bundle did not receive newly published card")
				}
				if scenario == "completion save failure" || scenario == "fallback save failure" {
					if len(storage.calls) != 3 || !reflect.DeepEqual(storage.cards[2], publishedCard()) || !reflect.DeepEqual(storage.bundles[2], publishedBundle()) {
						t.Fatal("fallback did not carry both published outputs")
					}
				} else if len(storage.calls) != 2 {
					t.Fatalf("unexpected writes: %v", storage.calls)
				}
				if scenario == "fallback save failure" && !strings.Contains(logs.String(), "database unavailable") {
					t.Fatal("missing persistence failure diagnostic")
				}
			})
		}
	}
}

// Exercise the real card exporter and bundle factory within a single handler invocation.
func TestOCIBundleWorkflowWithRegistry(t *testing.T) {
	for _, outcome := range []api.OverallState{api.OverallStateCompleted, api.OverallStatePartiallyFailed} {
		for _, failure := range []string{"", "missing child", "denied index"} {
			t.Run(string(outcome)+"/"+failure, func(t *testing.T) {
				child := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.empty.v1+json","size":2,"digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"},"layers":[]}`)
				digest := func(body []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(body)) }
				childDigest := digest(child)
				manifests := map[string][]byte{childDigest: child}
				indexPuts := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusOK)
						return
					}
					key := strings.TrimPrefix(r.URL.Path, "/v2/team/results/manifests/")
					if key == r.URL.Path {
						t.Errorf("unexpected registry request: %s", r.URL.Path)
						http.NotFound(w, r)
						return
					}
					if r.Method == http.MethodPut {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						if r.Header.Get("Content-Type") == "application/vnd.oci.image.index.v1+json" {
							indexPuts++
							if failure == "denied index" {
								http.Error(w, "bundle policy denied", http.StatusForbidden)
								return
							}
						}
						manifests[key], manifests[digest(body)] = body, body
						w.Header().Set("Docker-Content-Digest", digest(body))
						w.WriteHeader(http.StatusCreated)
						return
					}
					if r.Method == http.MethodGet {
						if key == childDigest && failure == "missing child" {
							http.NotFound(w, r)
							return
						}
						body, ok := manifests[key]
						if !ok {
							http.NotFound(w, r)
							return
						}
						w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
						w.Header().Set("Docker-Content-Digest", digest(body))
						_, _ = w.Write(body)
						return
					}
					t.Errorf("unexpected method: %s", r.Method)
				}))
				defer srv.Close()
				job := completedOCIJob()
				job.Status.State = outcome
				job.Status.Benchmarks = []api.BenchmarkStatus{{BenchmarkIndex: 0, ID: "bench", ProviderID: "provider", Status: api.StateCompleted}}
				job.Results.Benchmarks = []api.BenchmarkResult{{BenchmarkIndex: 0, ID: "bench", ProviderID: "provider", Artifacts: map[string]any{"oci_digest": childDigest}}}
				if outcome == api.OverallStatePartiallyFailed {
					job.Status.Benchmarks = append(job.Status.Benchmarks, api.BenchmarkStatus{BenchmarkIndex: 1, ID: "bench", ProviderID: "provider", Status: api.StateFailed})
				}
				job.Exports.OCI.Coordinates.OCIHost = srv.URL
				resolver := oci.NewLocalCredentialResolver()
				exporter := evalcards.NewManager(nil, evalcards.ManagerConfig{OCIPublisherFactory: evalcards.NewOCIPublisherFactory(resolver, srv.Client())})
				h := &Handlers{resultsExporter: exporter, bundlePublisherFactory: oci.NewBundlePublisherFactory(resolver, srv.Client())}
				storage := &ociReportingStorage{job: job}
				h.exportEvaluationResults(context.Background(), storage, job, nil)
				if job.Results.OCI.EvaluationCard == nil {
					t.Fatal("published card was lost")
				}
				if failure != "" {
					if job.Results.OCI.Status.State != api.OCIProcessingFailed || job.Results.OCI.EvaluationBundle != nil {
						t.Fatalf("incorrect failure: %+v", job.Results.OCI)
					}
					message := job.Results.OCI.Status.Message.Message
					if failure == "missing child" && (!strings.Contains(message, "benchmark 0") || !strings.Contains(message, "HTTP status 404") || indexPuts != 0) {
						t.Fatal(message)
					}
					if failure == "denied index" && (!strings.Contains(message, "HTTP status 403") || !strings.Contains(message, "bundle policy denied")) {
						t.Fatal(message)
					}
					return
				}
				if job.Results.OCI.Status.State != api.OCIProcessingCompleted || indexPuts != 1 || len(storage.calls) != 2 {
					t.Fatalf("workflow failed: %+v", job.Results.OCI)
				}
				bundle := job.Results.OCI.EvaluationBundle
				body := manifests["custom-job-1"]
				if bundle.OCIDigest != digest(body) {
					t.Fatal("bundle digest mismatch")
				}
				var index ociclient.Index
				if err := json.Unmarshal(body, &index); err != nil {
					t.Fatal(err)
				}
				if len(index.Manifests) != 2 || index.Manifests[0].Digest != job.Results.OCI.EvaluationCard.OCIDigest || index.Manifests[1].Digest != childDigest || index.Annotations["evaluation_state"] != string(outcome) {
					t.Fatalf("incorrect index: %+v", index)
				}
			})
		}
	}
}
