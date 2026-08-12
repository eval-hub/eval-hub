package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/eval-hub/eval-hub/internal/runtimeenv"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// validateGitCloneURL is overridable in tests so file:// local repos can exercise runGit.
var validateGitCloneURL = api.ValidateGitCloneURLResolved

// runGit clones a git repository into destDir using go-git (pure Go, no git binary required)
// and writes the resolved commit SHA to .git-metadata for the sidecar to report back.
func runGit() error {
	repoURL := strings.TrimSpace(os.Getenv(envGitURL))
	ref := strings.TrimSpace(os.Getenv(envGitRef))
	subPath := strings.TrimSpace(os.Getenv(envGitSubPath))

	if repoURL == "" {
		return fmt.Errorf("%s is required", envGitURL)
	}
	if err := validateGitCloneURL(repoURL, nil); err != nil {
		return fmt.Errorf("%s: %w", envGitURL, err)
	}
	if ref == "" {
		return fmt.Errorf("%s is required", envGitRef)
	}

	timeout := defaultTimeout
	if raw := strings.TrimSpace(os.Getenv(envGitTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", envGitTimeout, err)
		}
		timeout = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cloneDir, err := os.MkdirTemp("", "git-clone-*")
	if err != nil {
		return fmt.Errorf("create temp clone dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(cloneDir) }()

	auth, err := resolveGitAuth()
	if err != nil {
		return err
	}
	if err := api.ValidateGitCloneURLAuth(repoURL, auth != nil); err != nil {
		return fmt.Errorf("%s: %w", envGitURL, err)
	}

	// Re-validate every HTTP(S) redirect target so a public URL cannot bounce to a
	// blocked host/IP (or cleartext HTTP when credentials are in use).
	installSafeGitHTTPClient(auth != nil)

	slog.Info("cloning repository", "url", repoURL, "ref", ref)

	commitSHA, err := cloneRef(ctx, cloneDir, repoURL, ref, auth)
	if err != nil {
		return err
	}

	slog.Info("cloned", "commit_sha", commitSHA)

	cloneRoot, err := os.OpenRoot(cloneDir)
	if err != nil {
		return fmt.Errorf("open clone root: %w", err)
	}
	defer func() { _ = cloneRoot.Close() }()

	srcRel := "."
	if subPath != "" {
		cleanSub := filepath.Clean(filepath.FromSlash(subPath))
		if cleanSub == "." || !filepath.IsLocal(cleanSub) {
			return fmt.Errorf("sub_path escapes repository root: %q", subPath)
		}
		info, err := cloneRoot.Stat(cleanSub)
		if err != nil {
			return fmt.Errorf("sub_path %q not found in repository: %w", subPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("sub_path %q is not a directory", subPath)
		}
		srcRel = cleanSub
	}

	// Fail fast if the repository has submodules: go-git does not populate them,
	// so the cloned tree would contain empty stub directories. Use sub_path to
	// point at a subdirectory that does not require submodules.
	gitmodules := ".gitmodules"
	if srcRel != "." {
		gitmodules = filepath.Join(srcRel, ".gitmodules")
	}
	if _, err := cloneRoot.Stat(gitmodules); err == nil {
		return fmt.Errorf("repository contains submodules which are not supported; use sub_path to point to a directory that does not require submodules")
	}

	srcRoot := cloneRoot
	if srcRel != "." {
		srcRoot, err = cloneRoot.OpenRoot(srcRel)
		if err != nil {
			return fmt.Errorf("open sub_path root: %w", err)
		}
		defer func() { _ = srcRoot.Close() }()
	}

	if err := copyDirFromRoot(srcRoot, destDir); err != nil {
		return fmt.Errorf("copy to dest: %w", err)
	}

	// 0600: init and sidecar share the same pod UID (OpenShift SCC / runAsNonRoot).
	if err := writeGitMetadata(commitSHA); err != nil {
		return fmt.Errorf("write git metadata: %w", err)
	}

	slog.Info("git source ready", "dest", destDir, "commit_sha", commitSHA)
	return nil
}

// writeGitMetadata writes the resolved commit SHA under InitMetadataDir via os.Root
// so the path cannot escape the metadata directory.
func writeGitMetadata(commitSHA string) error {
	return writeGitMetadataTo(gitMetadataDir, commitSHA)
}

func writeGitMetadataTo(dir, commitSHA string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.WriteFile(filepath.Base(runtimeenv.GitMetadataFile), []byte(commitSHA+"\n"), 0o600)
}

// installSafeGitHTTPClient replaces go-git's default http/https transports with a client
// whose CheckRedirect re-runs SSRF and credential-scheme checks on every redirect target.
func installSafeGitHTTPClient(withCredentials bool) {
	safe := githttp.NewClient(newSafeGitHTTPClient(withCredentials))
	client.InstallProtocol("http", safe)
	client.InstallProtocol("https", safe)
}

// newSafeGitHTTPClient builds an http.Client that rejects unsafe redirect targets.
// lookup may be nil to use net.LookupIP (production); tests inject a stub via
// gitHTTPRedirectLookup.
func newSafeGitHTTPClient(withCredentials bool) *http.Client {
	return &http.Client{
		Transport:     http.DefaultTransport,
		CheckRedirect: gitHTTPCheckRedirect(withCredentials),
	}
}

// gitHTTPRedirectLookup is the DNS lookup used when validating redirect targets.
// Overridable in tests; nil means ValidateGitCloneURLResolved's default (net.LookupIP).
var gitHTTPRedirectLookup func(host string) ([]net.IP, error)

func gitHTTPCheckRedirect(withCredentials bool) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		target := req.URL.String()
		if err := api.ValidateGitCloneURLResolved(target, gitHTTPRedirectLookup); err != nil {
			return fmt.Errorf("redirect target blocked: %w", err)
		}
		if err := api.ValidateGitCloneURLAuth(target, withCredentials); err != nil {
			return fmt.Errorf("redirect target blocked: %w", err)
		}
		return nil
	}
}

// resolveGitAuth reads basic-auth credentials from the mounted secret dir.
// If the secret dir does not exist (no secretRef was configured), nil is returned
// for public-repo access. If the dir exists but "username" or "password" are missing
// or empty, an error is returned — a mounted secret without these keys is a misconfiguration.
func resolveGitAuth() (*githttp.BasicAuth, error) {
	// No secret dir → public repo; auth not required.
	if _, err := os.Stat(scrtDir); os.IsNotExist(err) {
		return nil, nil
	}

	username, err := readSecret("username")
	if err != nil {
		return nil, fmt.Errorf("git auth: %w", err)
	}
	password, err := readSecret("password")
	if err != nil {
		return nil, fmt.Errorf("git auth: %w", err)
	}

	return &githttp.BasicAuth{Username: username, Password: password}, nil
}

type refKind int

const (
	refKindBranch refKind = iota
	refKindTag
	refKindCommit
)

// errRefNotFound is returned by resolveRemoteRef when ref is not a branch or tag.
// The caller uses this to fall back to commit-SHA handling.
var errRefNotFound = fmt.Errorf("ref not found as branch or tag")

// resolveRemoteRef performs a lightweight ls-remote (no pack transfer) to determine
// whether ref is a branch or a tag. When both refs/heads/<ref> and refs/tags/<ref> exist,
// branch wins. Returns errRefNotFound when neither matches.
func resolveRemoteRef(ctx context.Context, repoURL, ref string, auth *githttp.BasicAuth) (refKind, error) {
	remote := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return refKindBranch, fmt.Errorf("ls-remote %q: %w", repoURL, err)
	}
	branchName := plumbing.NewBranchReferenceName(ref)
	tagName := plumbing.NewTagReferenceName(ref)
	foundTag := false
	for _, r := range refs {
		switch r.Name() {
		case branchName:
			return refKindBranch, nil // branch takes priority; stop scanning
		case tagName:
			foundTag = true
		}
	}
	if foundTag {
		return refKindTag, nil
	}
	return refKindBranch, errRefNotFound
}

// cloneRef resolves ref via ls-remote first. If the ref is not found as a branch or tag
// and it looks like a hex SHA, it falls back to a full clone + checkout. This ensures
// that a branch name that happens to be all-hex (e.g. "deadbeef") is handled correctly.
// Branches and tags use a shallow clone (depth=1); commit SHAs require a full clone.
func cloneRef(ctx context.Context, cloneDir, repoURL, ref string, auth *githttp.BasicAuth) (string, error) {
	kind, err := resolveRemoteRef(ctx, repoURL, ref, auth)
	if err != nil {
		if !errors.Is(err, errRefNotFound) {
			return "", err
		}
		// Not a branch or tag — treat as a commit SHA if it looks like one.
		if !api.LooksLikeHexSHA(ref) {
			return "", fmt.Errorf("ref %q not found as a branch or tag, and does not look like a commit SHA", ref)
		}
		kind = refKindCommit
	}

	var (
		repo     *git.Repository
		cloneErr error
	)

	var resolvedRef plumbing.ReferenceName

	switch kind {
	case refKindBranch, refKindTag:
		if kind == refKindTag {
			resolvedRef = plumbing.NewTagReferenceName(ref)
		} else {
			resolvedRef = plumbing.NewBranchReferenceName(ref)
		}
		slog.Info("shallow clone", "url", repoURL, "ref", ref, "kind", map[refKind]string{refKindBranch: "branch", refKindTag: "tag"}[kind])
		repo, cloneErr = git.PlainCloneContext(ctx, cloneDir, false, &git.CloneOptions{
			URL:           repoURL,
			Auth:          auth,
			Depth:         1,
			SingleBranch:  true,
			ReferenceName: resolvedRef,
			Progress:      os.Stdout,
		})
		if cloneErr != nil {
			return "", fmt.Errorf("shallow clone %q at %q: %w", repoURL, ref, cloneErr)
		}

	case refKindCommit:
		slog.Info("full clone for commit SHA", "url", repoURL, "sha", ref)
		repo, cloneErr = git.PlainCloneContext(ctx, cloneDir, false, &git.CloneOptions{
			URL:      repoURL,
			Auth:     auth,
			Progress: os.Stdout,
		})
		if cloneErr != nil {
			return "", fmt.Errorf("clone %q: %w", repoURL, cloneErr)
		}
		wt, err := repo.Worktree()
		if err != nil {
			return "", fmt.Errorf("get worktree: %w", err)
		}
		// ResolveRevision accepts full and abbreviated SHAs; NewHash only accepts full hashes.
		commitHash, err := repo.ResolveRevision(plumbing.Revision(ref))
		if err != nil {
			return "", fmt.Errorf("resolve commit %q: %w", ref, err)
		}
		if err := wt.Checkout(&git.CheckoutOptions{Hash: *commitHash, Force: true}); err != nil {
			return "", fmt.Errorf("checkout commit %q: %w", ref, err)
		}
		return commitHash.String(), nil
	}

	// For branches and tags, use ResolveRevision so annotated tags are dereferenced
	// to the underlying commit SHA rather than returning the tag object SHA.
	commitHash, err := repo.ResolveRevision(plumbing.Revision(resolvedRef.String()))
	if err != nil {
		return "", fmt.Errorf("resolve commit SHA for ref %q: %w", ref, err)
	}
	return commitHash.String(), nil
}
