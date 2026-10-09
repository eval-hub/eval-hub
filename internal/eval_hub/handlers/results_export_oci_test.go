package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type ociReportingStorage struct {
	noopStorage
	job         *api.EvaluationJobResource
	calls       []api.OCIProcessingState
	failAt      int
	unavailable bool
}

func (s *ociReportingStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.job, nil
}
func (s *ociReportingStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if id != s.job.Resource.ID {
		return errors.New("wrong job")
	}
	s.calls = append(s.calls, status.State)
	if s.unavailable || len(s.calls) == s.failAt {
		return errors.New("database unavailable")
	}
	if status.Message == nil || status.Message.Message == "" || status.Message.MessageCode == "" || status.Message.MessageOrigin != api.MessageOriginServer {
		return errors.New("incomplete message")
	}
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
	want := []api.OCIProcessingState{api.OCIProcessingPending, api.OCIProcessingGeneratingEvaluationCard, api.OCIProcessingGeneratingEvaluationCard, api.OCIProcessingCompleted}
	if !reflect.DeepEqual(storage.calls, want) {
		t.Fatalf("progress = %v", storage.calls)
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

func TestOCIReportingFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failAt      int
		unavailable bool
		exportError error
		emptyResult bool
		wantCode    string
		wantCard    bool
		wantExport  bool
	}{
		{name: "publisher failure", exportError: &evalcards.OCIExportError{Code: "OCI_CARD_ACCESS_FAILED", Operation: "resolve credentials", Cause: "tenant connection Secret was not found"}, wantCode: "OCI_CARD_ACCESS_FAILED", wantExport: true},
		{name: "missing published reference", emptyResult: true, wantCode: "OCI_CARD_PUBLICATION_FAILED", wantExport: true},
		{name: "pending persistence", failAt: 1, wantCode: "OCI_CARD_REPORTING_FAILED"},
		{name: "active progress persistence", failAt: 2, wantCode: "OCI_CARD_REPORTING_FAILED"},
		{name: "reference persistence", failAt: 3, wantCode: "OCI_CARD_REPORTING_FAILED", wantExport: true},
		{name: "completion persistence", failAt: 4, wantCode: "OCI_CARD_REPORTING_FAILED", wantCard: true, wantExport: true},
		{name: "unavailable storage", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := completedOCIJob()
			storage := &ociReportingStorage{job: job, failAt: tc.failAt, unavailable: tc.unavailable}
			exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard(), OCIError: tc.exportError}, err: tc.exportError}
			if tc.emptyResult {
				exporter.result = evalcards.ExportResult{}
			}
			h := &Handlers{resultsExporter: exporter}
			var logs bytes.Buffer
			h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
			if exporter.calls != 1 || exporter.ociEnabled != tc.wantExport {
				t.Fatalf("independent exporter calls=%d OCI enabled=%v", exporter.calls, exporter.ociEnabled)
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
				if (job.Results.OCI.EvaluationCard != nil) != tc.wantCard {
					t.Fatalf("artifact = %#v", job.Results.OCI.EvaluationCard)
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
		want := "OCI_CARD_ACCESS_FAILED"
		if invalidCard {
			want = "OCI_CARD_GENERATION_FAILED"
		}
		if job.Results.OCI.Status.Message.MessageCode != want {
			t.Fatalf("message = %#v", job.Results.OCI.Status.Message)
		}
	}
}
