package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type collectionRunCountStorage struct {
	*fakeStorage
	collection      *api.CollectionResource
	createdJob      *api.EvaluationJobResource
	atomicCalls     int
	atomicErr       error
	updatedStatusID string
	updatedStatus   *api.CollectionStatus
}

func (s *collectionRunCountStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *collectionRunCountStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *collectionRunCountStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }
func (s *collectionRunCountStorage) WithOwner(_ api.User) abstractions.Storage    { return s }

func (s *collectionRunCountStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if s.collection != nil && s.collection.Resource.ID == id {
		return s.collection, nil
	}
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
}

func (s *collectionRunCountStorage) CreateEvaluationJob(job *api.EvaluationJobResource) error {
	s.createdJob = job
	return nil
}

func (s *collectionRunCountStorage) CreateEvaluationJobAndUpdateCollection(job *api.EvaluationJobResource) error {
	s.atomicCalls++
	if s.atomicErr != nil {
		return s.atomicErr
	}
	s.createdJob = job
	if s.collection.Status != nil {
		s.collection.Status.RunCount++
		s.updatedStatusID = job.Collection.ID
		s.updatedStatus = s.collection.Status
	}
	return nil
}

func (s *collectionRunCountStorage) UpdateCollectionStatus(id string, status *api.CollectionStatus) (*api.CollectionResource, error) {
	s.updatedStatusID = id
	s.updatedStatus = status
	return s.collection, nil
}

func TestHandleListEvaluationsCollectionIDFilter(t *testing.T) {
	storage := &listEvaluationsStorage{fakeStorage: &fakeStorage{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, nil)
	req := &listEvaluationsRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/jobs?collection_id=collection-1"),
		queryValues: map[string][]string{"collection_id": {"collection-1"}},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	h.HandleListEvaluations(ctx, req, MockResponseWrapper{recorder: recorder})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if got := storage.lastFilter.Params["collection_id"]; got != "collection-1" {
		t.Errorf("collection_id filter = %v, want collection-1", got)
	}
}

func TestHandleCreateEvaluationUpdatesCustomCollectionRunCount(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name         string
		status       *api.CollectionStatus
		atomicErr    error
		wantCode     int
		wantUpdate   bool
		wantRunCount int
	}{
		{name: "custom collection", status: &api.CollectionStatus{RunCount: 2}, wantCode: http.StatusAccepted, wantUpdate: true, wantRunCount: 3},
		{name: "system collection", wantCode: http.StatusAccepted},
		{name: "atomic storage failure", status: &api.CollectionStatus{RunCount: 2}, atomicErr: errors.New("storage failed"), wantCode: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &collectionRunCountStorage{
				fakeStorage: &fakeStorage{providerConfigs: map[string]api.ProviderResource{
					"provider-1": {
						Resource: api.Resource{ID: "provider-1"},
						ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{
							ID: "benchmark-1",
						}}},
					},
				}},
				collection: &api.CollectionResource{
					Resource: api.Resource{ID: "collection-1", UpdatedAt: now},
					CollectionConfig: api.CollectionConfig{
						Name:     "collection",
						Category: "test",
						Benchmarks: []api.CollectionBenchmarkConfig{{
							Ref:        api.Ref{ID: "benchmark-1"},
							ProviderID: "provider-1",
						}},
					},
					Status: tt.status,
				},
				atomicErr: tt.atomicErr,
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, nil)
			req := &bodyRequest{
				MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
				body:        []byte(`{"name":"evaluation","model":{"url":"http://test.com","name":"test"},"collection":{"id":"collection-1"}}`),
			}
			recorder := httptest.NewRecorder()
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

			h.HandleCreateEvaluation(ctx, req, MockResponseWrapper{recorder: recorder})

			if recorder.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d body %s", tt.wantCode, recorder.Code, recorder.Body.String())
			}
			if tt.atomicErr != nil {
				if storage.createdJob != nil {
					t.Error("job was persisted after atomic storage failure")
				}
				return
			}
			if storage.createdJob == nil {
				t.Fatal("expected evaluation job to be persisted")
			}
			if storage.atomicCalls != 1 {
				t.Errorf("atomic create calls = %d, want 1", storage.atomicCalls)
			}
			if storage.createdJob.Collection == nil || storage.createdJob.Collection.CollectionUpdatedAt == nil || !storage.createdJob.Collection.CollectionUpdatedAt.Equal(now) {
				t.Errorf("persisted job collection_updated_at = %v, want %v", storage.createdJob.Collection, now)
			}
			if tt.wantUpdate {
				if storage.updatedStatusID != "collection-1" {
					t.Errorf("updated collection ID = %q, want collection-1", storage.updatedStatusID)
				}
				if storage.updatedStatus == nil || storage.updatedStatus.RunCount != tt.wantRunCount {
					t.Errorf("updated run count = %v, want %d", storage.updatedStatus, tt.wantRunCount)
				}
			} else if storage.updatedStatus != nil {
				t.Errorf("system collection status was updated: %+v", storage.updatedStatus)
			}
		})
	}
}
