package mlflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func TestBuildRunArtifactPath(t *testing.T) {
	t.Parallel()

	got := BuildRunArtifactPath("8", "run-1", "evaluation-card.json", "")
	want := "8/run-1/artifacts/evaluation-card.json"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	got = BuildRunArtifactPath("8", "run-1", "evaluation-card.json", "/mlflow/artifacts/workspaces/sagar/8")
	want = "mlflow/artifacts/workspaces/sagar/8/8/run-1/artifacts/evaluation-card.json"
	if got != want {
		t.Fatalf("path with artifact location = %q, want %q", got, want)
	}
}

func TestArtifactLocationPathPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		want     string
	}{
		{name: "empty", location: "", want: ""},
		{name: "whitespace", location: "  ", want: ""},
		{name: "trim slashes", location: "/mlflow/artifacts/workspaces/sagar/8/", want: "mlflow/artifacts/workspaces/sagar/8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ArtifactLocationPathPrefix(tt.location); got != tt.want {
				t.Fatalf("prefix = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUploadArtifactToExperimentWithArtifactLocation(t *testing.T) {
	t.Parallel()

	var uploadedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/mlflow-artifacts/artifacts/") {
			uploadedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := mlflowclient.NewClient(srv.URL).WithContext(t.Context())
	_, err := UploadArtifactToExperiment(
		client,
		"8",
		"run-1",
		"evaluation-card.json",
		"/mlflow/artifacts/workspaces/sagar/8",
		[]byte(`{"card_version":"1.0"}`),
		"application/json",
	)
	if err != nil {
		t.Fatalf("UploadArtifactToExperiment() err = %v", err)
	}
	want := "/mlflow-artifacts/artifacts/mlflow/artifacts/workspaces/sagar/8/8/run-1/artifacts/evaluation-card.json"
	if !strings.HasSuffix(uploadedPath, want) {
		t.Fatalf("uploaded path = %q, want suffix %q", uploadedPath, want)
	}
}

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
	url, err := PersistEvalCard(client, "exp-1", "job-1", "demo-job", "", []byte(`{"card_version":"1.0"}`))
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

func TestPersistEvalCardWithArtifactLocation(t *testing.T) {
	t.Parallel()

	var uploadedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create"):
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
	artifactLocation := "/mlflow/artifacts/workspaces/sagar/8"
	_, err := PersistEvalCard(client, "8", "job-1", "demo-job", artifactLocation, []byte(`{"card_version":"1.0"}`))
	if err != nil {
		t.Fatalf("PersistEvalCard() err = %v", err)
	}
	want := "/mlflow-artifacts/artifacts/mlflow/artifacts/workspaces/sagar/8/8/run-1/artifacts/evaluation-card.json"
	if !strings.HasSuffix(uploadedPath, want) {
		t.Fatalf("uploaded path = %q, want suffix %q", uploadedPath, want)
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

func TestUploadArtifactToExperimentValidationErrors(t *testing.T) {
	t.Parallel()

	if _, err := UploadArtifactToExperiment(nil, "8", "run-1", "file.json", "", nil, ""); err == nil {
		t.Fatal("expected error for nil client")
	}
	client := mlflowclient.NewClient("http://example.com").WithContext(t.Context())
	if _, err := UploadArtifactToExperiment(client, "", "run-1", "file.json", "", nil, ""); err == nil {
		t.Fatal("expected error for empty experiment id")
	}
	if _, err := UploadArtifactToExperiment(client, "8", "", "file.json", "", nil, ""); err == nil {
		t.Fatal("expected error for empty run id")
	}
}

func TestCreateEvaluationCardRunValidationErrors(t *testing.T) {
	t.Parallel()

	if _, err := CreateEvaluationCardRun(nil, "exp-1", "job-1", "demo"); err == nil {
		t.Fatal("expected error for nil client")
	}
	client := mlflowclient.NewClient("http://example.com").WithContext(t.Context())
	if _, err := CreateEvaluationCardRun(client, "", "job-1", "demo"); err == nil {
		t.Fatal("expected error for empty experiment id")
	}
	if _, err := CreateEvaluationCardRun(client, "exp-1", "", "demo"); err == nil {
		t.Fatal("expected error for empty job id")
	}
}

func TestCreateEvaluationCardRunMissingRunID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create") {
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := mlflowclient.NewClient(srv.URL).WithContext(t.Context())
	_, err := CreateEvaluationCardRun(client, "exp-1", "job-1", "")
	if err == nil {
		t.Fatal("expected error when create run response has no run id")
	}
}

func TestCreateEvaluationCardRunDefaultRunName(t *testing.T) {
	t.Parallel()

	var runName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create") {
			var req mlflowclient.CreateRunRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			runName = req.RunName
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{RunID: "run-1"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := mlflowclient.NewClient(srv.URL).WithContext(t.Context())
	runID, err := CreateEvaluationCardRun(client, "exp-1", "job-1", "")
	if err != nil {
		t.Fatalf("CreateEvaluationCardRun() err = %v", err)
	}
	if runID != "run-1" {
		t.Fatalf("runID = %q", runID)
	}
	if runName != "evaluation-card-job-1" {
		t.Fatalf("runName = %q", runName)
	}
}
