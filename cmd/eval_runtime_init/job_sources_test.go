package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestSourceDownloadersHaveIsolatedConfiguration(t *testing.T) {
	root := t.TempDir()
	a, err := sourceDownloader(shared.NamedDataSource{Name: "results", Source: api.TestDataRef{S3: &api.S3TestDataRef{Bucket: "data", Key: "predictions", SecretRef: "s3-auth"}}}, root, root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sourceDownloader(shared.NamedDataSource{Name: "calibration-0", Source: api.TestDataRef{HF: &api.HFTestDataRef{RepoID: "org/labels", Revision: "pinned", SubPath: "labels"}}}, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if a.destDir == b.destDir || a.gitMetadataDir == b.gitMetadataDir || a.scrtDir == b.scrtDir {
		t.Fatal("references share paths")
	}
	if b.source.S3 != nil || b.source.Git != nil || b.source.HF.Revision != "pinned" {
		t.Fatalf("source configuration leaked: %+v", b)
	}
	for _, secret := range []string{"", "../bad", "/bad"} {
		_, err := sourceDownloader(shared.NamedDataSource{Name: "results", Source: api.TestDataRef{S3: &api.S3TestDataRef{SecretRef: secret}}}, root, root)
		if err == nil {
			t.Fatalf("accepted invalid S3 secret %q", secret)
		}
	}
}

func TestRunReadsJobSpec(t *testing.T) {
	t.Setenv(shared.InitJobSpecPathEnv, filepath.Join(t.TempDir(), "missing.json"))
	if err := run(); err == nil || !strings.Contains(err.Error(), "read init job") {
		t.Fatalf("job path was not read: %v", err)
	}
}

// Exercise the existing S3 downloader with references from the actual job format.
func TestInitDownloadsMultipleS3SourcesFromJob(t *testing.T) {
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
	operations := api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
		ResultsDataRef:                 &api.PostProcessingResultsDataRef{S3: &api.S3TestDataRef{Bucket: "bucket", Key: "predictions/", SecretRef: "predictions-auth"}},
		ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{CalibrationDataRef: []api.CalibrationDataRef{{S3: &api.S3TestDataRef{Bucket: "bucket", Key: "labels/", SecretRef: "labels-auth"}}}},
	}}
	spec := shared.JobSpec{Parameters: map[string]any{"operations": operations}}
	payload, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "job.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	// Inherited evaluation source settings must not affect job references.
	t.Setenv(envGitURL, "https://invalid.example/ignored")
	if err := runJobSources(path, dataRoot, secretRoot); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{"results/results.json": files["predictions/results.json"], "calibration-0/calibration.jsonl": files["labels/calibration.jsonl"]} {
		got, err := os.ReadFile(filepath.Join(dataRoot, file))
		if err != nil || string(got) != want {
			t.Fatalf("staged %s: %q error=%v", file, got, err)
		}
	}
}

func writeInitJob(t *testing.T, root string, results *api.PostProcessingResultsDataRef, calibration []api.CalibrationDataRef) string {
	t.Helper()
	spec := shared.JobSpec{Parameters: map[string]any{"operations": api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{ResultsDataRef: results, ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{CalibrationDataRef: calibration}}}}}
	payload, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "job.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitDownloadsGitAndHFWithoutInheritedCredentials(t *testing.T) {
	root := t.TempDir()
	repo := newLocalBareRepo(t)
	original := validateGitCloneURL
	validateGitCloneURL = func(string, func(string) ([]net.IP, error)) error { return nil }
	t.Cleanup(func() { validateGitCloneURL = original })
	srv := newMockHFServer(t, "org/labels", map[string]string{"labels.jsonl": "calibration"}, http.StatusOK)
	defer srv.Close()
	t.Setenv("HF_ENDPOINT", srv.URL)
	// Both sources are public; an unrelated credential directory must be ignored.
	t.Setenv(envGitURL, "https://invalid.example/ignored")
	path := writeInitJob(t, root,
		&api.PostProcessingResultsDataRef{Git: &api.GitTestDataRef{URL: "file://" + repo, Ref: "master"}},
		[]api.CalibrationDataRef{{PVC: &api.PVCTestDataRef{ClaimName: "mounted"}}, {HF: &api.HFTestDataRef{RepoID: "org/labels"}}})
	stage := filepath.Join(root, "stage")
	if err := runJobSources(path, stage, filepath.Join(root, "secrets")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stage, "results")); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(stage, "calibration-1", "labels.jsonl"))
	if err != nil || string(content) != "calibration" {
		t.Fatalf("HF labels: %q error=%v", content, err)
	}
	for _, name := range []string{"results", "calibration-1"} {
		if _, err := os.Stat(filepath.Join(stage, ".metadata", name, ".git-metadata")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(stage, "calibration-0")); !os.IsNotExist(err) {
		t.Fatal("PVC input must not be downloaded")
	}
}

func TestInitJobDownloadFailureIncludesRole(t *testing.T) {
	root := t.TempDir()
	path := writeInitJob(t, root, &api.PostProcessingResultsDataRef{S3: &api.S3TestDataRef{Bucket: "data", Key: "results/", SecretRef: "missing"}}, nil)
	if err := runJobSources(path, root, root); err == nil || !strings.Contains(err.Error(), "download results") {
		t.Fatalf("error lacks input role: %v", err)
	}
}
