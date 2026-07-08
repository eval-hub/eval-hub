package mlflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func TestPersistEvalCard(t *testing.T) {
	t.Parallel()

	var uploadedPath string
	createCalled := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create"):
			createCalled++
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{RunID: "run-1"}},
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/mlflow-artifacts/artifacts/"):
			uploadedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := mlflowclient.NewClient(srv.URL).WithContext(t.Context())
	url, err := PersistEvalCard(client, "exp-1", "job-1", "demo-job", []byte(`{"card_version":"1.0"}`))
	if err != nil {
		t.Fatalf("PersistEvalCard() err = %v", err)
	}
	if createCalled != 1 {
		t.Fatalf("create run calls = %d, want 1", createCalled)
	}
	if !strings.Contains(uploadedPath, "exp-1/run-1/artifacts/evaluation-card.json") {
		t.Fatalf("uploaded path = %q", uploadedPath)
	}
	if !strings.Contains(url, "exp-1/run-1/artifacts/evaluation-card.json") {
		t.Fatalf("artifact url = %q", url)
	}
}

func TestCreateEvaluationCardRunAlwaysCreates(t *testing.T) {
	t.Parallel()

	createCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create"):
			createCalled = true
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{RunID: "new-run"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := mlflowclient.NewClient(srv.URL).WithContext(t.Context())
	runID, err := CreateEvaluationCardRun(client, "exp-1", "job-1", "demo-job")
	if err != nil {
		t.Fatalf("CreateEvaluationCardRun() err = %v", err)
	}
	if runID != "new-run" {
		t.Fatalf("runID = %q", runID)
	}
	if !createCalled {
		t.Fatal("expected create run to be called")
	}
}
