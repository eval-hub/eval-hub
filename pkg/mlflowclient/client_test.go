package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAuthToken(t *testing.T) {
	t.Parallel()

	t.Run("static token", func(t *testing.T) {
		t.Parallel()
		c := NewClient("http://example").WithToken("secret")
		if got := c.resolveAuthToken(); got != "secret" {
			t.Fatalf("token = %q", got)
		}
	})

	t.Run("token file takes precedence", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		c := NewClient("http://example").WithToken("static").WithTokenPath(path)
		if got := c.resolveAuthToken(); got != "from-file" {
			t.Fatalf("token = %q, want from-file", got)
		}
	})

	t.Run("falls back when file missing", func(t *testing.T) {
		t.Parallel()
		c := NewClient("http://example").
			WithToken("fallback").
			WithTokenPath(filepath.Join(t.TempDir(), "missing"))
		if got := c.resolveAuthToken(); got != "fallback" {
			t.Fatalf("token = %q, want fallback", got)
		}
	})
}

func TestApplyAuthHeader(t *testing.T) {
	t.Parallel()

	t.Run("adds Bearer prefix", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "http://example", nil)
		NewClient("http://example").WithToken("abc").applyAuthHeader(req)
		if got := req.Header.Get("Authorization"); got != "Bearer abc" {
			t.Fatalf("Authorization = %q", got)
		}
	})

	t.Run("preserves existing scheme", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, "http://example", nil)
		NewClient("http://example").WithToken("Bearer preset").applyAuthHeader(req)
		if got := req.Header.Get("Authorization"); got != "Bearer preset" {
			t.Fatalf("Authorization = %q", got)
		}
	})
}

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
