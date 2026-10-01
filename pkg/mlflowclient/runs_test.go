package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRun(t *testing.T) {
	t.Parallel()

	var req CreateRunRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/runs/create") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(CreateRunResponse{
			Run: Run{Info: RunInfo{RunID: "run-123", ExperimentID: req.ExperimentID, RunName: req.RunName}},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	resp, err := client.CreateRun(&CreateRunRequest{
		ExperimentID: "exp-1",
		RunName:      "demo-run",
		Tags:         []RunTag{{Key: "context", Value: "eval-hub"}},
	})
	if err != nil {
		t.Fatalf("CreateRun() err = %v", err)
	}
	if resp.Run.Info.RunID != "run-123" {
		t.Fatalf("run id = %q", resp.Run.Info.RunID)
	}
	if req.StartTime == 0 {
		t.Fatal("expected StartTime to be set automatically")
	}
}

func TestCreateRunNilRequest(t *testing.T) {
	t.Parallel()

	client := NewClient("http://example.com").WithContext(t.Context())
	if _, err := client.CreateRun(nil); err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestSearchRunsNilRequest(t *testing.T) {
	t.Parallel()

	client := NewClient("http://example.com").WithContext(t.Context())
	if _, err := client.SearchRuns(nil); err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestSearchRuns(t *testing.T) {
	t.Parallel()

	var req SearchRunsRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/runs/search") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.ExperimentIDs) != 1 || req.ExperimentIDs[0] != "exp-1" {
			t.Fatalf("experiment IDs = %v, want [exp-1]", req.ExperimentIDs)
		}
		if req.Filter != "demo-run" {
			t.Fatalf("filter = %q, want demo-run", req.Filter)
		}
		_ = json.NewEncoder(w).Encode(SearchRunsResponse{
			Runs: []Run{{Info: RunInfo{RunID: "run-123", ExperimentID: "exp-1"}}},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	resp, err := client.SearchRuns(&SearchRunsRequest{
		ExperimentIDs: []string{"exp-1"},
		Filter:        "demo-run",
	})
	if err != nil {
		t.Fatalf("SearchRuns() err = %v", err)
	}
	if resp.Runs[0].Info.RunID != "run-123" {
		t.Fatalf("run id = %q", resp.Runs[0].Info.RunID)
	}
}

func TestCreateRunErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error_code":"INVALID_PARAMETER_VALUE","message":"bad experiment"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	_, err := client.CreateRun(&CreateRunRequest{ExperimentID: "exp-1"})
	if err == nil {
		t.Fatal("expected create run error")
	}
	if apiErr, ok := err.(*APIError); !ok || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("error = %v (%T), want *APIError with status 400", err, err)
	}
}

func TestSearchRunsErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"INTERNAL_ERROR","message":"boom"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if _, err := client.SearchRuns(&SearchRunsRequest{ExperimentIDs: []string{"exp-1"}}); err == nil {
		t.Fatal("expected search runs error")
	}
}
