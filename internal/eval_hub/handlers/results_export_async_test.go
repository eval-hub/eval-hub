package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type exportSpanRecorder struct {
	*tracetest.SpanRecorder
	done chan struct{}
}

func (r *exportSpanRecorder) OnEnd(span sdktrace.ReadOnlySpan) {
	r.SpanRecorder.OnEnd(span)
	if span.Name() == "evaluation-results-export" {
		close(r.done)
	}
}

func TestExportAsyncLinksRequestAndParentsDownstreamSpans(t *testing.T) {
	recorder := &exportSpanRecorder{SpanRecorder: tracetest.NewSpanRecorder(), done: make(chan struct{})}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = tp.Shutdown(context.Background())
	})

	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ctx, requestSpan := tp.Tracer("test").Start(parent, "update-evaluation-job")
	requestSC := requestSpan.SpanContext()
	requestSpan.End()
	cancel()

	job := completedOCIJob()
	storage := newDetachedExportStorage(job)
	h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exportFunc(func(ctx context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (evalcards.ExportResult, error) {
		if ctx.Err() != nil {
			t.Error("export inherited request cancellation")
		}
		if _, ok := ctx.Deadline(); ok {
			t.Error("export inherited request deadline")
		}
		_, child := otel.Tracer("test").Start(ctx, "registry-request")
		child.End()
		return evalcards.ExportResult{OCIArtifact: publishedCard()}, nil
	})}
	h.exportEvaluationResultsAsync(ctx, storage, job, nil)
	awaitExportSignal(t, recorder.done)

	var export, child sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		switch span.Name() {
		case "evaluation-results-export":
			export = span
		case "registry-request":
			child = span
		}
	}
	if export == nil || child == nil {
		t.Fatal("missing export or downstream span")
	}
	if export.Parent().IsValid() || export.SpanContext().TraceID() == requestSC.TraceID() {
		t.Error("export should be a root in a separate trace")
	}
	links := export.Links()
	if len(links) != 1 || links[0].SpanContext.TraceID() != requestSC.TraceID() || links[0].SpanContext.SpanID() != requestSC.SpanID() {
		t.Errorf("export links = %v, want one link to %v", links, requestSC)
	}
	if child.Parent().SpanID() != export.SpanContext().SpanID() || child.SpanContext().TraceID() != export.SpanContext().TraceID() {
		t.Error("downstream span should be a child of the export span")
	}
	nextStatusWrite(t, storage, api.OCIProcessingRunning)
	nextStatusWrite(t, storage, api.OCIProcessingCompleted)
}

func awaitExportSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("export did not reach expected stage")
	}
}

type exportFunc func(context.Context, *api.EvaluationJobResource, *cards.EvaluationCard) (evalcards.ExportResult, error)

func (f exportFunc) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (evalcards.ExportResult, error) {
	return f(ctx, job, card)
}

type statusWrite struct {
	state   api.OCIProcessingState
	card    *api.OCIArtifactReference
	ctx     context.Context
	err     error
	message string
}

// Rebinding returns a copy, like SQL storage, and retains tenant/owner scoping.
// Unlike a permissive stub, every read/write rejects a canceled context.
type detachedExportStorage struct {
	noopStorage
	ctx            context.Context
	job            *api.EvaluationJobResource
	writes         chan statusWrite
	runningStarted chan struct{}
	releaseRunning chan struct{}
	failRunning    bool
	failCompleted  bool
	panicCompleted bool
	panicFailure   bool
	updated        context.CancelFunc
	tenant         api.Tenant
	owner          api.User
}

func (s *detachedExportStorage) WithContext(ctx context.Context) abstractions.Storage {
	cp := *s
	cp.ctx = ctx
	return &cp
}
func (s *detachedExportStorage) WithLogger(*slog.Logger) abstractions.Storage { return s }
func (s *detachedExportStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	cp := *s
	cp.tenant = tenant
	return &cp
}
func (s *detachedExportStorage) WithOwner(owner api.User) abstractions.Storage {
	cp := *s
	cp.owner = owner
	return &cp
}
func (s *detachedExportStorage) GetEvaluationJob(string) (*api.EvaluationJobResource, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if s.updated != nil && s.job.Status.State.IsTerminalState() {
		if _, bounded := s.ctx.Deadline(); !bounded {
			return nil, errors.New("handoff read has no deadline")
		}
	}
	return s.job, nil
}
func (s *detachedExportStorage) UpdateEvaluationJob(string, *api.StatusEvent) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.job.Status.State = api.OverallStateCompleted
	if s.updated != nil {
		s.updated()
	}
	return nil
}
func (s *detachedExportStorage) UpdateEvaluationJobOCI(_ string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if status.State == api.OCIProcessingRunning && s.runningStarted != nil {
		close(s.runningStarted)
		<-s.releaseRunning
	}
	err := s.ctx.Err()
	if _, bounded := s.ctx.Deadline(); !bounded {
		err = errors.New("status write has no deadline")
	}
	if s.tenant != "tenant-a" || s.owner != "owner-a" {
		err = errors.New("lost storage scoping")
	}
	if status.State == api.OCIProcessingRunning && s.failRunning {
		err = errors.New("running write failed")
	}
	if status.State == api.OCIProcessingCompleted && s.failCompleted {
		err = errors.New("completion write failed")
	}
	s.writes <- statusWrite{status.State, card, s.ctx, err, status.Message.Message}
	if s.panicCompleted && status.State == api.OCIProcessingCompleted {
		panic("completion write panicked")
	}
	if s.panicFailure && status.State == api.OCIProcessingFailed {
		panic("failure write panicked")
	}
	return err
}
func nextStatusWrite(t *testing.T, s *detachedExportStorage, want api.OCIProcessingState) statusWrite {
	t.Helper()
	select {
	case w := <-s.writes:
		if w.state != want || w.err != nil {
			t.Fatalf("status write = %+v, want %s with active bounded context", w, want)
		}
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("missing status write")
		return statusWrite{}
	}
}
func newDetachedExportStorage(job *api.EvaluationJobResource) *detachedExportStorage {
	return &detachedExportStorage{ctx: context.Background(), job: job, writes: make(chan statusWrite, 8), tenant: "tenant-a", owner: "owner-a"}
}

func TestExportAsyncDoesNotWaitForRunningWrite(t *testing.T) {
	job := completedOCIJob()
	// Exercise copying an existing OCI structure as well as its containing Results.
	job.Results.OCI = &api.EvaluationOCIResults{EvaluationBundle: publishedCard()}
	storage := newDetachedExportStorage(job)
	storage.runningStarted, storage.releaseRunning = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exportFunc(func(ctx context.Context, snapshot *api.EvaluationJobResource, _ *cards.EvaluationCard) (evalcards.ExportResult, error) {
		if err := ctx.Err(); err != nil {
			return evalcards.ExportResult{OCIError: err}, err
		}
		if snapshot == job || snapshot.Results == job.Results || snapshot.Results.OCI == job.Results.OCI {
			return evalcards.ExportResult{}, errors.New("snapshot shares mutable fields")
		}
		return evalcards.ExportResult{OCIArtifact: publishedCard()}, nil
	})}
	returned := make(chan struct{})
	go func() {
		h.onEvaluationJobUpdated(ctx, storage, func(readCtx context.Context) (*api.EvaluationJobResource, error) {
			return storage.WithContext(readCtx).GetEvaluationJob("job-1")
		}, api.OverallStateRunning, nil)
		close(returned)
	}()
	awaitExportSignal(t, storage.runningStarted)
	awaitExportSignal(t, returned)
	cancel()
	// Concurrent readers continue using the original while the worker writes its copy.
	readingDone := make(chan struct{})
	stopReading := make(chan struct{})
	go func() {
		defer close(readingDone)
		for {
			select {
			case <-stopReading:
				return
			default:
				_ = cards.NewEvaluationCard(job)
				_ = job.Results.OCI.Status
			}
		}
	}()
	close(storage.releaseRunning)
	nextStatusWrite(t, storage, api.OCIProcessingRunning)
	nextStatusWrite(t, storage, api.OCIProcessingCompleted)
	close(stopReading)
	awaitExportSignal(t, readingDone)
	if job.Results.OCI.Status != nil || job.Results.OCI.EvaluationCard != nil {
		t.Fatal("worker mutated original OCI results")
	}
}

func TestExportAsyncSurvivesRequestCancellationDuringExport(t *testing.T) {
	job := completedOCIJob()
	storage := newDetachedExportStorage(job)
	started, release, checked := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exportFunc(func(ctx context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (evalcards.ExportResult, error) {
		close(started)
		<-release
		err := ctx.Err()
		close(checked)
		return evalcards.ExportResult{OCIArtifact: publishedCard(), OCIError: err}, err
	})}
	h.exportEvaluationResultsAsync(ctx, storage, job, nil)
	awaitExportSignal(t, started)
	cancel()
	close(release)
	awaitExportSignal(t, checked)
	nextStatusWrite(t, storage, api.OCIProcessingRunning)
	if w := nextStatusWrite(t, storage, api.OCIProcessingCompleted); w.card == nil {
		t.Fatal("completion lost published artifact")
	}
}

func TestExportAsyncPreservesPublishedCardOnCompletionFailure(t *testing.T) {
	for _, panicCompleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "storage error", true: "storage panic"}[panicCompleted], func(t *testing.T) {
			job := completedOCIJob()
			storage := newDetachedExportStorage(job)
			storage.failCompleted = true
			storage.panicCompleted = panicCompleted
			published := publishedCard()
			exporter := &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: published}}
			h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exporter}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			h.exportEvaluationResultsAsync(ctx, storage, job, nil)
			running := nextStatusWrite(t, storage, api.OCIProcessingRunning)
			if running.card != nil {
				t.Fatal("running write recorded an unpublished artifact")
			}
			var completed statusWrite
			select {
			case completed = <-storage.writes:
				if completed.state != api.OCIProcessingCompleted || completed.err == nil || !reflect.DeepEqual(completed.card, published) {
					t.Fatalf("completion attempt = %+v", completed)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("missing completion attempt")
			}
			failed := nextStatusWrite(t, storage, api.OCIProcessingFailed)
			if !reflect.DeepEqual(failed.card, published) {
				t.Fatalf("failure lost published reference: got %+v, want %+v", failed.card, published)
			}
			if failed.ctx == completed.ctx || failed.ctx == running.ctx {
				t.Fatal("failure reused a previous write context")
			}
			if exporter.calls != 1 {
				t.Fatalf("export calls = %d, want 1", exporter.calls)
			}
			if job.Status.State != api.OverallStateCompleted || job.Results.OCI != nil {
				t.Fatal("worker mutated original job")
			}
		})
	}
}

func TestDetachedHandoffAfterCommittedRuntimeUpdate(t *testing.T) {
	job := completedOCIJob()
	job.Status.State = api.OverallStateRunning
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	storage := newDetachedExportStorage(job)
	storage.updated = cancel // Cancel at the commit boundary, before the handoff read.
	h := &Handlers{storage: storage, validate: newOCIWorkflowValidator(t), resultsExporter: &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}}}
	runtime := &runtimeStorage{ctx: ctx, logger: slog.New(slog.DiscardHandler), handlers: h, tenant: "tenant-a", owner: "owner-a", validate: h.validate}
	if err := runtime.UpdateEvaluationJob("job-1", &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{ProviderID: "p1", ID: "b1", Status: api.StateCompleted}}); err != nil {
		t.Fatal(err)
	}
	nextStatusWrite(t, storage, api.OCIProcessingRunning)
	nextStatusWrite(t, storage, api.OCIProcessingCompleted)
}

func TestExportInitialWriteFailureKeepsIndependentExports(t *testing.T) {
	job := completedOCIJob()
	storage := newDetachedExportStorage(job)
	storage.failRunning = true
	exported := make(chan struct{})
	h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exportFunc(func(ctx context.Context, snapshot *api.EvaluationJobResource, _ *cards.EvaluationCard) (evalcards.ExportResult, error) {
		defer close(exported)
		if api.IsOCIExportEnabled(snapshot) || ctx.Err() != nil {
			t.Error("independent export got OCI or canceled context")
			return evalcards.ExportResult{}, errors.New("independent export got OCI or canceled context")
		}
		return evalcards.ExportResult{CardURL: "mlflow/card"}, nil
	})}
	h.exportEvaluationResultsAsync(context.Background(), storage, job, nil)
	select {
	case w := <-storage.writes:
		if w.state != api.OCIProcessingRunning || w.err == nil {
			t.Fatalf("initial write: %+v", w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing running attempt")
	}
	nextStatusWrite(t, storage, api.OCIProcessingFailed)
	awaitExportSignal(t, exported)
	if job.Exports.OCI == nil {
		t.Fatal("worker changed original export configuration")
	}
}

func TestExportWorkerRecoversPanic(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "publisher", true: "failure writer"}[nested], func(t *testing.T) {
			job := completedOCIJob()
			storage := newDetachedExportStorage(job)
			storage.panicFailure = nested
			done := make(chan struct{})
			var logs bytes.Buffer
			h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exportFunc(func(context.Context, *api.EvaluationJobResource, *cards.EvaluationCard) (evalcards.ExportResult, error) {
				panic("publisher panicked")
			})}
			// Run the worker directly with an owned job so completion is observable even
			// when the failure writer panics. Its production caller launches this in a goroutine.
			go func() {
				defer close(done)
				h.exportEvaluationResults(context.Background(), storage, job, slog.New(slog.NewTextHandler(&logs, nil)))
			}()
			awaitExportSignal(t, done)
			nextStatusWrite(t, storage, api.OCIProcessingRunning)
			failed := nextStatusWrite(t, storage, api.OCIProcessingFailed)
			if !strings.Contains(logs.String(), "stack=") || !strings.Contains(logs.String(), "publisher panicked") {
				t.Fatal("panic and stack missing from logs")
			}
			if strings.Contains(failed.message, "goroutine") || failed.message != "Evaluation-card OCI processing panicked" {
				t.Fatalf("panic status message: %s", failed.message)
			}
			if nested && !strings.Contains(logs.String(), "failure write panicked") {
				t.Fatal("nested panic not logged")
			}
		})
	}
}

type asyncPublisherFactory struct{ publisher evalcards.OCIPublisher }

func (f asyncPublisherFactory) NewPublisher(context.Context, *api.EvaluationJobResource) (evalcards.OCIPublisher, error) {
	return f.publisher, nil
}

type asyncPublisherFunc func(context.Context, []byte) (*api.OCIArtifactReference, error)

func (f asyncPublisherFunc) PublishEvalCard(ctx context.Context, data []byte) (*api.OCIArtifactReference, error) {
	return f(ctx, data)
}
func (asyncPublisherFunc) Close() error { return nil }

func TestExportPushTimeoutUsesFreshFailureContext(t *testing.T) {
	storage := newDetachedExportStorage(completedOCIJob())
	pushStarted := make(chan struct{})
	expired := make(chan struct{})
	factory := asyncPublisherFactory{publisher: asyncPublisherFunc(func(ctx context.Context, _ []byte) (*api.OCIArtifactReference, error) {
		close(pushStarted)
		<-ctx.Done()
		close(expired)
		return nil, ctx.Err()
	})}
	h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: evalcards.NewManager(nil, evalcards.ManagerConfig{OCIPublisherFactory: factory, OCIPushTimeout: time.Millisecond})}
	h.exportEvaluationResultsAsync(context.Background(), storage, storage.job, nil)
	awaitExportSignal(t, pushStarted)
	awaitExportSignal(t, expired)
	running := nextStatusWrite(t, storage, api.OCIProcessingRunning)
	failed := nextStatusWrite(t, storage, api.OCIProcessingFailed)
	if !strings.Contains(failed.message, context.DeadlineExceeded.Error()) {
		t.Fatalf("timeout cause missing: %s", failed.message)
	}
	if running.ctx == failed.ctx {
		t.Fatal("failure reused the running-write context")
	}
}

type asyncEventRequest struct{ httpwrappers.RequestWrapper }

func (asyncEventRequest) PathValue(string) string { return "job-1" }
func (asyncEventRequest) BodyAsBytes() ([]byte, error) {
	return []byte(`{"benchmark_status_event":{"provider_id":"p1","id":"b1","status":"completed"}}`), nil
}

type asyncEventResponse struct {
	httpwrappers.ResponseWrapper
	code int
	err  error
}

func (r *asyncEventResponse) WriteJSON(_ any, code int, _ ...any) { r.code = code }
func (r *asyncEventResponse) Error(err error, _ string)           { r.err = err }

func TestHandleTerminalEventReturns204BeforeRunningWrite(t *testing.T) {
	for _, cancelAtCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel after 204", true: "cancel at commit"}[cancelAtCommit], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			job := completedOCIJob()
			job.Status.State = api.OverallStateRunning
			storage := newDetachedExportStorage(job)
			storage.runningStarted, storage.releaseRunning = make(chan struct{}), make(chan struct{})
			if cancelAtCommit {
				storage.updated = cancel
			}
			h := &Handlers{storage: storage, validate: newOCIWorkflowValidator(t), resultsExporter: &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}}}
			response := &asyncEventResponse{}
			returned := make(chan struct{})
			go func() {
				h.HandleUpdateEvaluation(executioncontext.NewExecutionContext(ctx, "request", slog.New(slog.DiscardHandler), "owner-a", "tenant-a"), asyncEventRequest{}, response)
				close(returned)
			}()
			awaitExportSignal(t, storage.runningStarted)
			awaitExportSignal(t, returned)
			if response.code != 204 || response.err != nil {
				t.Fatalf("response: %+v", response)
			}
			cancel()
			close(storage.releaseRunning)
			nextStatusWrite(t, storage, api.OCIProcessingRunning)
			nextStatusWrite(t, storage, api.OCIProcessingCompleted)
		})
	}
}
