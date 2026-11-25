package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}
}

func validWorkerSpec(t *testing.T, callback, sidecarModelURL string) ([]byte, string) {
	t.Helper()
	jobID := "56ffacc2-deb2-4e10-bf9a-0b69ed6da7c0"
	request, err := generation.NormalizeRequest(api.CollectionGenerationRequest{
		EvaluationGoal: "safe tool use",
		Model:          api.ModelRef{URL: "https://model.example/v1", Name: "generator", Auth: &api.ModelAuth{SecretRef: "generator-auth"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	budget, err := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(generation.JobConfig{
		Request: request, Limits: budget.Limits, RunnerImage: "registry.example/evalhub:v1", Retention: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := jobSpec{
		JobID: jobID, ProviderID: generation.ProviderID, BenchmarkID: generation.BenchmarkID,
		BenchmarkIndex: 0, Model: &api.ModelRef{URL: sidecarModelURL, Name: "generator", Auth: &api.ModelAuth{SecretRef: "generator-auth"}},
		Parameters: map[string]any{generation.ConfigParameter: string(snapshot)}, CallbackURL: &callback,
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return data, jobID
}

func TestRunSpecReportsOnlySafeLifecycleEventsThroughLoopback(t *testing.T) {
	useTestNamespace(t)
	var events []api.StatusEvent
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Scheme != "http" || !strings.HasPrefix(request.URL.Host, "127.0.0.1:") || request.URL.Path != "/api/v1/evaluations/jobs/56ffacc2-deb2-4e10-bf9a-0b69ed6da7c0/events" {
			t.Errorf("request target = %s %s", request.Method, request.URL)
		}
		if got := request.Header.Get("X-Tenant"); got != "tenant-test" {
			t.Errorf("X-Tenant = %q, want tenant-test", got)
		}
		var event api.StatusEvent
		if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
		}
		events = append(events, event)
		return response(http.StatusNoContent), nil
	})}
	data, _ := validWorkerSpec(t, "http://127.0.0.1:8080", "http://127.0.0.1:8080/model/56ffacc2-de2b-4e10-bf9a-0b69ed6da7c0/v1")
	if err := RunSpec(context.Background(), data, client); err != nil {
		t.Fatalf("RunSpec(): %v", err)
	}
	if len(events) != 2 || events[0].BenchmarkStatusEvent.Status != api.StateRunning || events[1].BenchmarkStatusEvent.Status != api.StateFailed {
		t.Fatalf("reported lifecycle events = %+v", events)
	}
	if len(events[1].BenchmarkStatusEvent.AdditionalInfo) != 1 {
		t.Fatalf("failure event contains unexpected fields: %+v", events[1].BenchmarkStatusEvent.AdditionalInfo)
	}
	failure, ok := events[1].BenchmarkStatusEvent.AdditionalInfo["collection_generation_error"].(map[string]any)
	if !ok || failure["code"] != "workload_failed" || failure["message"] != "The collection-generation worker failed." {
		t.Fatalf("failure was not sanitized: %+v", events[1].BenchmarkStatusEvent.AdditionalInfo)
	}
}

func TestRunSpecRejectsMalformedAndUntrustedJobSpecs(t *testing.T) {
	useTestNamespace(t)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid spec reached the callback transport")
		return nil, errors.New("unexpected request")
	})}
	valid, _ := validWorkerSpec(t, "http://127.0.0.1:8080", "http://127.0.0.1:8080/model/job/v1")
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "malformed", data: []byte("{")},
		{name: "multiple documents", data: append(append([]byte(nil), valid...), []byte(` {}`)...)},
		{name: "unknown field", data: append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"unexpected":"value"}`)...)},
		{name: "oversized", data: []byte(strings.Repeat("x", maxJobSpecBytes+1))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := RunSpec(context.Background(), tc.data, client); err == nil {
				t.Fatal("RunSpec() succeeded")
			}
		})
	}

	var spec jobSpec
	if err := json.Unmarshal(valid, &spec); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		change func(*jobSpec)
		want   string
	}{
		{name: "invalid ID", change: func(s *jobSpec) { s.JobID = "not-a-uuid" }, want: "generation job ID is invalid"},
		{name: "wrong workload", change: func(s *jobSpec) { s.ProviderID = "external-provider" }, want: "job does not identify the collection-generation workload"},
		{name: "missing model", change: func(s *jobSpec) { s.Model = nil }, want: "generation model and callback are required"},
		{name: "missing callback", change: func(s *jobSpec) { s.CallbackURL = nil }, want: "generation model and callback are required"},
		{name: "missing snapshot", change: func(s *jobSpec) { s.Parameters = nil }, want: "generation snapshot is invalid"},
		{name: "external model target", change: func(s *jobSpec) { s.Model.URL = "https://model.example/v1" }, want: "job model does not match the trusted generation connection"},
		{name: "mismatched model name", change: func(s *jobSpec) { s.Model.Name = "different" }, want: "job model does not match the trusted generation connection"},
		{name: "mismatched secret ref", change: func(s *jobSpec) { s.Model.Auth.SecretRef = "other-secret" }, want: "job model Secret reference does not match the trusted generation request"},
		{name: "external callback", change: func(s *jobSpec) { value := "http://example.com:8080"; s.CallbackURL = &value }, want: "generation callback must use the local sidecar URL"},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			candidate := spec
			candidate.Model = cloneModel(spec.Model)
			candidate.Parameters = map[string]any{}
			for key, value := range spec.Parameters {
				candidate.Parameters[key] = value
			}
			candidate.CallbackURL = cloneString(spec.CallbackURL)
			tc.change(&candidate)
			data, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := RunSpec(context.Background(), data, client); err == nil || err.Error() != tc.want {
				t.Fatalf("RunSpec() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunReadsMountedSpecAndHandlesOpenFailure(t *testing.T) {
	useTestNamespace(t)
	data, _ := validWorkerSpec(t, "http://127.0.0.1:8080", "http://127.0.0.1:8080/model/56ffacc2-de2b-4e10-bf9a-0b69ed6da7c0/v1")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusNoContent), nil })}
	path := filepath.Join(t.TempDir(), "job.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	oldOpenSpec := openSpec
	openSpec = func(specPath string) (io.ReadCloser, error) { return os.Open(specPath) }
	t.Cleanup(func() { openSpec = oldOpenSpec })
	if err := Run(context.Background(), path, client); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if err := Run(context.Background(), filepath.Join(t.TempDir(), "missing.json"), client); err == nil {
		t.Fatal("Run() succeeded for missing job spec")
	}
}

func TestRunSpecRejectsEventEndpointFailure(t *testing.T) {
	useTestNamespace(t)
	data, _ := validWorkerSpec(t, "http://127.0.0.1:8080", "http://127.0.0.1:8080/model/56ffacc2-de2b-4e10-bf9a-0b69ed6da7c0/v1")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusForbidden), nil })}
	if err := RunSpec(context.Background(), data, client); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("RunSpec() error = %v, want HTTP 403", err)
	}
}

func TestRunSpecAttemptsTerminalReportWhenRunningReportFails(t *testing.T) {
	useTestNamespace(t)
	data, _ := validWorkerSpec(t, "http://127.0.0.1:8080", "http://127.0.0.1:8080/model/job/v1")
	for _, tc := range []struct {
		name           string
		terminalStatus int
	}{
		{name: "terminal report succeeds", terminalStatus: http.StatusNoContent},
		{name: "terminal report also fails", terminalStatus: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var states []api.State
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var event api.StatusEvent
				if err := json.NewDecoder(request.Body).Decode(&event); err != nil || event.BenchmarkStatusEvent == nil {
					t.Fatalf("decode benchmark status event: %v", err)
				}
				states = append(states, event.BenchmarkStatusEvent.Status)
				if len(states) == 1 {
					cancel()
					return response(http.StatusServiceUnavailable), nil
				}
				if err := request.Context().Err(); err != nil {
					t.Errorf("terminal report inherited cancellation: %v", err)
				}
				if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > workerTimeout {
					t.Error("terminal report lacks a bounded deadline")
				}
				return response(tc.terminalStatus), nil
			})}
			if err := RunSpec(ctx, data, client); err == nil || err.Error() != "report generation start: generation event endpoint returned HTTP 503" {
				t.Fatalf("RunSpec() = %v, want original running report error", err)
			}
			if len(states) != 2 || states[0] != api.StateRunning || states[1] != api.StateFailed {
				t.Fatalf("callback states = %v, want running then failed", states)
			}
		})
	}
}

func TestRunSpecReportsTerminalFailureAfterCallerCancellation(t *testing.T) {
	useTestNamespace(t)
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			cancel()
		case 2:
			if err := request.Context().Err(); err != nil {
				t.Errorf("terminal failure event context is cancelled: %v", err)
			}
			if _, ok := request.Context().Deadline(); !ok {
				t.Error("terminal failure event context has no bounded deadline")
			}
		}
		return response(http.StatusNoContent), nil
	})}
	data, _ := validWorkerSpec(t, "http://127.0.0.1:8080", "http://127.0.0.1:8080/model/job/v1")
	if err := RunSpec(ctx, data, client); err != nil {
		t.Fatalf("RunSpec() after cancellation: %v", err)
	}
	if calls != 2 {
		t.Fatalf("event calls = %d, want running and terminal failure", calls)
	}
}

func TestRunRejectsUnreadableOrOversizedMountedSpecs(t *testing.T) {
	if err := Run(context.Background(), "/different/job.json", nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unexpected path error = %v", err)
	}
	oldOpenSpec := openSpec
	t.Cleanup(func() { openSpec = oldOpenSpec })
	for _, tc := range []struct {
		name string
		body io.ReadCloser
		want string
	}{
		{"read error", io.NopCloser(iotest.ErrReader(errors.New("fixture read error"))), "read generation job specification"},
		{"close error", failingCloseBody{strings.NewReader("")}, "close generation job specification"},
		{"size limit", io.NopCloser(strings.NewReader(strings.Repeat("x", maxJobSpecBytes+1))), "exceeds the size limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			openSpec = func(path string) (io.ReadCloser, error) {
				if path != DefaultJobSpecPath {
					t.Errorf("default path = %q", path)
				}
				return tc.body, nil
			}
			if err := Run(context.Background(), "", nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run() error = %v, want %q", err, tc.want)
			}
		})
	}
}

type failingCloseBody struct{ io.Reader }

func (failingCloseBody) Close() error { return errors.New("fixture close error") }

func TestRunSpecDefaultClientDoesNotFollowRedirects(t *testing.T) {
	useTestNamespace(t)
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			t.Error("worker followed the sidecar redirect")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer sidecar.Close()
	spec, _ := validWorkerSpec(t, sidecar.URL, sidecar.URL+"/model/job/v1")
	if err := RunSpec(context.Background(), spec, nil); err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("RunSpec() error = %v, want an unfollowed redirect", err)
	}
}

func TestRunSpecRequiresPodNamespace(t *testing.T) {
	useTestNamespace(t)
	readPodNamespace = func() (string, error) { return "", errors.New("missing service account projection") }
	spec, _ := validWorkerSpec(t, "http://localhost:8080", "http://localhost:8080/model/job/v1")
	if err := RunSpec(context.Background(), spec, nil); err == nil || err.Error() != "read generation pod namespace" {
		t.Fatalf("RunSpec() error = %v", err)
	}
}

func TestPostEventBoundsPayloadAndSanitizesIOFailures(t *testing.T) {
	endpoint, err := url.Parse("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		info map[string]any
		body io.ReadCloser
		err  error
		want string
	}{
		{name: "too large", info: map[string]any{"data": strings.Repeat("x", maxEventBytes)}, want: "generation event is invalid"},
		{name: "transport error", err: errors.New("sensitive upstream message"), want: "send generation event through the local sidecar"},
		{name: "response read error", body: io.NopCloser(iotest.ErrReader(errors.New("sensitive upstream message"))), want: "read generation event response"},
		{name: "response close error", body: failingCloseBody{strings.NewReader("")}, want: "read generation event response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				if tc.body == nil {
					t.Fatal("invalid event was sent")
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: tc.body}, nil
			})}
			event := api.BenchmarkStatusEvent{AdditionalInfo: tc.info}
			if err := postEvent(context.Background(), client, endpoint, "tenant", event); err == nil || err.Error() != tc.want {
				t.Fatalf("postEvent() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func useTestNamespace(t *testing.T) {
	t.Helper()
	old := readPodNamespace
	readPodNamespace = func() (string, error) { return "tenant-test", nil }
	t.Cleanup(func() { readPodNamespace = old })
}

func TestRunSpecValidatesCallbackAndLoopbackURL(t *testing.T) {
	for _, raw := range []string{"", "https://localhost:8080", "http://localhost", "http://user@localhost:8080", "http://localhost:8080/path", "http://example.com:8080"} {
		if _, err := parseCallbackURL(raw, "56ffacc2-de2b-4e10-bf9a-0b69ed6da7c0"); err == nil {
			t.Errorf("parseCallbackURL(%q) succeeded", raw)
		}
	}
	for _, raw := range []string{"https://localhost:8080", "http://model.example:80", "http://user@localhost:8080", "http://localhost:8080#fragment", "http://localhost/model"} {
		if isLoopbackURL(raw) {
			t.Errorf("isLoopbackURL(%q) = true", raw)
		}
	}
	if !isLoopbackURL("http://127.0.0.1:8080/model") {
		t.Fatal("valid loopback URL was rejected")
	}
}

func cloneModel(model *api.ModelRef) *api.ModelRef {
	if model == nil {
		return nil
	}
	copy := *model
	if model.Auth != nil {
		auth := *model.Auth
		copy.Auth = &auth
	}
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
