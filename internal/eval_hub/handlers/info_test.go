package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type infoQueueRuntime struct {
	abstractions.Runtime
	queues []api.QueueInfo
	err    error
	tenant string
}

func (r *infoQueueRuntime) ListQueues(_ context.Context, namespace string) ([]api.QueueInfo, error) {
	r.tenant = namespace
	return r.queues, r.err
}

func TestHandleGetInfo(t *testing.T) {
	t.Run("returns metadata and tenant queues", func(t *testing.T) {
		runtime := &infoQueueRuntime{queues: []api.QueueInfo{{Name: "gpu", Active: true}}}
		h := handlers.New(nil, nil, runtime, nil, nil, &config.Config{Service: &config.ServiceConfig{
			Version: "1.2.3", Build: "release", BuildDate: "2026-10-02", GitHash: "abc123",
		}}, nil)
		recorder := httptest.NewRecorder()
		ctx := &executioncontext.ExecutionContext{Ctx: context.Background(), Tenant: api.Tenant("tenant-a")}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		var got api.InfoResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.Version != "1.2.3" || got.Build != "release" || got.BuildDate != "2026-10-02" || got.GitHash != "abc123" {
			t.Fatalf("metadata = %#v", got)
		}
		if runtime.tenant != "tenant-a" {
			t.Fatalf("queue namespace = %q, want tenant-a", runtime.tenant)
		}
		if len(got.Queues) != 1 || got.Queues[0].Name != "gpu" || !got.Queues[0].Active {
			t.Fatalf("queues = %#v", got.Queues)
		}
	})

	t.Run("runtimes without queue discovery return an empty array", func(t *testing.T) {
		h := handlers.New(nil, nil, nil, nil, nil, &config.Config{Service: &config.ServiceConfig{}}, nil)
		recorder := httptest.NewRecorder()
		ctx := &executioncontext.ExecutionContext{Ctx: context.Background()}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		var got map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if string(got["queues"]) != "[]" {
			t.Fatalf("queues = %s, want []", got["queues"])
		}
	})

	t.Run("unexpected queue lookup errors are logged and redacted", func(t *testing.T) {
		const rawError = "queue lookup denied"
		runtime := &infoQueueRuntime{err: errors.New(rawError)}
		h := handlers.New(nil, nil, runtime, nil, nil, &config.Config{Service: &config.ServiceConfig{}}, nil)
		recorder := httptest.NewRecorder()
		var logs bytes.Buffer
		ctx := &executioncontext.ExecutionContext{
			Ctx:       context.Background(),
			RequestID: "request-1",
			Logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
			Tenant:    api.Tenant("tenant-a"),
		}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		if recorder.Code != 500 {
			t.Fatalf("status = %d, want 500: %s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), rawError) {
			t.Fatalf("response exposed internal error: %s", recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "An internal server error occurred") {
			t.Fatalf("response does not contain generic error: %s", recorder.Body.String())
		}
		if !strings.Contains(logs.String(), rawError) {
			t.Fatalf("original error was not logged: %s", logs.String())
		}
	})

	t.Run("service errors retain their client message", func(t *testing.T) {
		runtime := &infoQueueRuntime{err: serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "queue", "ResourceId", "gpu")}
		h := handlers.New(nil, nil, runtime, nil, nil, &config.Config{Service: &config.ServiceConfig{}}, nil)
		recorder := httptest.NewRecorder()
		ctx := &executioncontext.ExecutionContext{Ctx: context.Background(), RequestID: "request-1", Tenant: api.Tenant("tenant-a")}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		if recorder.Code != 404 {
			t.Fatalf("status = %d, want 404: %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), `"message_code":"resource_not_found"`) {
			t.Fatalf("service error response not preserved: %s", recorder.Body.String())
		}
	})
}
