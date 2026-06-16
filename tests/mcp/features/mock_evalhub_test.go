package features

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

var mockProviders = []api.ProviderResource{
	{
		Resource: api.Resource{ID: "lighteval"},
		ProviderConfig: api.ProviderConfig{
			Name:        "lighteval",
			Title:       "LightEval",
			Description: "Lightweight evaluation framework",
			Benchmarks: []api.BenchmarkResource{
				{ID: "hellaswag", Name: "HellaSwag", Category: "reasoning", Tags: []string{"reasoning", "general"}},
				{ID: "mmlu", Name: "MMLU", Category: "knowledge", Tags: []string{"knowledge", "general"}},
				{ID: "toxigen", Name: "ToxiGen", Category: "safety", Tags: []string{"safety"}},
			},
		},
	},
	{
		Resource: api.Resource{ID: "unitxt"},
		ProviderConfig: api.ProviderConfig{
			Name:        "unitxt",
			Title:       "Unitxt",
			Description: "Flexible text evaluation",
			Benchmarks: []api.BenchmarkResource{
				{ID: "rag_eval", Name: "RAG Evaluation", Category: "rag", Tags: []string{"rag", "safety"}},
			},
		},
	},
}

var mockCollections = []api.CollectionResource{
	{
		Resource: api.Resource{ID: "safety-suite"},
		CollectionConfig: api.CollectionConfig{
			Name:     "Safety Suite",
			Category: "safety",
			Tags:     []string{"safety", "production"},
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "toxigen"}, ProviderID: "lighteval"},
			},
		},
	},
	{
		Resource: api.Resource{ID: "general-eval"},
		CollectionConfig: api.CollectionConfig{
			Name:     "General Evaluation",
			Category: "general",
			Tags:     []string{"general"},
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "hellaswag"}, ProviderID: "lighteval"},
				{Ref: api.Ref{ID: "mmlu"}, ProviderID: "unitxt"},
			},
		},
	},
}

var mockJobs = []api.EvaluationJobResource{
	{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", CreatedAt: time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)}},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
			Benchmarks: []api.BenchmarkStatus{
				{ID: "hellaswag", ProviderID: "lighteval", Status: api.StateRunning, StartedAt: "2026-04-30T10:00:00Z"},
			},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Name:  "test-eval-1",
			Model: api.ModelRef{URL: "http://model:8080", Name: "test-model"},
		},
	},
	{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-2", CreatedAt: time.Date(2026, 4, 30, 11, 0, 0, 0, time.UTC)}},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
			Benchmarks: []api.BenchmarkStatus{
				{ID: "mmlu", ProviderID: "unitxt", Status: api.StateCompleted, CompletedAt: "2026-04-30T11:00:00Z"},
			},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Name:  "test-eval-2",
			Model: api.ModelRef{URL: "http://model:8080", Name: "test-model"},
		},
	},
}

func newMockEvalHubHandler() http.Handler {
	mux := http.NewServeMux()
	basePath := "/api/v1/evaluations"

	mux.HandleFunc(basePath+"/providers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, api.ProviderResourceList{
			Items: mockProviders,
			Page:  api.Page{TotalCount: len(mockProviders)},
		})
	})

	mux.HandleFunc(basePath+"/providers/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, basePath+"/providers/")
		for _, p := range mockProviders {
			if p.Resource.ID == id {
				writeJSON(w, p)
				return
			}
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	mux.HandleFunc(basePath+"/collections", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, api.CollectionResourceList{
			Items: mockCollections,
			Page:  api.Page{TotalCount: len(mockCollections)},
		})
	})

	mux.HandleFunc(basePath+"/collections/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, basePath+"/collections/")
		for _, c := range mockCollections {
			if c.Resource.ID == id {
				writeJSON(w, c)
				return
			}
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	mux.HandleFunc(basePath+"/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var config api.EvaluationJobConfig
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
				return
			}
			job := api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{ID: "job-new", CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)},
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending},
				},
				EvaluationJobConfig: config,
			}
			writeJSONStatus(w, http.StatusAccepted, job)
			return
		}

		status := r.URL.Query().Get("status")
		if status != "" {
			var filtered []api.EvaluationJobResource
			for _, j := range mockJobs {
				if j.Status != nil && string(j.Status.State) == status {
					filtered = append(filtered, j)
				}
			}
			writeJSON(w, api.EvaluationJobResourceList{
				Items: filtered,
				Page:  api.Page{TotalCount: len(filtered)},
			})
			return
		}

		writeJSON(w, api.EvaluationJobResourceList{
			Items: mockJobs,
			Page:  api.Page{TotalCount: len(mockJobs)},
		})
	})

	mux.HandleFunc(basePath+"/jobs/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, basePath+"/jobs/")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		for _, j := range mockJobs {
			if j.Resource.ID == id {
				writeJSON(w, j)
				return
			}
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
