package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetOrCreateExperiment(t *testing.T) {
	t.Parallel()

	t.Run("returns existing active experiment", func(t *testing.T) {
		t.Parallel()
		var createCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case endpointExperimentsGetByNameBase:
				_ = json.NewEncoder(w).Encode(GetExperimentResponse{
					Experiment: Experiment{
						ExperimentID:   "exp-1",
						Name:           "demo",
						LifecycleStage: "active",
					},
				})
			case endpointExperimentsCreate:
				createCalls++
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithContext(t.Context())
		resp, err := client.GetOrCreateExperiment(&CreateExperimentRequest{Name: "demo"})
		if err != nil {
			t.Fatalf("GetOrCreateExperiment() = %v", err)
		}
		if resp.Experiment.ExperimentID != "exp-1" {
			t.Fatalf("ExperimentID = %q, want exp-1", resp.Experiment.ExperimentID)
		}
		if createCalls != 0 {
			t.Fatalf("createCalls = %d, want 0", createCalls)
		}
	})

	t.Run("creates experiment when missing", func(t *testing.T) {
		t.Parallel()
		var getCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case endpointExperimentsGetByNameBase:
				getCalls++
				if getCalls == 1 {
					http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`, http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(GetExperimentResponse{
					Experiment: Experiment{
						ExperimentID:   "new-exp",
						Name:           "demo",
						LifecycleStage: "active",
					},
				})
			case endpointExperimentsCreate:
				_ = json.NewEncoder(w).Encode(CreateExperimentResponse{ExperimentID: "new-exp"})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithContext(t.Context())
		resp, err := client.GetOrCreateExperiment(&CreateExperimentRequest{Name: "demo"})
		if err != nil {
			t.Fatalf("GetOrCreateExperiment() = %v", err)
		}
		if resp.Experiment.ExperimentID != "new-exp" {
			t.Fatalf("ExperimentID = %q, want new-exp", resp.Experiment.ExperimentID)
		}
	})

	t.Run("create races with RESOURCE_ALREADY_EXISTS", func(t *testing.T) {
		t.Parallel()
		var getCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case endpointExperimentsGetByNameBase:
				getCalls++
				if getCalls == 1 {
					http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`, http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(GetExperimentResponse{
					Experiment: Experiment{
						ExperimentID:   "race-exp",
						Name:           "demo",
						LifecycleStage: "active",
					},
				})
			case endpointExperimentsCreate:
				http.Error(w, `{"error_code":"RESOURCE_ALREADY_EXISTS"}`, http.StatusBadRequest)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithContext(t.Context())
		resp, err := client.GetOrCreateExperiment(&CreateExperimentRequest{Name: "demo"})
		if err != nil {
			t.Fatalf("GetOrCreateExperiment() = %v", err)
		}
		if resp.Experiment.ExperimentID != "race-exp" {
			t.Fatalf("ExperimentID = %q, want race-exp", resp.Experiment.ExperimentID)
		}
		if getCalls != 2 {
			t.Fatalf("getCalls = %d, want 2", getCalls)
		}
	})
}
