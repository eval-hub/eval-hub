package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gomlx/go-huggingface/hub"
)

const testHFCommitSHA = "aabbccddeeff00112233445566778899aabbccdd"

type hfRepoInfoResponse struct {
	ID       string `json:"id"`
	SHA      string `json:"sha"`
	Siblings []struct {
		RFilename string `json:"rfilename"`
	} `json:"siblings"`
}

func newMockHFServer(t *testing.T, repoID string, files map[string]string, infoStatus int) *httptest.Server {
	return newMockHFServerWithSHA(t, repoID, testHFCommitSHA, files, infoStatus)
}

func newMockHFServerWithSHA(t *testing.T, repoID, commitSHA string, files map[string]string, infoStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/datasets/"+repoID+"/revision/") {
			if infoStatus != http.StatusOK {
				http.Error(w, "gated", infoStatus)
				return
			}
			resp := hfRepoInfoResponse{ID: repoID, SHA: commitSHA}
			for name := range files {
				resp.Siblings = append(resp.Siblings, struct {
					RFilename string `json:"rfilename"`
				}{RFilename: name})
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("encode repo info: %v", err)
			}
			return
		}

		prefix := "/datasets/" + repoID + "/resolve/" + commitSHA + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, prefix)
		content, ok := files[rel]
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := "etag-" + strings.ReplaceAll(rel, "/", "-")
		w.Header().Set("X-Repo-Commit", commitSHA)
		w.Header().Set("X-Linked-Etag", `"`+etag+`"`)
		w.Header().Set("ETag", `"`+etag+`"`)
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(content))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	return srv
}

func withRunHFTestEnv(t *testing.T, dest, meta, secret, cache string) {
	t.Helper()
	origDest, origMeta, origSecret, origCache := destDir, gitMetadataDir, scrtDir, hfCacheDir
	destDir, gitMetadataDir, scrtDir, hfCacheDir = dest, meta, secret, cache
	t.Cleanup(func() {
		destDir, gitMetadataDir, scrtDir, hfCacheDir = origDest, origMeta, origSecret, origCache
	})
	t.Setenv(envHFRevision, "")
	t.Setenv(envHFSubPath, "")
	t.Setenv(envHFTimeout, "")
}

func TestValidateHFSubPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subPath string
		wantErr string
	}{
		{name: "valid nested", subPath: "staging_sub_path"},
		{name: "valid single file", subPath: "data/train.jsonl"},
		{name: "traversal rejected", subPath: "../etc", wantErr: "escapes repository root"},
		{name: "absolute rejected", subPath: "/abs", wantErr: "escapes repository root"},
		{name: "non-canonical rejected", subPath: "data/../README.md", wantErr: "canonical relative path"},
		{name: "dot segment rejected", subPath: "./staging", wantErr: "canonical relative path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateHFSubPath(tt.subPath)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateHFSubPath() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateHFSubPath() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFileMatchesSubPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fileName string
		subPath  string
		want     bool
	}{
		{fileName: "staging_sub_path/foo.txt", subPath: "staging_sub_path", want: true},
		{fileName: "staging_sub_path", subPath: "staging_sub_path", want: true},
		{fileName: "other/foo.txt", subPath: "staging_sub_path", want: false},
		{fileName: "anything", subPath: "", want: true},
	}

	for _, tt := range tests {
		got := fileMatchesSubPath(tt.fileName, tt.subPath)
		if got != tt.want {
			t.Fatalf("fileMatchesSubPath(%q, %q) = %v, want %v", tt.fileName, tt.subPath, got, tt.want)
		}
	}
}

func TestClassifyHFError(t *testing.T) {
	t.Parallel()

	gated := classifyHFError("org/repo", false, errors.New("401 Client Error: gated repo"))
	if !strings.Contains(gated.Error(), "gated") {
		t.Fatalf("expected gated message, got %v", gated)
	}

	gatedOnly := classifyHFError("org/repo", false, errors.New("gated repo"))
	if !strings.Contains(gatedOnly.Error(), "gated") {
		t.Fatalf("expected gated message, got %v", gatedOnly)
	}

	wantMissingOrPrivate := "repository eval-hub-test/invalid-db not found or is private; provide secret_ref with a Hugging Face token if the dataset is private or gated"

	notFound := classifyHFError("eval-hub-test/invalid-db", false, errors.New("404 Repository Not Found"))
	if notFound.Error() != wantMissingOrPrivate {
		t.Fatalf("expected common missing/private message for 404, got %v", notFound)
	}

	// huggingface_hub wraps missing/private repos as 401 + "Repository Not Found".
	hfMissing := classifyHFError("eval-hub-test/invalid-db", false, errors.New(
		"401 Client Error. (Request ID: Root=1-abc)\n\nRepository Not Found for url: https://huggingface.co/api/datasets/eval-hub-test/invalid-db.\nPlease make sure you specified the correct `repo_id` and `repo_type`.\nIf you are trying to access a private or gated repo, make sure you are authenticated.",
	))
	if hfMissing.Error() != wantMissingOrPrivate {
		t.Fatalf("expected common missing/private message, got %v", hfMissing)
	}

	// go-huggingface returns only 401 + "Invalid username or password" for missing repos.
	goHFMissing := classifyHFError("eval-hub-test/invalid-db", false, errors.New(
		`failed to download repository info: while downloading "https://huggingface.co/api/datasets/eval-hub-test/invalid-db/revision/main?blobs=true": bad status code 401: Invalid username or password.`,
	))
	if goHFMissing.Error() != wantMissingOrPrivate {
		t.Fatalf("expected common missing/private message for go-huggingface 401, got %v", goHFMissing)
	}
	if strings.Contains(goHFMissing.Error(), "huggingface.co") {
		t.Fatalf("expected user-facing message without hub URL, got %v", goHFMissing)
	}

	authFailed := classifyHFError("org/repo", true, errors.New(
		`bad status code 401: Invalid username or password.`,
	))
	if authFailed.Error() != "hugging face authentication failed for repository org/repo; check secret_ref token" {
		t.Fatalf("expected clean auth failure message, got %v", authFailed)
	}
	if strings.Contains(authFailed.Error(), "not found or is private") {
		t.Fatalf("expected not to classify authenticated 401 as missing/private, got %v", authFailed)
	}

	raw := errors.New("connection reset by peer")
	if classifyHFError("org/repo", false, raw) != raw {
		t.Fatalf("expected unclassified error to pass through unchanged")
	}
}

func TestStageHFSubPathAndClearDest(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	snapshot := filepath.Join(tmp, "snap")
	sub := filepath.Join(snapshot, "staging_sub_path")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "data.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotRoot, err := os.OpenRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshotRoot.Close() }()

	dest := filepath.Join(tmp, "dest")
	if err := stageHFSubPath(snapshotRoot, "staging_sub_path", dest); err != nil {
		t.Fatalf("stageHFSubPath: %v", err)
	}
	if !destHasData(dest) {
		t.Fatal("expected staged data")
	}

	if err := clearDestDir(dest); err != nil {
		t.Fatalf("clearDestDir: %v", err)
	}
	if destHasData(dest) {
		t.Fatal("expected dest cleared")
	}
}

func TestRevisionOrDefault(t *testing.T) {
	t.Parallel()

	if revisionOrDefault("main") != "main" {
		t.Fatal("expected explicit revision")
	}
	if revisionOrDefault("") != "default" {
		t.Fatal("expected default revision label")
	}
}

func TestRunHF_RequiresRepoID(t *testing.T) {
	t.Setenv(envHFRepoID, "")
	err := runHF()
	if err == nil {
		t.Fatal("runHF() = nil, want missing repo id error")
	}
	if !strings.Contains(err.Error(), envHFRepoID) {
		t.Fatalf("runHF() error = %v, want mention of %s", err, envHFRepoID)
	}
}

func TestRunHF_RejectsInvalidSubPath(t *testing.T) {
	t.Setenv(envHFRepoID, "org/repo")
	t.Setenv(envHFSubPath, "../escape")
	err := runHF()
	if err == nil {
		t.Fatal("runHF() = nil, want invalid sub_path error")
	}
	if !strings.Contains(err.Error(), "escapes repository root") {
		t.Fatalf("runHF() error = %v, want sub_path validation error", err)
	}
}

func TestStageHFSubPath_SingleFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	snapshot := filepath.Join(tmp, "snap")
	if err := os.MkdirAll(snapshot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "data.jsonl"), []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotRoot, err := os.OpenRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshotRoot.Close() }()

	dest := filepath.Join(tmp, "dest")
	if err := stageHFSubPath(snapshotRoot, "data.jsonl", dest); err != nil {
		t.Fatalf("stageHFSubPath: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "data.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "line\n" {
		t.Fatalf("unexpected file content: %q", got)
	}
}

func TestDestHasData(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	if destHasData(empty) {
		t.Fatal("expected empty dir to have no data")
	}

	withData := t.TempDir()
	if err := os.WriteFile(filepath.Join(withData, "x"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !destHasData(withData) {
		t.Fatal("expected dir with file to have data")
	}
}

func TestWriteTerminationMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage("repository org/gated is gated; provide secret_ref")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "gated") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestWriteTerminationMessage_EmptySkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage("   ")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected no file for empty termination message")
	}
}

func TestWriteTerminationMessage_TruncatesLongMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage(strings.Repeat("x", maxTerminationMessageBytes+10))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(content) > maxTerminationMessageBytes+1 {
		t.Fatalf("termination message too long: %d bytes", len(content))
	}
}

func TestWriteTerminationMessageFile_InvalidPath(t *testing.T) {
	t.Setenv("TERMINATION_MESSAGE_PATH", "/tmp/")
	err := writeTerminationMessageFile([]byte("fail"))
	if err == nil {
		t.Fatal("expected error for invalid termination message path")
	}
}

func TestRunHF_HappyPathAndSubPath(t *testing.T) {
	repoID := "org/offline-dataset"
	files := map[string]string{
		"README.md":                 "hello",
		"staging_sub_path/data.txt": "samples",
	}
	srv := newMockHFServer(t, repoID, files, http.StatusOK)
	defer srv.Close()

	dest := t.TempDir()
	meta := t.TempDir()
	secret := filepath.Join(t.TempDir(), "missing-secret")
	cache := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest, meta, secret, cache)
	t.Setenv("HF_ENDPOINT", srv.URL)
	t.Setenv(envHFRepoID, repoID)

	if err := runHF(); err != nil {
		t.Fatalf("runHF: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "README.md"))
	if err != nil {
		t.Fatalf("README.md missing: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("README.md = %q, want hello", got)
	}
	sha, err := os.ReadFile(filepath.Join(meta, ".git-metadata"))
	if err != nil {
		t.Fatalf("metadata missing: %v", err)
	}
	if strings.TrimSpace(string(sha)) != testHFCommitSHA {
		t.Fatalf("metadata SHA = %q, want %s", strings.TrimSpace(string(sha)), testHFCommitSHA)
	}

	dest2 := t.TempDir()
	meta2 := t.TempDir()
	cache2 := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest2, meta2, secret, cache2)
	t.Setenv("HF_ENDPOINT", srv.URL)
	t.Setenv(envHFRepoID, repoID)
	t.Setenv(envHFSubPath, "staging_sub_path")
	if err := runHF(); err != nil {
		t.Fatalf("runHF with sub_path: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(dest2, "data.txt"))
	if err != nil {
		t.Fatalf("sub_path data.txt missing: %v", err)
	}
	if string(got) != "samples" {
		t.Fatalf("data.txt = %q, want samples", got)
	}
	if _, err := os.Stat(filepath.Join(dest2, "README.md")); !os.IsNotExist(err) {
		t.Fatal("README.md should not appear when using sub_path")
	}
}

func TestRunHF_WithRevisionAndToken(t *testing.T) {
	repoID := "org/private-dataset"
	files := map[string]string{"data.jsonl": "line\n"}
	srv := newMockHFServer(t, repoID, files, http.StatusOK)
	defer srv.Close()

	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, hfTokenKey), []byte("hf_test_token"), 0o600); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	meta := t.TempDir()
	cache := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest, meta, secretDir, cache)
	t.Setenv("HF_ENDPOINT", srv.URL)
	t.Setenv(envHFRepoID, repoID)
	t.Setenv(envHFRevision, "main")

	if err := runHF(); err != nil {
		t.Fatalf("runHF: %v", err)
	}
	if !destHasData(dest) {
		t.Fatal("expected staged data")
	}
}

func TestRunHF_ClassifiedErrors(t *testing.T) {
	repoID := "org/gated-dataset"
	srv := newMockHFServer(t, repoID, nil, http.StatusUnauthorized)
	defer srv.Close()

	dest := t.TempDir()
	meta := t.TempDir()
	cache := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest, meta, filepath.Join(t.TempDir(), "missing"), cache)
	t.Setenv("HF_ENDPOINT", srv.URL)
	t.Setenv(envHFRepoID, repoID)

	err := runHF()
	if err == nil || !strings.Contains(err.Error(), "gated") {
		t.Fatalf("runHF() = %v, want gated error", err)
	}

	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv404.Close()
	withRunHFTestEnv(t, dest, meta, filepath.Join(t.TempDir(), "missing"), cache)
	t.Setenv("HF_ENDPOINT", srv404.URL)
	t.Setenv(envHFRepoID, "org/missing")
	err = runHF()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("runHF() = %v, want not found error", err)
	}
}

func TestRunHF_InvalidTimeoutDuration(t *testing.T) {
	dest := t.TempDir()
	meta := t.TempDir()
	cache := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest, meta, filepath.Join(t.TempDir(), "missing"), cache)
	t.Setenv(envHFRepoID, "org/offline-dataset")
	t.Setenv(envHFTimeout, "not-a-duration")

	err := runHF()
	if err == nil || !strings.Contains(err.Error(), envHFTimeout) {
		t.Fatalf("runHF() = %v, want invalid timeout error", err)
	}
}

func TestDownloadHFRepo_SubPathMissingInRepo(t *testing.T) {
	repoID := "org/empty-subpath"
	files := map[string]string{"README.md": "hello"}
	srv := newMockHFServer(t, repoID, files, http.StatusOK)
	defer srv.Close()

	t.Setenv("HF_ENDPOINT", srv.URL)
	origCache := hfCacheDir
	hfCacheDir = filepath.Join(t.TempDir(), "hf-cache")
	t.Cleanup(func() { hfCacheDir = origCache })

	_, err := downloadHFRepo(t.Context(), repoID, "", "missing-dir", "")
	if err == nil || !strings.Contains(err.Error(), "not found in repository") {
		t.Fatalf("downloadHFRepo() = %v, want sub_path not found error", err)
	}
}

func TestDownloadHFRepo_NoFilesInRepo(t *testing.T) {
	repoID := "org/no-files"
	srv := newMockHFServer(t, repoID, map[string]string{}, http.StatusOK)
	defer srv.Close()

	t.Setenv("HF_ENDPOINT", srv.URL)
	origCache := hfCacheDir
	hfCacheDir = filepath.Join(t.TempDir(), "hf-cache")
	t.Cleanup(func() { hfCacheDir = origCache })

	_, err := downloadHFRepo(t.Context(), repoID, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "no files found") {
		t.Fatalf("downloadHFRepo() = %v, want no files error", err)
	}
}

func TestClassifyHFError_Nil(t *testing.T) {
	if classifyHFError("org/repo", false, nil) != nil {
		t.Fatal("expected nil for nil error")
	}
}

func TestDownloadHFInfo_ContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repo := hub.New("org/slow").WithType(hub.RepoTypeDataset).WithCacheDir(t.TempDir())
	err := downloadHFInfo(ctx, repo, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("downloadHFInfo() = %v, want context.Canceled", err)
	}
}

func TestDownloadHFRepo_InvalidCommitSHA(t *testing.T) {
	repoID := "org/bad-sha"
	srv := newMockHFServerWithSHA(t, repoID, "not-a-valid-sha", map[string]string{"README.md": "x"}, http.StatusOK)
	defer srv.Close()

	t.Setenv("HF_ENDPOINT", srv.URL)
	origCache := hfCacheDir
	hfCacheDir = filepath.Join(t.TempDir(), "hf-cache")
	t.Cleanup(func() { hfCacheDir = origCache })

	_, err := downloadHFRepo(t.Context(), repoID, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "invalid commit SHA") {
		t.Fatalf("downloadHFRepo() = %v, want invalid commit SHA error", err)
	}
}

func TestDownloadHFRepo_EmptyCommitSHA(t *testing.T) {
	repoID := "org/empty-sha"
	srv := newMockHFServerWithSHA(t, repoID, "", map[string]string{"README.md": "x"}, http.StatusOK)
	defer srv.Close()

	t.Setenv("HF_ENDPOINT", srv.URL)
	origCache := hfCacheDir
	hfCacheDir = filepath.Join(t.TempDir(), "hf-cache")
	t.Cleanup(func() { hfCacheDir = origCache })

	_, err := downloadHFRepo(t.Context(), repoID, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "could not resolve commit SHA") {
		t.Fatalf("downloadHFRepo() = %v, want missing commit SHA error", err)
	}
}

func TestResolveHFRepoFileRel_RejectsInvalidPath(t *testing.T) {
	_, err := resolveHFRepoFileRel(t.TempDir(), testHFCommitSHA, "../escape")
	if err == nil || !strings.Contains(err.Error(), "invalid repository file path") {
		t.Fatalf("resolveHFRepoFileRel() = %v, want invalid path error", err)
	}
}

func TestResolveHFRepoFileRel_MissingFile(t *testing.T) {
	_, err := resolveHFRepoFileRel(t.TempDir(), testHFCommitSHA, "missing.txt")
	if err == nil {
		t.Fatal("resolveHFRepoFileRel() = nil, want error for missing file")
	}
}

func TestClearDestDir_RemovesFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearDestDir(dir); err != nil {
		t.Fatalf("clearDestDir: %v", err)
	}
	if destHasData(dir) {
		t.Fatal("expected dest cleared")
	}
}

func TestClearDestDirPreservesHFCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, hfCacheDirName)
	if err := os.MkdirAll(filepath.Join(cache, "snapshots"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tokenizer"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer", "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearDestDir(dir); err != nil {
		t.Fatalf("clearDestDir: %v", err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("expected .hf-cache preserved")
	}
	if _, err := os.Stat(filepath.Join(dir, "tokenizer")); err == nil {
		t.Fatal("expected staged files removed")
	}
}

func TestEffectiveHFCacheDir_DefaultUnderTestData(t *testing.T) {
	origDest, origOverride := destDir, hfCacheDir
	destDir = "/test_data"
	hfCacheDir = ""
	defer func() {
		destDir, hfCacheDir = origDest, origOverride
	}()

	got := effectiveHFCacheDir()
	want := filepath.Join("/test_data", hfCacheDirName)
	if got != want {
		t.Fatalf("effectiveHFCacheDir() = %q, want %q", got, want)
	}
}

func TestCopyHFRepoFile_MissingSource(t *testing.T) {
	srcRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srcRoot.Close() }()
	dstRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstRoot.Close() }()

	if err := copyHFRepoFile(srcRoot, dstRoot, "missing", "out.txt"); err == nil {
		t.Fatal("copyHFRepoFile() = nil, want missing source error")
	}
}

func TestCopyHFRepoFile_CopiesContent(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "blob"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcRoot, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srcRoot.Close() }()
	dstRoot, err := os.OpenRoot(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstRoot.Close() }()

	if err := copyHFRepoFile(srcRoot, dstRoot, "blob", "out.txt"); err != nil {
		t.Fatalf("copyHFRepoFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("copied content = %q, want payload", got)
	}
}

func TestResolveHFToken_MissingKeyWhenSecretMounted(t *testing.T) {
	secretDir := t.TempDir()
	orig := scrtDir
	scrtDir = secretDir
	t.Cleanup(func() { scrtDir = orig })

	_, err := resolveHFToken()
	if err == nil || !strings.Contains(err.Error(), "hugging face auth") {
		t.Fatalf("resolveHFToken() = %v, want hugging face auth error", err)
	}
	if strings.Contains(err.Error(), "huggingface.co") {
		t.Fatalf("expected clean auth error without hub URL, got %v", err)
	}
}

func TestRunHF_SecretReadFailure(t *testing.T) {
	dest := t.TempDir()
	meta := t.TempDir()
	cache := filepath.Join(t.TempDir(), "hf-cache")
	badSecret := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badSecret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	withRunHFTestEnv(t, dest, meta, badSecret, cache)
	t.Setenv(envHFRepoID, "org/offline-dataset")

	err := runHF()
	if err == nil || !strings.Contains(err.Error(), "hugging face auth") {
		t.Fatalf("runHF() = %v, want secret auth error", err)
	}
}

func TestRunHF_WriteGitMetadataFailure(t *testing.T) {
	repoID := "org/offline-dataset"
	files := map[string]string{"README.md": "hello"}
	srv := newMockHFServer(t, repoID, files, http.StatusOK)
	defer srv.Close()

	dest := t.TempDir()
	metaFile := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(metaFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "missing-secret")
	cache := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest, metaFile, secret, cache)
	t.Setenv("HF_ENDPOINT", srv.URL)
	t.Setenv(envHFRepoID, repoID)

	err := runHF()
	if err == nil || !strings.Contains(err.Error(), "write git metadata") {
		t.Fatalf("runHF() = %v, want git metadata error", err)
	}
}

func TestStageHFFiles_FromSymlinkedCache(t *testing.T) {
	cache := t.TempDir()
	dest := t.TempDir()
	commit := testHFCommitSHA
	blobDir := filepath.Join(cache, "blobs")
	snapDir := filepath.Join(cache, "snapshots", commit)
	if err := os.MkdirAll(blobDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snapDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, "etag-1"), []byte("blob-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "blobs", "etag-1"), filepath.Join(snapDir, "file.txt")); err != nil {
		t.Fatal(err)
	}

	if err := stageHFFiles(cache, commit, []string{"file.txt"}, "", dest); err != nil {
		t.Fatalf("stageHFFiles: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("staged file missing: %v", err)
	}
	if string(got) != "blob-data" {
		t.Fatalf("staged content = %q, want blob-data", got)
	}
}

func TestDownloadHFRepo_FileDownloadError(t *testing.T) {
	repoID := "org/fail-download"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/datasets/"+repoID+"/revision/") {
			resp := hfRepoInfoResponse{
				ID:  repoID,
				SHA: testHFCommitSHA,
				Siblings: []struct {
					RFilename string `json:"rfilename"`
				}{{RFilename: "README.md"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "download failed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("HF_ENDPOINT", srv.URL)
	origCache := hfCacheDir
	hfCacheDir = filepath.Join(t.TempDir(), "hf-cache")
	t.Cleanup(func() { hfCacheDir = origCache })

	_, err := downloadHFRepo(t.Context(), repoID, "", "", "")
	if err == nil {
		t.Fatal("downloadHFRepo() = nil, want file download error")
	}
}

func TestDownloadHFRepo_SecondInfoDownloadFails(t *testing.T) {
	repoID := "org/fail-second-info"
	infoCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/datasets/"+repoID+"/revision/") {
			http.NotFound(w, r)
			return
		}
		infoCalls++
		if infoCalls > 1 {
			http.Error(w, "info refresh failed", http.StatusInternalServerError)
			return
		}
		resp := hfRepoInfoResponse{
			ID:  repoID,
			SHA: testHFCommitSHA,
			Siblings: []struct {
				RFilename string `json:"rfilename"`
			}{{RFilename: "README.md"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("HF_ENDPOINT", srv.URL)
	origCache := hfCacheDir
	hfCacheDir = filepath.Join(t.TempDir(), "hf-cache")
	t.Cleanup(func() { hfCacheDir = origCache })

	_, err := downloadHFRepo(t.Context(), repoID, "", "", "")
	if err == nil {
		t.Fatal("downloadHFRepo() = nil, want second info download error")
	}
}

func TestResolveHFRepoFileRel_SymlinkOutsideCache(t *testing.T) {
	cache := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(cache, "snapshots", testHFCommitSHA)
	if err := os.MkdirAll(snap, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(snap, "evil.txt")); err != nil {
		t.Fatal(err)
	}

	_, err := resolveHFRepoFileRel(cache, testHFCommitSHA, "evil.txt")
	if err == nil || (!strings.Contains(err.Error(), "resolves outside cache root") && !strings.Contains(err.Error(), "escapes cache root")) {
		t.Fatalf("resolveHFRepoFileRel() = %v, want cache confinement error", err)
	}
}

func TestRunHF_NestedFilePaths(t *testing.T) {
	repoID := "org/nested-dataset"
	files := map[string]string{"data/nested/file.txt": "nested-content"}
	srv := newMockHFServer(t, repoID, files, http.StatusOK)
	defer srv.Close()

	dest := t.TempDir()
	meta := t.TempDir()
	secret := filepath.Join(t.TempDir(), "missing-secret")
	cache := filepath.Join(t.TempDir(), "hf-cache")
	withRunHFTestEnv(t, dest, meta, secret, cache)
	t.Setenv("HF_ENDPOINT", srv.URL)
	t.Setenv(envHFRepoID, repoID)
	t.Setenv(envHFTimeout, "30s")

	if err := runHF(); err != nil {
		t.Fatalf("runHF: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "data", "nested", "file.txt"))
	if err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
	if string(got) != "nested-content" {
		t.Fatalf("nested file = %q, want nested-content", got)
	}
}

func TestDestRelForHFSubPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repoFile string
		subPath  string
		want     string
	}{
		{repoFile: "README.md", subPath: "", want: "README.md"},
		{repoFile: "data.jsonl", subPath: "data.jsonl", want: "data.jsonl"},
		{repoFile: "staging_sub_path/data.txt", subPath: "staging_sub_path", want: "data.txt"},
	}
	for _, tt := range tests {
		got := destRelForHFSubPath(tt.repoFile, tt.subPath)
		if got != tt.want {
			t.Fatalf("destRelForHFSubPath(%q, %q) = %q, want %q", tt.repoFile, tt.subPath, got, tt.want)
		}
	}
}
