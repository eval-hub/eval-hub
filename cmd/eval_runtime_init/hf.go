package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gomlx/go-huggingface/hub"

	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	envHFRepoID   = "TEST_DATA_HF_REPO_ID"
	envHFRevision = "TEST_DATA_HF_REVISION"
	envHFSubPath  = "TEST_DATA_HF_SUBPATH"
	envHFTimeout  = "TEST_DATA_HF_TIMEOUT"
	hfTokenKey    = "token"
	// hfCacheDirName is the Hub cache directory under the test-data emptyDir volume.
	hfCacheDirName = ".hf-cache"

	terminationMessagePath = "/dev/termination-log"
)

// hfCacheDir overrides the Hub cache location when non-empty (unit tests only).
var hfCacheDir string

const (
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

	token, err := resolveHFToken()
	if err != nil {
		return failHF(err)
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

// resolveHFToken reads the Hugging Face token when secret_ref mounted the credentials
// volume. No secret dir means public access is allowed. A mounted dir without a valid
// token key is a misconfiguration (same contract as git auth).
func resolveHFToken() (string, error) {
	if _, err := os.Stat(scrtDir); os.IsNotExist(err) {
		return "", nil
	}
	token, err := readSecret(hfTokenKey)
	if err != nil {
		return "", fmt.Errorf("hugging face auth: %w", err)
	}
	return token, nil
}

func effectiveHFCacheDir() string {
	if strings.TrimSpace(hfCacheDir) != "" {
		return hfCacheDir
	}
	return filepath.Join(destDir, hfCacheDirName)
}

func downloadHFRepo(ctx context.Context, repoID, revision, subPath, token string) (string, error) {
	cachePath := effectiveHFCacheDir()
	if err := os.MkdirAll(cachePath, 0o750); err != nil {
		return "", fmt.Errorf("create hf cache dir: %w", err)
	}

	repo := hub.New(repoID).
		WithType(hub.RepoTypeDataset).
		WithCacheDir(cachePath).
		WithProgressBar(false)
	repo.Verbosity = 0

	if revision != "" {
		repo = repo.WithRevision(revision)
	}
	if token != "" {
		repo = repo.WithAuth(token)
	}

	slog.Info("resolving revision", "repo_id", repoID, "revision", revisionOrDefault(revision))

	authenticated := token != ""
	if err := downloadHFInfo(ctx, repo, false); err != nil {
		return "", classifyHFError(repoID, authenticated, err)
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
	if err := downloadHFInfo(ctx, repo, true); err != nil {
		return "", classifyHFError(repoID, authenticated, err)
	}

	slog.Info("downloading repository", "repo_id", repoID, "revision", commitSHA)

	var repoFiles []string
	for fileName, iterErr := range repo.IterFileNames() {
		if iterErr != nil {
			return "", classifyHFError(repoID, authenticated, iterErr)
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

	// Download files sequentially: go-huggingface v0.4.1 races on shared counters
	// inside DownloadFilesCtx when multiple files are passed at once.
	for _, fileName := range repoFiles {
		if _, err := repo.DownloadFileCtx(ctx, fileName); err != nil {
			return "", classifyHFError(repoID, authenticated, err)
		}
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

	if err := stageHFFiles(cacheDir, commitSHA, repoFiles, subPath, destDir); err != nil {
		return "", fmt.Errorf("stage repository: %w", err)
	}

	if err := cleanupHFCache(); err != nil {
		slog.Warn("failed to remove hf cache after staging", "error", err)
	}

	if !destHasData(destDir) {
		return "", fmt.Errorf("no files were staged under %s", destDir)
	}

	return commitSHA, nil
}

// downloadHFInfo wraps repo.DownloadInfo with context cancellation. go-huggingface
// v0.4.1 uses context.Background() internally for metadata downloads.
func downloadHFInfo(ctx context.Context, repo *hub.Repo, forceDownload bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- repo.DownloadInfo(forceDownload) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func revisionOrDefault(revision string) string {
	if revision != "" {
		return revision
	}
	return "default"
}

func stageHFFiles(cacheDir, commitSHA string, repoFiles []string, subPath, dst string) error {
	if err := clearDestDir(dst); err != nil {
		return err
	}
	cacheRoot, err := os.OpenRoot(cacheDir)
	if err != nil {
		return err
	}
	defer func() { _ = cacheRoot.Close() }()

	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstRoot.Close() }()

	for _, repoFile := range repoFiles {
		srcRel, err := resolveHFRepoFileRel(cacheDir, commitSHA, repoFile)
		if err != nil {
			return fmt.Errorf("read repository file %q: %w", repoFile, err)
		}
		destRel := destRelForHFSubPath(repoFile, subPath)
		if !filepath.IsLocal(destRel) {
			return fmt.Errorf("staged path %q escapes destination", destRel)
		}
		if dir := filepath.Dir(destRel); dir != "." {
			if err := dstRoot.MkdirAll(dir, 0o750); err != nil {
				return err
			}
		}
		if err := copyHFRepoFile(cacheRoot, dstRoot, srcRel, destRel); err != nil {
			return err
		}
	}
	return nil
}

func destRelForHFSubPath(repoFile, subPath string) string {
	slashFile := filepath.ToSlash(repoFile)
	normalized := strings.Trim(strings.TrimSpace(subPath), "/")
	if normalized == "" {
		return filepath.FromSlash(slashFile)
	}
	if slashFile == normalized {
		return filepath.Base(slashFile)
	}
	prefix := normalized + "/"
	if strings.HasPrefix(slashFile, prefix) {
		return filepath.FromSlash(strings.TrimPrefix(slashFile, prefix))
	}
	return filepath.FromSlash(slashFile)
}

func pathWithinBase(base, target string) bool {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if resolvedBase, err := filepath.EvalSymlinks(base); err == nil {
		base = resolvedBase
	}
	if resolvedTarget, err := filepath.EvalSymlinks(target); err == nil {
		target = resolvedTarget
	}
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// resolveHFRepoFileRel returns a root-confined path to a downloaded Hub file in
// cache, following symlinks that point at blob objects under the same cache root.
func resolveHFRepoFileRel(cacheDir, commitSHA, repoFile string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(repoFile))
	if !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("invalid repository file path %q", repoFile)
	}
	snapPath := filepath.Join(cacheDir, "snapshots", commitSHA, cleaned)
	if !pathWithinBase(cacheDir, snapPath) {
		return "", fmt.Errorf("repository file %q escapes cache root", repoFile)
	}
	resolved, err := filepath.EvalSymlinks(snapPath)
	if err != nil {
		return "", err
	}
	if !pathWithinBase(cacheDir, resolved) {
		return "", fmt.Errorf("repository file %q resolves outside cache root", repoFile)
	}
	cacheAbs := filepath.Clean(cacheDir)
	if resolvedCache, err := filepath.EvalSymlinks(cacheDir); err == nil {
		cacheAbs = resolvedCache
	}
	rel, err := filepath.Rel(cacheAbs, resolved)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("invalid cache path for %q", repoFile)
	}
	return rel, nil
}

func copyHFRepoFile(cacheRoot, dstRoot *os.Root, srcRel, dstRel string) error {
	in, err := cacheRoot.Open(srcRel)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := dstRoot.OpenFile(dstRel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
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
	trimmed := strings.TrimSpace(subPath)
	normalized := filepath.FromSlash(trimmed)
	clean := filepath.Clean(normalized)
	if clean != normalized {
		return fmt.Errorf("sub_path must be a canonical relative path: %q", subPath)
	}
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

func hfRepoNotFoundOrPrivateMessage(repoID string) error {
	return fmt.Errorf(
		"repository %s not found or is private; provide secret_ref with a Hugging Face token if the dataset is private or gated",
		repoID,
	)
}

func classifyHFError(repoID string, authenticated bool, err error) error {
	if err == nil {
		return nil
	}

	slog.Error("hugging face hub error", "repo_id", repoID, "error", err)

	lower := strings.ToLower(err.Error())
	// HF returns 401 for missing and private datasets without a token; 404 when the repo
	// is explicitly not found. Use one operator-facing message for both cases.
	switch {
	case isHFGatedRepoError(lower):
		return fmt.Errorf("repository %s is gated; provide secret_ref with a Hugging Face token", repoID)
	case strings.Contains(lower, "401"),
		strings.Contains(lower, "invalid username or password"):
		if authenticated {
			return fmt.Errorf("hugging face authentication failed for repository %s; check secret_ref token", repoID)
		}
		return hfRepoNotFoundOrPrivateMessage(repoID)
	case strings.Contains(lower, "404"),
		strings.Contains(lower, "repository not found"),
		strings.Contains(lower, "not found for url"):
		return hfRepoNotFoundOrPrivateMessage(repoID)
	default:
		if strings.Contains(lower, "huggingface.co") {
			return fmt.Errorf("failed to download repository %s from Hugging Face Hub", repoID)
		}
		return err
	}
}

func isHFGatedRepoError(lower string) bool {
	// Avoid matching huggingface_hub help text ("private or gated repo, make sure...").
	return strings.Contains(lower, "is gated") ||
		strings.Contains(lower, "gated dataset") ||
		(strings.Contains(lower, "gated") && !strings.Contains(lower, "repository not found"))
}

func cleanupHFCache() error {
	cache := effectiveHFCacheDir()
	if !pathWithinBase(destDir, cache) {
		return nil
	}
	return os.RemoveAll(cache)
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
		if entry.Name() == hfCacheDirName {
			continue
		}
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
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == hfCacheDirName {
			continue
		}
		return true
	}
	return false
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
