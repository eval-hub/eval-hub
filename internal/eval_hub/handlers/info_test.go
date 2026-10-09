package handlers_test

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

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type infoHardwareProfileRuntime struct {
	abstractions.Runtime
	profiles []api.HardwareProfileInfo
	err      error
	tenant   string
}

func (r *infoHardwareProfileRuntime) ListHardwareProfiles(_ context.Context, namespace string) ([]api.HardwareProfileInfo, error) {
	r.tenant = namespace
	return r.profiles, r.err
}

func TestHandleGetInfo(t *testing.T) {
	t.Run("returns metadata and tenant profiles", func(t *testing.T) {
		runtime := &infoHardwareProfileRuntime{profiles: []api.HardwareProfileInfo{
			{Name: "gpu", QueueName: "gpu-queue", PriorityClassName: "high-priority", DisplayName: "GPU", Description: "GPU profile",
				SchedulingType: api.HardwareProfileSchedulingQueue, Identifiers: []api.HardwareProfileIdentifier{{Identifier: "nvidia.com/gpu", ResourceType: api.HardwareProfileResourceAccelerator, MinCount: "0", DefaultCount: "1", MaxCount: "4"}},
				QueueAvailability: &api.QueueAvailability{Status: api.QueueAvailabilityInactive, Reason: "Paused", Message: "queue paused"}},
			{Name: "cpu", SchedulingType: api.HardwareProfileSchedulingNode, Identifiers: []api.HardwareProfileIdentifier{}},
		}}
		h := handlers.New(nil, nil, runtime, nil, nil, &config.Config{Service: &config.ServiceConfig{
			Version: "1.2.3", Build: "release", BuildDate: "2026-10-02", GitHash: "abc123",
		}}, nil)
		recorder := httptest.NewRecorder()
		ctx := &executioncontext.ExecutionContext{Ctx: context.Background(), Tenant: api.Tenant("tenant-a")}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", contentType)
		}
		var got api.InfoResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.Version != "1.2.3" || got.Build != "release" || got.BuildDate != "2026-10-02" || got.GitHash != "abc123" {
			t.Fatalf("metadata = %#v", got)
		}
		if runtime.tenant != "tenant-a" {
			t.Fatalf("profile namespace = %q, want tenant-a", runtime.tenant)
		}
		if len(got.HardwareProfiles) != len(runtime.profiles) {
			t.Fatalf("profiles = %#v", got.HardwareProfiles)
		}
		for i, want := range runtime.profiles {
			if !reflect.DeepEqual(got.HardwareProfiles[i], want) {
				t.Errorf("profiles[%d] = %#v, want %#v", i, got.HardwareProfiles[i], want)
			}
		}
		if strings.Contains(recorder.Body.String(), `"queues"`) {
			t.Fatal("response still includes queues")
		}
		var wire struct {
			HardwareProfiles []map[string]json.RawMessage `json:"hardware_profiles"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &wire); err != nil {
			t.Fatalf("decode profile fields: %v", err)
		}
		if string(wire.HardwareProfiles[0]["queue_name"]) != `"gpu-queue"` {
			t.Fatal("queue-backed profile does not include queue_name")
		}
		if !strings.Contains(string(wire.HardwareProfiles[0]["queue_availability"]), `"status":"inactive"`) {
			t.Fatal("queue availability must include enum status")
		}
		var availability map[string]json.RawMessage
		if err := json.Unmarshal(wire.HardwareProfiles[0]["queue_availability"], &availability); err != nil {
			t.Fatalf("decode queue availability: %v", err)
		}
		if _, ok := availability["active"]; ok {
			t.Fatal("queue availability must omit active")
		}
		if string(wire.HardwareProfiles[1]["identifiers"]) != "[]" {
			t.Fatal("profile without identifiers must return an empty array")
		}
		if _, ok := wire.HardwareProfiles[1]["queue_availability"]; ok {
			t.Fatal("node profile should omit queue availability")
		}
		if string(wire.HardwareProfiles[0]["priority_class_name"]) != `"high-priority"` {
			t.Fatal("queue-backed profile must include configured priority class")
		}
		if _, ok := wire.HardwareProfiles[1]["priority_class_name"]; ok {
			t.Fatal("profile without priority class must omit priority_class_name")
		}

		if _, ok := wire.HardwareProfiles[1]["queue_name"]; ok {
			t.Fatal("node profile should omit queue_name")
		}
		for i, profile := range wire.HardwareProfiles {
			if _, ok := profile["name"]; !ok {
				t.Errorf("profiles[%d] does not include name", i)
			}
		}
	})

	t.Run("runtimes without profile discovery return an empty array", func(t *testing.T) {
		h := handlers.New(nil, nil, nil, nil, nil, &config.Config{Service: &config.ServiceConfig{}}, nil)
		recorder := httptest.NewRecorder()
		ctx := &executioncontext.ExecutionContext{Ctx: context.Background()}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		var got map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if string(got["hardware_profiles"]) != "[]" {
			t.Fatalf("profiles = %s, want []", got["hardware_profiles"])
		}
	})

	t.Run("profile lister returning nil profiles produces an empty array", func(t *testing.T) {
		runtime := &infoHardwareProfileRuntime{}
		h := handlers.New(nil, nil, runtime, nil, nil, &config.Config{Service: &config.ServiceConfig{}}, nil)
		recorder := httptest.NewRecorder()
		ctx := &executioncontext.ExecutionContext{Ctx: context.Background(), Tenant: api.Tenant("tenant-a")}
		h.HandleGetInfo(ctx, createMockRequest("GET", "/api/v1/info"), MockResponseWrapper{recorder: recorder})

		var got map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if string(got["hardware_profiles"]) != "[]" {
			t.Fatalf("profiles = %s, want []", got["hardware_profiles"])
		}
	})

	t.Run("unexpected profile lookup errors are logged and redacted", func(t *testing.T) {
		const rawError = "profile lookup denied"
		runtime := &infoHardwareProfileRuntime{err: errors.New(rawError)}
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
		runtime := &infoHardwareProfileRuntime{err: serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "profile", "ResourceId", "gpu")}
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
