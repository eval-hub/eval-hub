package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-playground/validator/v10"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type ociReportingStorage struct {
	noopStorage
	job         *api.EvaluationJobResource
	calls       []api.OCIProcessingState
	cards       []*api.OCIArtifactReference
	codes       []string
	failAt      int
	unavailable bool
	done        chan struct{}
}

func (s *ociReportingStorage) WithContext(context.Context) abstractions.Storage { return s }

func (s *ociReportingStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.job, nil
}
func (s *ociReportingStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if s.done != nil && api.IsOCIProcessingTerminalState(status.State) {
		defer close(s.done)
	}
	if id != s.job.Resource.ID {
		return errors.New("wrong job")
	}
	s.calls = append(s.calls, status.State)
	s.cards = append(s.cards, card)
	if s.unavailable || len(s.calls) == s.failAt {
		return errors.New("database unavailable")
	}
	if status.Message == nil || status.Message.Message == "" || status.Message.MessageCode == "" || status.Message.MessageOrigin != api.MessageOriginServer {
		return errors.New("incomplete message")
	}
	s.codes = append(s.codes, status.Message.MessageCode)
	if s.job.Results.OCI == nil {
		s.job.Results.OCI = &api.EvaluationOCIResults{}
	}
	s.job.Results.OCI.Status = status
	if card != nil {
		s.job.Results.OCI.EvaluationCard = card
	}
	return nil
}

type ociOutcomeExporter struct {
	result     evalcards.ExportResult
	err        error
	calls      int
	ociEnabled bool
	done       chan struct{}
}

func (e *ociOutcomeExporter) Export(_ context.Context, job *api.EvaluationJobResource, _ *cards.EvaluationCard) (evalcards.ExportResult, error) {
	if e.done != nil {
		defer close(e.done)
	}
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
	storage := &ociReportingStorage{job: job, done: make(chan struct{})}
	h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}
	h.onEvaluationJobUpdated(context.Background(), storage, func(context.Context) (*api.EvaluationJobResource, error) { return job, nil }, api.OverallStateRunning, nil)
	awaitExportSignal(t, storage.done)
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
	if !reflect.DeepEqual(job.Results.OCI.EvaluationCard, publishedCard()) || job.Results.OCI.EvaluationBundle != nil || job.Results.OCI.Signing != nil {
		t.Fatalf("OCI outputs = %#v", job.Results.OCI)
	}
	after, _ := json.Marshal(job.Status)
	if !bytes.Equal(before, after) || job.Results.Benchmarks[0].Metrics["accuracy"] != 0.7 {
		t.Fatal("evaluation status or benchmark results changed")
	}
	h.onEvaluationJobUpdated(context.Background(), storage, func(context.Context) (*api.EvaluationJobResource, error) { return job, nil }, api.OverallStateCompleted, nil)
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
	(&Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}).exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
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
		done        chan struct{}
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
		{name: "missing digest", result: &evalcards.ExportResult{OCIArtifact: &api.OCIArtifactReference{OCIReference: publishedCard().OCIReference}}, wantCode: "oci_processing_failed", wantMessage: "Publish evaluation card: publisher returned an invalid manifest reference", wantExport: true},
		{name: "missing reference", result: &evalcards.ExportResult{OCIArtifact: &api.OCIArtifactReference{OCIDigest: publishedCard().OCIDigest}}, wantCode: "oci_processing_failed", wantMessage: "Publish evaluation card: publisher returned an invalid manifest reference", wantExport: true},
		{name: "running persistence", failAt: 1, wantCode: "oci_processing_failed", wantMessage: "Persist running OCI progress: storage update failed"},
		{name: "completion persistence", failAt: 2, wantCode: "oci_processing_failed", wantMessage: "Persist evaluation-card OCI completion: storage update failed", wantCard: true, wantExport: true},
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
			h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}
			var logs bytes.Buffer
			h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
			if exporter.calls != 1 || exporter.ociEnabled != tc.wantExport {
				t.Fatalf("independent exporter calls=%d OCI enabled=%v", exporter.calls, exporter.ociEnabled)
			}
			wantStates := []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingFailed}
			if tc.failAt == 2 {
				wantStates = []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted, api.OCIProcessingFailed}
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
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/oci=%t", state, enabled), func(t *testing.T) {
				job := completedOCIJob()
				job.Status.State = state
				if !enabled {
					job.Exports = nil
				}
				storage := &ociReportingStorage{job: job, done: make(chan struct{})}
				exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}, done: make(chan struct{})}
				(&Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}).exportEvaluationResultsAsync(context.Background(), storage, job, nil)
				eligible := state == api.OverallStateCompleted || state == api.OverallStatePartiallyFailed
				wantCalls := 0
				if eligible {
					awaitExportSignal(t, exporter.done)
					if enabled {
						awaitExportSignal(t, storage.done)
					}
					wantCalls = 1
				}
				if exporter.calls != wantCalls {
					t.Fatalf("export calls=%d, want %d", exporter.calls, wantCalls)
				}
				if eligible && enabled {
					if !exporter.ociEnabled || !reflect.DeepEqual(storage.calls, []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted}) || !reflect.DeepEqual(job.Results.OCI.EvaluationCard, publishedCard()) {
						t.Fatal("eligible job did not publish and persist its OCI card")
					}
				} else if len(storage.calls) != 0 || job.Results.OCI != nil {
					t.Fatal("ineligible job received OCI updates")
				}
			})
		}
	}
}

func TestOCIReportingMissingExporterAndInvalidCard(t *testing.T) {
	for _, invalidCard := range []bool{false, true} {
		job := completedOCIJob()
		if invalidCard {
			job.Resource.ID = ""
		}
		storage := &ociReportingStorage{job: job}
		(&Handlers{validate: newOCIWorkflowValidator(t)}).exportEvaluationResults(context.Background(), storage, job, nil)
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
	(&Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}).exportEvaluationResults(context.Background(), nil, job, slog.New(slog.NewTextHandler(&logs, nil)))
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

func newOCIWorkflowValidator(t *testing.T) *validator.Validate {
	t.Helper()
	v, err := validation.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPersistOCIResultsValidation(t *testing.T) {
	validStatus := func() *api.OCIProcessingStatus {
		return &api.OCIProcessingStatus{State: api.OCIProcessingRunning, Message: &api.MessageInfo{Message: "Running", MessageCode: "running", MessageOrigin: api.MessageOriginServer}}
	}
	for _, name := range []string{"nil status", "invalid state", "missing message", "missing text", "missing code", "empty artifact", "malformed digest", "blank reference", "storage failure", "running", "completed", "failed"} {
		t.Run(name, func(t *testing.T) {
			job := completedOCIJob()
			storage := &ociReportingStorage{job: job}
			h := &Handlers{validate: newOCIWorkflowValidator(t)}
			status := validStatus()
			var card *api.OCIArtifactReference
			valid := false
			switch name {
			case "nil status":
				status = nil
			case "invalid state":
				status.State = "invalid"
			case "missing message":
				status.Message = nil
			case "missing text":
				status.Message.Message = ""
			case "missing code":
				status.Message.MessageCode = ""
			case "empty artifact":
				card = &api.OCIArtifactReference{}
			case "malformed digest":
				card = publishedCard()
				card.OCIDigest = "sha256:bad"
			case "blank reference":
				card = publishedCard()
				card.OCIReference = "  "
			case "storage failure":
				storage.failAt = 1
			case "running":
				valid = true
			case "completed":
				valid = true
				status.State = api.OCIProcessingCompleted
				card = publishedCard()
			case "failed":
				valid = true
				status.State = api.OCIProcessingFailed
			}
			before, _ := json.Marshal(job.Results)
			err := h.persistOCIResults(storage, job, status, card)
			if valid {
				if err != nil || len(storage.calls) != 1 || job.Results.OCI.Status != status {
					t.Fatalf("valid update failed: %v", err)
				}
			} else {
				after, _ := json.Marshal(job.Results)
				if err == nil || !bytes.Equal(before, after) {
					t.Fatalf("invalid or failed update changed results: %v", err)
				}
				if name != "storage failure" && len(storage.calls) != 0 {
					t.Fatal("invalid update reached storage")
				}
			}
		})
	}
}

func TestOCIInvalidCompletionArtifactIsNotRetried(t *testing.T) {
	for _, card := range []*api.OCIArtifactReference{{}, {OCIDigest: "sha256:bad", OCIReference: "registry/card"}, {OCIDigest: publishedCard().OCIDigest, OCIReference: " "}} {
		job := completedOCIJob()
		storage := &ociReportingStorage{job: job}
		exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: card}}
		h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}
		var logs bytes.Buffer
		h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
		if job.Results.OCI.Status.Message.Message != "Publish evaluation card: publisher returned an invalid manifest reference" {
			t.Fatalf("invalid artifact diagnosis = %q", job.Results.OCI.Status.Message.Message)
		}
		if !strings.Contains(logs.String(), "Invalid published OCI evaluation-card reference") || !strings.Contains(logs.String(), "OCIArtifactReference.") {
			t.Fatalf("artifact validation details not logged: %s", logs.String())
		}
		if !reflect.DeepEqual(storage.calls, []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingFailed}) || job.Results.OCI.EvaluationCard != nil {
			t.Fatalf("invalid artifact persisted: states=%v", storage.calls)
		}
		for _, saved := range storage.cards {
			if saved != nil {
				t.Fatal("retried invalid artifact")
			}
		}
	}
}

func TestOCIReportingValidationFailurePreventsPublishing(t *testing.T) {
	job := completedOCIJob()
	storage := &ociReportingStorage{job: job}
	exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}}
	v := newOCIWorkflowValidator(t)
	// Reject generated statuses to exercise both running and failure-reporting validation.
	v.RegisterStructValidation(func(sl validator.StructLevel) {
		sl.ReportError(sl.Current().Interface(), "State", "State", "workflow", "")
	}, api.OCIProcessingStatus{})
	h := &Handlers{validate: v, resultsExporter: exporter}
	h.exportEvaluationResults(context.Background(), storage, job, nil)
	if len(storage.calls) != 0 || job.Results.OCI != nil || exporter.ociEnabled {
		t.Fatal("validation failure persisted progress or allowed OCI publishing")
	}
}
