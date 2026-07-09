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
