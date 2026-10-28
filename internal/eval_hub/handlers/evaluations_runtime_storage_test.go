package handlers

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

type runtimeStorageJobStub struct {
	noopStorage
	job *api.EvaluationJobResource
	err error
}

func (s *runtimeStorageJobStub) WithLogger(_ *slog.Logger) abstractions.Storage     { return s }
func (s *runtimeStorageJobStub) WithContext(_ context.Context) abstractions.Storage { return s }
func (s *runtimeStorageJobStub) WithTenant(_ api.Tenant) abstractions.Storage       { return s }
func (s *runtimeStorageJobStub) WithOwner(_ api.User) abstractions.Storage          { return s }

func (s *runtimeStorageJobStub) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.job != nil {
		return s.job, nil
	}
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
}

func TestRuntimeStorageGetEvaluationJob(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
	}
	h := &Handlers{storage: &runtimeStorageJobStub{job: job}, validate: validator.New(validator.WithRequiredStructEnabled())}
	rs := &runtimeStorage{
		ctx:      context.Background(),
		logger:   logger,
		handlers: h,
		tenant:   "tenant",
		owner:    "user",
		validate: h.validate,
	}

	got, err := rs.GetEvaluationJob("job-1")
	if err != nil {
		t.Fatalf("GetEvaluationJob: %v", err)
	}
	if got == nil || got.Resource.ID != "job-1" {
		t.Fatalf("unexpected job: %#v", got)
	}

	h.storage = &runtimeStorageJobStub{err: serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", "missing")}
	if _, err := rs.GetEvaluationJob("missing"); err == nil {
		t.Fatal("expected not-found error")
	}
}
