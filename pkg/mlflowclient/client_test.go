package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MLflow REST wire paths issued by mlflow-go, used to route the httptest servers.
const (
	endpointExperimentsCreate     = "/api/2.0/mlflow/experiments/create"
	endpointExperimentsGetBase    = "/api/2.0/mlflow/experiments/get"
	endpointExperimentsDeleteBase = "/api/2.0/mlflow/experiments/delete"
)

func TestCreateExperiment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointExperimentsCreate {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(CreateExperimentResponse{ExperimentID: "1"})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	resp, err := client.CreateExperiment(&CreateExperimentRequest{Name: "demo"})
	if err != nil {
		t.Fatalf("CreateExperiment() = %v", err)
	}
	if resp.ExperimentID != "1" {
		t.Fatalf("ExperimentID = %q", resp.ExperimentID)
	}
}

func TestGetExperiment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointExperimentsGetBase {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(GetExperimentResponse{
			Experiment: Experiment{ExperimentID: "exp-9", Name: "demo"},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	resp, err := client.GetExperiment("exp-9")
	if err != nil {
		t.Fatalf("GetExperiment() = %v", err)
	}
	if resp.Experiment.ExperimentID != "exp-9" {
		t.Fatalf("ExperimentID = %q", resp.Experiment.ExperimentID)
	}
}

func TestDeleteExperiment(t *testing.T) {
	t.Parallel()
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == endpointExperimentsDeleteBase {
			deleted = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if err := client.DeleteExperiment("exp-del"); err != nil {
		t.Fatalf("DeleteExperiment() = %v", err)
	}
	if !deleted {
		t.Fatal("expected delete endpoint to be called")
	}
}
