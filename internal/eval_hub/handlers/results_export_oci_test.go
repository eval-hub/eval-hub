package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
)

type ociReportingStorage struct {
	noopStorage
	job         *api.EvaluationJobResource
	calls       []api.OCIProcessingState
	cards       []*api.OCIArtifactReference
	bundles     []*api.OCIArtifactReference
	codes       []string
	failAt      int
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
	if s.unavailable || len(s.calls) == s.failAt {
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

func TestOCIReportingSuccessAndUnchangedTerminalCallback(t *testing.T) {
	job := completedOCIJob()
	before, _ := json.Marshal(job.Status)
	exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}, err: errors.New("MLflow upload failed")}
	storage := &ociReportingStorage{job: job}
	h := &Handlers{resultsExporter: exporter}
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
	if !reflect.DeepEqual(storage.bundles, []*api.OCIArtifactReference{nil, nil}) {
		t.Fatalf("card workflow supplied bundle references: %v", storage.bundles)
	}
	if !reflect.DeepEqual(job.Results.OCI.EvaluationCard, publishedCard()) || job.Results.OCI.EvaluationBundle != nil || job.Results.OCI.Signing != nil {
		t.Fatalf("OCI outputs = %#v", job.Results.OCI)
	}
	after, _ := json.Marshal(job.Status)
	if !bytes.Equal(before, after) || job.Results.Benchmarks[0].Metrics["accuracy"] != 0.7 {
		t.Fatal("evaluation status or benchmark results changed")
	}
	h.onEvaluationJobUpdated(context.Background(), storage, func() (*api.EvaluationJobResource, error) { return job, nil }, api.OverallStateCompleted, nil)
	if exporter.calls != 1 || !reflect.DeepEqual(storage.calls, want) {
		t.Fatal("unchanged callback repeated exports or regressed progress")
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
		unavailable bool
		exportError error
		emptyResult bool
		result      *evalcards.ExportResult
		wantCode    string
		wantMessage string
		wantCard    bool
		wantExport  bool
	}{
		{name: "publisher failure", exportError: &evalcards.OCIExportError{Operation: "resolve credentials", Reason: "tenant connection Secret was not found"}, wantCode: "oci_processing_failed", wantExport: true},
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
			h := &Handlers{resultsExporter: exporter}
			var logs bytes.Buffer
			h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
			if exporter.calls != 1 || exporter.ociEnabled != tc.wantExport {
				t.Fatalf("independent exporter calls=%d OCI enabled=%v", exporter.calls, exporter.ociEnabled)
			}
			wantStates := []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingFailed}
			if tc.failAt == 2 {
				wantStates = []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted, api.OCIProcessingFailed}
			}
			for _, bundle := range storage.bundles {
				if bundle != nil {
					t.Fatal("card workflow supplied a bundle")
				}
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
	for _, state := range []api.OverallState{api.OverallStateFailed, api.OverallStateCancelled, api.OverallStatePartiallyFailed, api.OverallStateCompleted} {
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
