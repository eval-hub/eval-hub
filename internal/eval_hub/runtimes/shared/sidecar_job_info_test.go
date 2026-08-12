package shared_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
)

func TestWriteSidecarJobInfo(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	info := &shared.SidecarJobInfo{
		Model: &config.SidecarModelConfig{
			URL:                 "https://model-serving.example.com/v1",
			AuthSecretMountPath: "/home/user1/model-auth",
			HTTPTimeout:         60 * time.Second,
		},
	}

	path, err := shared.WriteSidecarJobInfo(tmpDir, "job-123", info)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "job-123", shared.SidecarJobInfoFileName)
	if path != expectedPath {
		t.Fatalf("expected path %q, got %q", expectedPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to exist, got %v", err)
	}

	var parsed shared.SidecarJobInfo
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if parsed.Model == nil {
		t.Fatal("expected model config, got nil")
	}
	if parsed.Model.URL != "https://model-serving.example.com/v1" {
		t.Fatalf("expected model URL %q, got %q", "https://model-serving.example.com/v1", parsed.Model.URL)
	}
	if parsed.Model.AuthSecretMountPath != "/home/user1/model-auth" {
		t.Fatalf("expected auth path %q, got %q", "/home/user1/model-auth", parsed.Model.AuthSecretMountPath)
	}
	if parsed.Model.HTTPTimeout != 60*time.Second {
		t.Fatalf("expected timeout %v, got %v", 60*time.Second, parsed.Model.HTTPTimeout)
	}
}

func TestWriteSidecarJobInfoNoAuth(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	info := &shared.SidecarJobInfo{
		Model: &config.SidecarModelConfig{
			URL:         "https://open-model.example.com/v1",
			HTTPTimeout: 60 * time.Second,
		},
	}

	path, err := shared.WriteSidecarJobInfo(tmpDir, "job-no-auth", info)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to exist, got %v", err)
	}

	var parsed shared.SidecarJobInfo
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if parsed.Model.AuthSecretMountPath != "" {
		t.Fatalf("expected empty auth path, got %q", parsed.Model.AuthSecretMountPath)
	}
}

func TestRewriteModelURLForLocalSidecar(t *testing.T) {
	tests := []struct {
		name           string
		sidecarBaseURL string
		jobID          string
		modelURL       string
		want           string
		wantErr        bool
	}{
		{
			name:           "standard model URL with path",
			sidecarBaseURL: "http://localhost:8082",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com/v1",
			want:           "http://localhost:8082/model/job-abc/v1",
		},
		{
			name:           "model URL with multi-segment path",
			sidecarBaseURL: "http://localhost:8082",
			jobID:          "job-abc",
			modelURL:       "https://gateway.example.com/llm/llama-3/v1",
			want:           "http://localhost:8082/model/job-abc/llm/llama-3/v1",
		},
		{
			name:           "model URL without path",
			sidecarBaseURL: "http://localhost:8082",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com",
			want:           "http://localhost:8082/model/job-abc",
		},
		{
			name:           "sidecar with trailing slash",
			sidecarBaseURL: "http://localhost:8082/",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com/v1",
			want:           "http://localhost:8082/model/job-abc/v1",
		},
		{
			name:           "model URL with trailing slash",
			sidecarBaseURL: "http://localhost:8082",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com/v1/",
			want:           "http://localhost:8082/model/job-abc/v1",
		},
		{
			name:           "model URL with query string",
			sidecarBaseURL: "http://localhost:8082",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com/v1?timeout=30",
			want:           "http://localhost:8082/model/job-abc/v1?timeout=30",
		},
		{
			name:           "empty sidecar host",
			sidecarBaseURL: "http://",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com/v1",
			wantErr:        true,
		},
		{
			name:           "invalid sidecar URL",
			sidecarBaseURL: "://invalid",
			jobID:          "job-abc",
			modelURL:       "https://model-serving.example.com/v1",
			wantErr:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := shared.RewriteModelURLForLocalSidecar(tc.sidecarBaseURL, tc.jobID, tc.modelURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result: %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
