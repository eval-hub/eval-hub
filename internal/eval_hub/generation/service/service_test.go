package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/local"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type generationTestStorage struct {
	abstractions.Storage
	job               *api.EvaluationJobResource
	createErr         error
	updateErr         error
	protectErr        error
	terminalOnProtect bool
	getErr            error
	deleteErr         error
	created           bool
	deleted           bool
	protected         bool
	protectCalls      int
	workload          workloads.Type
	tenant            api.Tenant
	owner             api.User
	requestContext    context.Context
}

func validRequest() api.CollectionGenerationRequest {
	return api.CollectionGenerationRequest{
		EvaluationGoal: "Assess safe tool use",
		CollectionName: "Safety checks",
		Model: api.ModelRef{
			URL:  "https://model.example/v1",
			Name: "model",
			Auth: &api.ModelAuth{SecretRef: "model-auth"},
		},
	}
}

func (s *generationTestStorage) WithLogger(*slog.Logger) abstractions.Storage { return s }
func (s *generationTestStorage) WithContext(ctx context.Context) abstractions.Storage {
	s.requestContext = ctx
	return s
}
func (s *generationTestStorage) WithWorkloadType(value workloads.Type) abstractions.Storage {
	s.workload = value
	return s
}
func (s *generationTestStorage) WithTenant(value api.Tenant) abstractions.Storage {
	s.tenant = value
	return s
}
func (s *generationTestStorage) WithOwner(value api.User) abstractions.Storage {
	s.owner = value
	return s
}
func (s *generationTestStorage) CreateEvaluationJob(job *api.EvaluationJobResource) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.job, s.created = job, true
	return nil
}
func (s *generationTestStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.job == nil || s.job.Resource.ID != id || s.deleted {
		return nil, errors.New("job not found")
	}
	job := *s.job
	if s.job.Status != nil {
		status := *s.job.Status
		job.Status = &status
	}
	if s.job.Results != nil {
		results := *s.job.Results
		job.Results = &results
	}
	return &job, nil
}
func (s *generationTestStorage) UpdateEvaluationJob(id string, event *api.StatusEvent) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.job == nil || s.job.Resource.ID != id {
		return errors.New("job not found")
	}
	if event != nil && event.BenchmarkStatusEvent != nil {
		switch event.BenchmarkStatusEvent.Status {
		case api.StateCompleted:
			s.job.Status.State = api.OverallStateCompleted
		case api.StateCancelled:
			s.job.Status.State = api.OverallStateCancelled
		default:
			s.job.Status.State = api.OverallStateFailed
		}
		s.job.Status.Message = event.BenchmarkStatusEvent.ErrorMessage
	}
	return nil
}
func (s *generationTestStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	if s.job == nil || s.job.Resource.ID != id {
		return errors.New("job not found")
	}
	s.job.Status.State, s.job.Status.Message = state, message
	return nil
}
func (s *generationTestStorage) DeleteCleanedCollectionGenerationJob(id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if s.job == nil || s.job.Resource.ID != id {
		return errors.New("job not found")
	}
	s.deleted = true
	return nil
}
func (s *generationTestStorage) ProtectCollectionGenerationJobForCleanup(id string, message *api.MessageInfo) (*api.EvaluationJobResource, error) {
	s.protectCalls++
	if s.protectErr != nil {
		return nil, s.protectErr
	}
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.job == nil || s.job.Resource.ID != id || s.job.Status == nil || !generation.IsJob(&s.job.EvaluationJobConfig) {
		return nil, errors.New("job not found")
	}
	if s.terminalOnProtect {
		s.job.Status.State = api.OverallStateCompleted
	} else if !s.job.Status.State.IsTerminalState() {
		s.job.Status.State = api.OverallStateCancelled
		s.job.Status.Message = message
	}
	s.protected = true
	return s.GetEvaluationJob(id)
}

type generationTestRuntime struct {
	abstractions.Runtime
	ctx             context.Context
	runErr          error
	deleteErr       error
	secretErr       error
	runCalls        int
	deleteCalls     int
	secretChecks    int
	secretNamespace string
	secretName      string
	runJob          *api.EvaluationJobResource
	deleteJob       *api.EvaluationJobResource
}

func (r *generationTestRuntime) WithLogger(*slog.Logger) abstractions.Runtime { return r }
func (r *generationTestRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	r.ctx = ctx
	return r
}
func (r *generationTestRuntime) Name() string                       { return "generation-test" }
func (r *generationTestRuntime) SupportsCollectionGeneration() bool { return true }
func (r *generationTestRuntime) RunEvaluationJob(job *api.EvaluationJobResource, _ []api.EvaluationBenchmarkConfig, _ abstractions.RuntimeStorage) error {
	r.runCalls++
	r.runJob = job
	return r.runErr
}
func (r *generationTestRuntime) DeleteEvaluationJobResources(job *api.EvaluationJobResource) error {
	r.deleteCalls++
	r.deleteJob = job
	return r.deleteErr
}
func (r *generationTestRuntime) ValidateModelSecret(_ context.Context, namespace, name string) error {
	r.secretChecks++
	r.secretNamespace, r.secretName = namespace, name
	return r.secretErr
}

func newGenerationTestService(storage *generationTestStorage, runtime abstractions.Runtime) *Service {
	return NewService(storage, runtime, &config.GenerationConfig{RunnerImage: "registry.example/evalhub:v1"}, nil)
}

func generationTestJob(state api.OverallState) *api.EvaluationJobResource {
	request, _ := generation.NormalizeRequest(validRequest())
	budget, _ := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	job, err := generation.NewJob("job-1", "tenant-a", "user-a", request, "registry.example/runner:v1", budget, "", 24*time.Hour, time.Now().UTC())
	if err != nil {
		panic(err)
	}
	job.Status.State = state
	return job
}

func TestGenerationServiceAdmitPersistsAndStartsWorker(t *testing.T) {
	storage := &generationTestStorage{}
	runtime := &generationTestRuntime{}
	service := newGenerationTestService(storage, runtime)
	service.newID = func() string { return "generation-job-1" }
	service.now = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }
	job, err := service.Admit(context.Background(), "tenant-a", "user-a", validRequest())
	if err != nil {
		t.Fatalf("Admit(): %v", err)
	}
	if !storage.created || runtime.runCalls != 1 || job.Resource.ID != "generation-job-1" || job.Status.State != api.OverallStatePending {
		t.Fatalf("admission did not persist and launch: created=%v calls=%d job=%+v", storage.created, runtime.runCalls, job)
	}
	if storage.tenant != "tenant-a" || storage.owner != "user-a" || storage.workload != workloads.CollectionGeneration {
		t.Fatalf("storage scope = tenant %q owner %q workload %q", storage.tenant, storage.owner, storage.workload)
	}
	if runtime.runJob != storage.job || storage.job.Model.Name != "model" || storage.job.Model.Parameters != nil {
		t.Fatalf("runtime did not receive normalized persisted job: %+v", runtime.runJob)
	}
}

func TestGenerationServiceAdmitSecretValidationAndSafeLaunchFailure(t *testing.T) {
	request := validRequest()
	storage := &generationTestStorage{}
	runtime := &generationTestRuntime{secretErr: errors.New("secret contents must remain private")}
	service := newGenerationTestService(storage, runtime)
	_, err := service.Admit(context.Background(), "tenant-a", "user-a", request)
	var serviceErr *serviceerrors.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.MessageCode() != messages.GenerationModelSecretLookupUnavailable ||
		!errors.Is(err, runtime.secretErr) || strings.Contains(err.Error(), "secret contents") || storage.created {
		t.Fatalf("Secret validation leaked details or created job: err=%v created=%v", err, storage.created)
	}
	if runtime.secretChecks != 1 || runtime.secretNamespace != "tenant-a" || runtime.secretName != "model-auth" {
		t.Fatalf("Secret validation scope = %d %q %q", runtime.secretChecks, runtime.secretNamespace, runtime.secretName)
	}
	runtime.secretErr = serviceerrors.NewServiceError(messages.GenerationModelSecretAccessDenied)
	if _, err := service.Admit(context.Background(), "tenant-a", "user-a", request); err != runtime.secretErr || storage.created {
		t.Fatalf("admission failed to preserve the runtime's classified error: err=%v created=%v", err, storage.created)
	}
	storage = &generationTestStorage{}
	runtime = &generationTestRuntime{runErr: errors.New("unsafe runtime detail")}
	job, err := newGenerationTestService(storage, runtime).Admit(context.Background(), "tenant-a", "user-a", request)
	if err != nil {
		t.Fatalf("Admit() after launch failure: %v", err)
	}
	if job.Status.State != api.OverallStateFailed || job.Status.Message == nil || job.Status.Message.MessageCode != launchFailureCode || strings.Contains(job.Status.Message.Message, "unsafe runtime") {
		t.Fatalf("launch failure not sanitized: %+v", job.Status)
	}
	storage = &generationTestStorage{updateErr: errors.New("status write failed")}
	if _, err := newGenerationTestService(storage, runtime).Admit(context.Background(), "tenant-a", "user-a", request); err == nil {
		t.Fatal("failure to persist launch error was ignored")
	}
}

func TestGenerationServiceAdmitChecksRuntimeWithoutModelAuth(t *testing.T) {
	request := validRequest()
	request.Model.Auth = nil
	storage := &generationTestStorage{}
	service := newGenerationTestService(storage, &local.LocalRuntime{})
	_, err := service.Admit(context.Background(), "tenant-a", "user-a", request)
	if err == nil || err.Error() != "runtime does not support collection generation" || storage.created {
		t.Fatalf("local runtime admission = %v, persisted = %v", err, storage.created)
	}

	runtime := &generationTestRuntime{}
	service = newGenerationTestService(storage, runtime)
	if _, err := service.Admit(context.Background(), "tenant-a", "user-a", request); err != nil {
		t.Fatalf("supported runtime rejected an unauthenticated model endpoint: %v", err)
	}
	if runtime.secretChecks != 0 || runtime.runCalls != 1 {
		t.Fatalf("admission checked %d Secrets and launched %d jobs", runtime.secretChecks, runtime.runCalls)
	}
}

func TestGenerationServiceAdmitRejectsMissingOrInvalidDependencies(t *testing.T) {
	request := validRequest()
	var nilService *Service
	if _, err := nilService.Admit(context.Background(), "tenant", "owner", request); err == nil {
		t.Fatal("nil service admitted a request")
	}
	if _, err := NewService(nil, &generationTestRuntime{}, nil, nil).Admit(context.Background(), "tenant", "owner", request); err == nil {
		t.Fatal("service without storage admitted a request")
	}
	if _, err := NewService(&generationTestStorage{}, nil, nil, nil).Admit(context.Background(), "tenant", "owner", request); err == nil {
		t.Fatal("service without runtime admitted a request")
	}
	for _, tc := range []struct {
		name    string
		service *Service
		tenant  api.Tenant
		owner   api.User
		request api.CollectionGenerationRequest
	}{
		{name: "missing tenant", service: newGenerationTestService(&generationTestStorage{}, &generationTestRuntime{}), owner: "owner", request: request},
		{name: "missing owner", service: newGenerationTestService(&generationTestStorage{}, &generationTestRuntime{}), tenant: "tenant", request: request},
		{name: "invalid config", service: NewService(&generationTestStorage{}, &generationTestRuntime{}, &config.GenerationConfig{RunnerImage: "image", Retention: -time.Second}, nil), tenant: "tenant", owner: "owner", request: request},
		{name: "invalid request", service: newGenerationTestService(&generationTestStorage{}, &generationTestRuntime{}), tenant: "tenant", owner: "owner"},
		{name: "missing image", service: NewService(&generationTestStorage{}, &generationTestRuntime{}, &config.GenerationConfig{}, nil), tenant: "tenant", owner: "owner", request: request},
		{name: "storage create error", service: newGenerationTestService(&generationTestStorage{createErr: errors.New("db down")}, &generationTestRuntime{}), tenant: "tenant", owner: "owner", request: request},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.service.Admit(context.Background(), tc.tenant, tc.owner, tc.request); err == nil {
				t.Fatal("Admit() succeeded")
			}
		})
	}
}

func TestGenerationServiceGetScopesAndPropagatesStorageResult(t *testing.T) {
	storage := &generationTestStorage{job: generationTestJob(api.OverallStateRunning)}
	service := newGenerationTestService(storage, &generationTestRuntime{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := service.Get(ctx, "tenant-a", "user-a", "job-1")
	if err != nil || got.Resource.ID != "job-1" {
		t.Fatalf("Get() = (%+v, %v)", got, err)
	}
	if storage.requestContext.Err() == nil {
		t.Fatal("Get() detached the caller's cancellation context")
	}
	if storage.tenant != "tenant-a" || storage.owner != "user-a" || storage.workload != workloads.CollectionGeneration {
		t.Fatalf("Get() scope = %q %q %q", storage.tenant, storage.owner, storage.workload)
	}
	if _, err := NewService(nil, nil, nil, nil).Get(context.Background(), "tenant", "owner", "job"); err == nil {
		t.Fatal("unconfigured service Get() succeeded")
	}
	wantErr := errors.New("read failed")
	if _, err := newGenerationTestService(&generationTestStorage{getErr: wantErr}, &generationTestRuntime{}).Get(context.Background(), "tenant", "owner", "job"); !errors.Is(err, wantErr) {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestGenerationServiceDeleteCancelsCleansAndDeletes(t *testing.T) {
	storage := &generationTestStorage{job: generationTestJob(api.OverallStateRunning)}
	runtime := &generationTestRuntime{}
	if err := newGenerationTestService(storage, runtime).Delete(context.Background(), "tenant-a", "user-a", "job-1"); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if storage.job.Status.State != api.OverallStateCancelled || storage.protectCalls != 1 || !storage.protected || runtime.deleteCalls != 1 || !storage.deleted {
		t.Fatalf("delete lifecycle: state=%s protect_calls=%d protected=%v runtime=%d deleted=%v", storage.job.Status.State, storage.protectCalls, storage.protected, runtime.deleteCalls, storage.deleted)
	}
	if !generation.IsJob(&runtime.deleteJob.EvaluationJobConfig) {
		t.Fatal("runtime cleanup received the wrong workload")
	}
	if _, ok := runtime.ctx.Deadline(); !ok {
		t.Fatal("runtime cleanup did not receive a bounded context")
	}

	// If a terminal worker callback wins the cancellation race, preserve it and
	// continue runtime cleanup and record deletion.
	storage = &generationTestStorage{job: generationTestJob(api.OverallStateRunning), terminalOnProtect: true}
	runtime = &generationTestRuntime{}
	if err := newGenerationTestService(storage, runtime).Delete(context.Background(), "tenant-a", "user-a", "job-1"); err != nil {
		t.Fatalf("Delete() after terminal callback race: %v", err)
	}
	if storage.job.Status.State != api.OverallStateCompleted || runtime.deleteJob == nil || runtime.deleteJob.Status.State != api.OverallStateCompleted || !storage.deleted {
		t.Fatalf("terminal race did not use reread worker state for cleanup: stored=%s cleaned=%v deleted=%v", storage.job.Status.State, runtime.deleteJob, storage.deleted)
	}

	storage = &generationTestStorage{job: generationTestJob(api.OverallStateCompleted)}
	runtime = &generationTestRuntime{deleteErr: errors.New("cleanup failed")}
	if err := newGenerationTestService(storage, runtime).Delete(context.Background(), "tenant-a", "user-a", "job-1"); err == nil || storage.deleted || !storage.protected || storage.protectCalls != 1 {
		t.Fatal("runtime cleanup failure did not preserve the terminal record")
	}
	storage = &generationTestStorage{job: generationTestJob(api.OverallStateCompleted), protectErr: errors.New("could not protect expiry")}
	runtime = &generationTestRuntime{}
	if err := newGenerationTestService(storage, runtime).Delete(context.Background(), "tenant-a", "user-a", "job-1"); err == nil || !strings.Contains(err.Error(), "could not protect expiry") || storage.deleted || runtime.deleteCalls != 0 {
		t.Fatalf("failure to protect cleanup record was not reported before runtime cleanup: err=%v deleted=%v cleanup_calls=%d", err, storage.deleted, runtime.deleteCalls)
	}
	storage = &generationTestStorage{job: generationTestJob(api.OverallStateCompleted), deleteErr: errors.New("delete failed")}
	if err := newGenerationTestService(storage, &generationTestRuntime{}).Delete(context.Background(), "tenant-a", "user-a", "job-1"); err == nil {
		t.Fatal("record deletion failure was ignored")
	}
}

func TestGenerationServiceDeleteRejectsInvalidJobAndHandlesCancellationFailure(t *testing.T) {
	if err := NewService(nil, nil, nil, nil).Delete(context.Background(), "tenant", "owner", "job"); err == nil {
		t.Fatal("unconfigured service Delete() succeeded")
	}
	for _, job := range []*api.EvaluationJobResource{
		{Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning}}},
		{EvaluationJobConfig: api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{ProviderID: "ordinary", Ref: api.Ref{ID: "benchmark"}}}}, Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning}}},
		{EvaluationJobConfig: api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{ProviderID: generation.ProviderID, Ref: api.Ref{ID: generation.BenchmarkID}}}}},
	} {
		job.Resource.ID = "job-1"
		storage := &generationTestStorage{job: job}
		if err := newGenerationTestService(storage, &generationTestRuntime{}).Delete(context.Background(), "tenant", "owner", "job-1"); err == nil {
			t.Errorf("Delete() accepted invalid job %+v", job)
		}
	}
	storage := &generationTestStorage{job: generationTestJob(api.OverallStateRunning), protectErr: errors.New("atomic protection failed")}
	runtime := &generationTestRuntime{}
	if err := newGenerationTestService(storage, runtime).Delete(context.Background(), "tenant", "owner", "job-1"); err == nil || storage.deleted || runtime.deleteCalls != 0 {
		t.Fatal("failed atomic cancellation/protection was ignored")
	}
	storage = &generationTestStorage{getErr: errors.New("not found")}
	if err := newGenerationTestService(storage, &generationTestRuntime{}).Delete(context.Background(), "tenant", "owner", "job-1"); err == nil {
		t.Fatal("Get error during Delete() was ignored")
	}
}
