package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestStageDataSourcesValidatesBeforeDownloading(t *testing.T) {
	valid := shared.DataStagingSource{Name: "results", Source: api.TestDataRef{Git: &api.GitTestDataRef{URL: "https://example.org/data", Ref: "main"}}}
	for _, test := range []struct {
		name    string
		sources []shared.DataStagingSource
	}{
		{"empty", nil}, {"duplicate", []shared.DataStagingSource{valid, valid}},
		{"unsafe name", []shared.DataStagingSource{{Name: "../escape", Source: valid.Source}}},
		{"PVC must be mounted", []shared.DataStagingSource{{Name: "results", Source: api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: "data"}}}}},
		{"multiple sources", []shared.DataStagingSource{{Name: "results", Source: api.TestDataRef{Git: valid.Source.Git, HF: &api.HFTestDataRef{RepoID: "org/data"}}}}},
		{"unsafe secret", []shared.DataStagingSource{{Name: "results", Source: api.TestDataRef{S3: &api.S3TestDataRef{SecretRef: "../bad"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			err := stageDataSources(shared.DataStagingPlan{Sources: test.sources}, func(shared.DataStagingSource) error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("invalid plan reached downloader: err=%v called=%v", err, called)
			}
		})
	}
	plan := shared.DataStagingPlan{Sources: []shared.DataStagingSource{valid, {Name: "calibration-0", Source: valid.Source}}}
	var calls []string
	err := stageDataSources(plan, func(source shared.DataStagingSource) error {
		calls = append(calls, source.Name)
		return errors.New("unavailable")
	})
	if err == nil || !strings.Contains(err.Error(), "stage results") || !reflect.DeepEqual(calls, []string{"results"}) {
		t.Fatalf("failure handling: calls=%v error=%v", calls, err)
	}
}

func TestStagingSourcesHaveIsolatedConfiguration(t *testing.T) {
	a, err := stagingSourceEnv(shared.DataStagingSource{Name: "results", Source: api.TestDataRef{S3: &api.S3TestDataRef{Bucket: "data", Key: "predictions", SecretRef: "s3-auth"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := stagingSourceEnv(shared.DataStagingSource{Name: "calibration-0", Source: api.TestDataRef{HF: &api.HFTestDataRef{RepoID: "org/labels", Revision: "pinned", SubPath: "labels"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a[envInitDestination] == b[envInitDestination] || a[envInitMetadataDir] == b[envInitMetadataDir] || a[envInitSecretDir] == b[envInitSecretDir] {
		t.Fatal("references share paths")
	}
	if b[envBucket] != "" || b[envGitURL] != "" || b[envHFRevision] != "pinned" || b[envHFSubPath] != "labels" || b[shared.DataStagingPlanEnv] != "" {
		t.Fatalf("worker inherits another source: %v", b)
	}
}

// Exercise the existing executable and S3 downloader, not a replacement downloader.
func TestInitExecutableStagesMultipleS3Sources(t *testing.T) {
	files := map[string]string{"predictions/results.json": "{\"samples\":[]}", "labels/calibration.jsonl": "{\"sample_id\":1,\"label\":1}\n"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			prefix := r.URL.Query().Get("prefix")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, "<ListBucketResult><IsTruncated>false</IsTruncated>")
			for key, value := range files {
				if strings.HasPrefix(key, prefix) {
					_, _ = fmt.Fprintf(w, "<Contents><Key>%s</Key><Size>%d</Size></Contents>", key, len(value))
				}
			}
			_, _ = fmt.Fprint(w, "</ListBucketResult>")
			return
		}
		if value, ok := files[strings.TrimPrefix(r.URL.Path, "/bucket/")]; ok {
			_, _ = fmt.Fprint(w, value)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	secretRoot := filepath.Join(root, "secrets")
	for _, name := range []string{"predictions-auth", "labels-auth"} {
		dir := filepath.Join(secretRoot, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{accessKeyIDKey: "access", awsAccessKeyEnvName: "secret", regionOptionalKey: "us-east-1", endpointKey: server.URL} {
			if err := os.WriteFile(filepath.Join(dir, key), []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	binary := filepath.Join(root, "eval-runtime-init")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build init: %v\n%s", err, output)
	}
	plan := shared.DataStagingPlan{Sources: []shared.DataStagingSource{
		{Name: "results", Source: api.TestDataRef{S3: &api.S3TestDataRef{Bucket: "bucket", Key: "predictions/", SecretRef: "predictions-auth"}}},
		{Name: "calibration-0", Source: api.TestDataRef{S3: &api.S3TestDataRef{Bucket: "bucket", Key: "labels/", SecretRef: "labels-auth"}}},
	}}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "data-sources.json")
	if err := os.WriteFile(planPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), shared.DataStagingPlanEnv+"="+planPath, envStagingRoot+"="+dataRoot, envStagingSecretRoot+"="+secretRoot)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("staging: %v\n%s", err, output)
	}
	for file, want := range map[string]string{"results/results.json": files["predictions/results.json"], "calibration-0/calibration.jsonl": files["labels/calibration.jsonl"]} {
		got, err := os.ReadFile(filepath.Join(dataRoot, file))
		if err != nil || string(got) != want {
			t.Fatalf("staged %s: %q error=%v", file, got, err)
		}
	}
}
