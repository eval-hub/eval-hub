package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gomlx/go-huggingface/hub"

	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	envHFRepoID    = "TEST_DATA_HF_REPO_ID"
	envHFRevision  = "TEST_DATA_HF_REVISION"
	envHFSubPath   = "TEST_DATA_HF_SUBPATH"
	envHFTimeout   = "TEST_DATA_HF_TIMEOUT"
	hfTokenKey     = "token"
	defaultHFCache = "/tmp/huggingface/hub"

	terminationMessagePath     = "/dev/termination-log"
	maxTerminationMessageBytes = 4096
)

// runHF downloads a Hugging Face Hub dataset repository into destDir and writes the
// resolved commit SHA to init metadata for the sidecar (same contract as git init).
func runHF() error {
	repoID := strings.TrimSpace(os.Getenv(envHFRepoID))
	if repoID == "" {
		return failHF(fmt.Errorf("%s is required", envHFRepoID))
	}

	revision := strings.TrimSpace(os.Getenv(envHFRevision))
	subPath := strings.TrimSpace(os.Getenv(envHFSubPath))
	if subPath != "" {
		if err := validateHFSubPath(subPath); err != nil {
			return failHF(err)
		}
	}

	timeout := defaultTimeout
	if raw := strings.TrimSpace(os.Getenv(envHFTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return failHF(fmt.Errorf("invalid %s: %w", envHFTimeout, err))
		}
		if parsed <= 0 {
			return failHF(fmt.Errorf("invalid %s: must be a positive duration", envHFTimeout))
		}
		timeout = parsed
	}

	token, err := readOptionalSecret(hfTokenKey)
	if err != nil {
		return failHF(fmt.Errorf("read hugging face token: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	commitSHA, err := downloadHFRepo(ctx, repoID, revision, subPath, token)
	if err != nil {
		return failHF(err)
	}

	if err := writeGitMetadata(commitSHA); err != nil {
		return failHF(fmt.Errorf("write git metadata: %w", err))
	}

	slog.Info("hf source ready", "dest", destDir, "commit_sha", commitSHA)
	return nil
}

func downloadHFRepo(ctx context.Context, repoID, revision, subPath, token string) (string, error) {
	repo := hub.New(repoID).
		WithType(hub.RepoTypeDataset).
		WithCacheDir(defaultHFCache).
		WithProgressBar(false)
	repo.Verbosity = 0

	if revision != "" {
		repo = repo.WithRevision(revision)
	}
	if token != "" {
		repo = repo.WithAuth(token)
	}

	slog.Info("resolving revision", "repo_id", repoID, "revision", revisionOrDefault(revision))

	if err := repo.DownloadInfo(false); err != nil {
		return "", classifyHFError(repoID, err)
	}
	info := repo.Info()
	if info == nil || strings.TrimSpace(info.CommitHash) == "" {
		return "", fmt.Errorf("could not resolve commit SHA for %s", repoID)
	}
	commitSHA := strings.TrimSpace(info.CommitHash)
	if !api.LooksLikeHexSHA(commitSHA) {
		return "", fmt.Errorf("invalid commit SHA from hub: %q", commitSHA)
	}

	// Pin downloads to the resolved commit SHA (same as huggingface_hub snapshot_download).
	repo = repo.WithRevision(commitSHA)
	if err := repo.DownloadInfo(true); err != nil {
		return "", classifyHFError(repoID, err)
	}

	slog.Info("downloading repository", "repo_id", repoID, "revision", commitSHA)

	var repoFiles []string
	for fileName, iterErr := range repo.IterFileNames() {
		if iterErr != nil {
			return "", classifyHFError(repoID, iterErr)
		}
		if subPath != "" && !fileMatchesSubPath(fileName, subPath) {
			continue
		}
		repoFiles = append(repoFiles, fileName)
	}
	if len(repoFiles) == 0 {
		if subPath != "" {
			return "", fmt.Errorf("sub_path %q not found in repository", subPath)
		}
		return "", fmt.Errorf("no files found in repository %s", repoID)
	}

	if _, err := repo.DownloadFilesCtx(ctx, repoFiles...); err != nil {
		return "", classifyHFError(repoID, err)
	}

	cacheDir, err := repo.CacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve hf cache dir: %w", err)
	}
	cacheRoot, err := os.OpenRoot(cacheDir)
	if err != nil {
		return "", fmt.Errorf("open hf cache dir: %w", err)
	}
	defer func() { _ = cacheRoot.Close() }()

	snapshotRel := filepath.Join("snapshots", commitSHA)
	snapshotInfo, err := cacheRoot.Stat(snapshotRel)
	if err != nil {
		return "", fmt.Errorf("snapshot directory not found after download: %w", err)
	}
	if !snapshotInfo.IsDir() {
		return "", fmt.Errorf("snapshot path %q is not a directory", snapshotRel)
	}

	snapshotRoot, err := cacheRoot.OpenRoot(snapshotRel)
	if err != nil {
		return "", fmt.Errorf("open snapshot root: %w", err)
	}
	defer func() { _ = snapshotRoot.Close() }()

	if err := clearDestDir(destDir); err != nil {
		return "", fmt.Errorf("prepare dest dir: %w", err)
	}

	if subPath != "" {
		if err := stageHFSubPath(snapshotRoot, subPath, destDir); err != nil {
			return "", err
		}
	} else if err := copyDirFromRoot(snapshotRoot, destDir); err != nil {
		return "", fmt.Errorf("stage repository: %w", err)
	}

	if !destHasData(destDir) {
		return "", fmt.Errorf("no files were staged under %s", destDir)
	}

	return commitSHA, nil
}

func revisionOrDefault(revision string) string {
	if revision != "" {
		return revision
	}
	return "default"
}

func stageHFSubPath(snapshotRoot *os.Root, subPath, dst string) error {
	cleanSub := filepath.Clean(filepath.FromSlash(subPath))
	info, err := snapshotRoot.Stat(cleanSub)
	if err != nil {
		return fmt.Errorf("sub_path %q not found in repository: %w", subPath, err)
	}

	if info.IsDir() {
		subRoot, err := snapshotRoot.OpenRoot(cleanSub)
		if err != nil {
			return fmt.Errorf("open sub_path root: %w", err)
		}
		defer func() { _ = subRoot.Close() }()
		return copyDirFromRoot(subRoot, dst)
	}

	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstRoot.Close() }()
	return copyFileBetweenRoots(snapshotRoot, dstRoot, cleanSub)
}

func validateHFSubPath(subPath string) error {
	clean := filepath.Clean(filepath.FromSlash(subPath))
	if clean == "." || !filepath.IsLocal(clean) {
		return fmt.Errorf("sub_path escapes repository root: %q", subPath)
	}
	return nil
}

func fileMatchesSubPath(fileName, subPath string) bool {
	normalized := strings.Trim(strings.TrimSpace(subPath), "/")
	if normalized == "" {
		return true
	}
	fileName = strings.TrimPrefix(filepath.ToSlash(fileName), "./")
	return fileName == normalized || strings.HasPrefix(fileName, normalized+"/")
}

func classifyHFError(repoID string, err error) error {
	if err == nil {
		return nil
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "401"), strings.Contains(lower, "gated"):
		return fmt.Errorf("repository %s is gated; provide secret_ref with a Hugging Face token", repoID)
	case strings.Contains(lower, "404"), strings.Contains(lower, "not found"):
		return fmt.Errorf("repository not found: %v", err)
	default:
		return err
	}
}

func clearDestDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func destHasData(root string) bool {
	entries, err := os.ReadDir(root)
	return err == nil && len(entries) > 0
}

func failHF(err error) error {
	slog.Error("hf init failed", "error", err)
	writeTerminationMessage(err.Error())
	return err
}

func writeTerminationMessage(message string) {
	text := strings.TrimSpace(message)
	if text == "" {
		return
	}
	encoded := []byte(text)
	if len(encoded) > maxTerminationMessageBytes {
		encoded = encoded[:maxTerminationMessageBytes]
	}
	if writeErr := writeTerminationMessageFile(encoded); writeErr != nil {
		slog.Warn("failed to write termination message", "error", writeErr)
	}
}

// writeTerminationMessageFile writes via os.Root so TERMINATION_MESSAGE_PATH cannot escape its directory.
func writeTerminationMessageFile(content []byte) error {
	path := strings.TrimSpace(os.Getenv("TERMINATION_MESSAGE_PATH"))
	if path == "" {
		path = terminationMessagePath
	}
	clean := filepath.Clean(path)
	dir, name := filepath.Split(clean)
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid termination message path %q", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.WriteFile(name, append(content, '\n'), 0o600)
}
