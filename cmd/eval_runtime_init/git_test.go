package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// newLocalBareRepo creates a bare git repo in dir with one commit on "main" and a tag "v1.0".
func newLocalBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Init a bare repo that can be cloned / ls-remote'd via file:// URL.
	bare, err := git.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}

	// We need a working tree to create a commit, so use a separate non-bare repo
	// and push to the bare one.
	wtDir := t.TempDir()
	wt, err := git.PlainInit(wtDir, false)
	if err != nil {
		t.Fatalf("PlainInit worktree: %v", err)
	}
	w, err := wt.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	// Write a file and commit.
	if err := os.WriteFile(filepath.Join(wtDir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(wtDir, "datasets", "lm-eval"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "datasets", "lm-eval", "data.txt"), []byte("samples"), 0o644); err != nil {
		t.Fatalf("WriteFile data: %v", err)
	}
	if _, err := w.Add("README.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := w.Add("datasets/lm-eval/data.txt"); err != nil {
		t.Fatalf("Add data: %v", err)
	}
	hash, err := w.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com"},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Create a tag.
	if _, err := wt.CreateTag("v1.0", hash, nil); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	// Push main + tags to bare.
	if _, err := wt.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{dir},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	if err := wt.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitconfig.RefSpec{
			// Push to master so the bare repo's default HEAD (refs/heads/master) is valid.
			// This matters for full-clone paths that rely on HEAD resolution.
			"refs/heads/master:refs/heads/master",
			"refs/tags/*:refs/tags/*",
		},
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	_ = bare
	return dir
}

// newLocalBareRepoWithAnnotatedTag creates a bare repo with one commit and an annotated tag
// pointing at it. An annotated tag has a tag object SHA distinct from the commit SHA.
func newLocalBareRepoWithAnnotatedTag(t *testing.T) (repoDir string, commitSHA string) {
	t.Helper()
	dir := t.TempDir()
	bare, err := git.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	_ = bare

	wtDir := t.TempDir()
	wt, err := git.PlainInit(wtDir, false)
	if err != nil {
		t.Fatalf("PlainInit worktree: %v", err)
	}
	w, err := wt.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := w.Add("README.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hash, err := w.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com"},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// CreateTag with a non-nil TagObject creates an annotated tag.
	if _, err := wt.CreateTag("v2.0", hash, &git.CreateTagOptions{
		Tagger:  &object.Signature{Name: "test", Email: "t@t.com"},
		Message: "release v2.0",
	}); err != nil {
		t.Fatalf("CreateTag annotated: %v", err)
	}
	if _, err := wt.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{dir},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	if err := wt.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitconfig.RefSpec{
			"refs/heads/master:refs/heads/master",
			"refs/tags/*:refs/tags/*",
		},
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	return dir, hash.String()
}

func TestResolveRemoteRef(t *testing.T) {
	t.Parallel()
	repoDir := newLocalBareRepo(t)
	repoURL := "file://" + repoDir

	t.Run("branch", func(t *testing.T) {
		t.Parallel()
		kind, err := resolveRemoteRef(context.Background(), repoURL, "master", nil)
		if err != nil {
			t.Fatalf("resolveRemoteRef(master) error: %v", err)
		}
		if kind != refKindBranch {
			t.Errorf("resolveRemoteRef(master) = %v, want refKindBranch", kind)
		}
	})

	t.Run("tag", func(t *testing.T) {
		t.Parallel()
		kind, err := resolveRemoteRef(context.Background(), repoURL, "v1.0", nil)
		if err != nil {
			t.Fatalf("resolveRemoteRef(v1.0) error: %v", err)
		}
		if kind != refKindTag {
			t.Errorf("resolveRemoteRef(v1.0) = %v, want refKindTag", kind)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		_, err := resolveRemoteRef(context.Background(), repoURL, "nonexistent", nil)
		if !errors.Is(err, errRefNotFound) {
			t.Errorf("resolveRemoteRef(nonexistent) error = %v, want errRefNotFound", err)
		}
	})

	t.Run("branch beats tag with same name", func(t *testing.T) {
		t.Parallel()
		// Create a repo where both refs/heads/ambiguous and refs/tags/ambiguous exist.
		dir := t.TempDir()
		bare, err := git.PlainInit(dir, true)
		if err != nil {
			t.Fatalf("PlainInit bare: %v", err)
		}
		_ = bare

		wtDir := t.TempDir()
		wt, err := git.PlainInit(wtDir, false)
		if err != nil {
			t.Fatalf("PlainInit worktree: %v", err)
		}
		w, err := wt.Worktree()
		if err != nil {
			t.Fatalf("Worktree: %v", err)
		}
		if err := os.WriteFile(filepath.Join(wtDir, "f"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if _, err := w.Add("f"); err != nil {
			t.Fatalf("Add: %v", err)
		}
		hash, err := w.Commit("init", &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t.com"},
		})
		if err != nil {
			t.Fatalf("Commit: %v", err)
		}
		// Create both a branch and a tag named "ambiguous".
		if _, err := wt.CreateTag("ambiguous", hash, nil); err != nil {
			t.Fatalf("CreateTag ambiguous: %v", err)
		}
		if _, err := wt.CreateRemote(&gitconfig.RemoteConfig{
			Name: "origin",
			URLs: []string{dir},
		}); err != nil {
			t.Fatalf("CreateRemote: %v", err)
		}
		if err := wt.Push(&git.PushOptions{
			RemoteName: "origin",
			RefSpecs: []gitconfig.RefSpec{
				"refs/heads/master:refs/heads/ambiguous",
				"refs/tags/ambiguous:refs/tags/ambiguous",
			},
		}); err != nil {
			t.Fatalf("Push: %v", err)
		}

		kind, err := resolveRemoteRef(context.Background(), "file://"+dir, "ambiguous", nil)
		if err != nil {
			t.Fatalf("resolveRemoteRef(ambiguous) error: %v", err)
		}
		if kind != refKindBranch {
			t.Errorf("resolveRemoteRef(ambiguous) = %v, want refKindBranch (branch takes priority over tag)", kind)
		}
	})
}

func TestCloneRefBranch(t *testing.T) {
	t.Parallel()
	repoDir := newLocalBareRepo(t)
	repoURL := "file://" + repoDir
	cloneDir := t.TempDir()

	sha, err := cloneRef(context.Background(), cloneDir, repoURL, "master", nil)
	if err != nil {
		t.Fatalf("cloneRef(master) error: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("cloneRef(master) SHA = %q, want 40-char hex", sha)
	}
	// Verify the README is present.
	if _, err := os.Stat(filepath.Join(cloneDir, "README.md")); err != nil {
		t.Errorf("README.md not found after clone: %v", err)
	}
}

func TestCloneRefTag(t *testing.T) {
	t.Parallel()
	repoDir := newLocalBareRepo(t)
	repoURL := "file://" + repoDir
	cloneDir := t.TempDir()

	sha, err := cloneRef(context.Background(), cloneDir, repoURL, "v1.0", nil)
	if err != nil {
		t.Fatalf("cloneRef(v1.0) error: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("cloneRef(v1.0) SHA = %q, want 40-char hex", sha)
	}
}

func TestCloneRefCommitSHA(t *testing.T) {
	t.Parallel()
	repoDir := newLocalBareRepo(t)
	repoURL := "file://" + repoDir

	// First clone to get the SHA.
	tmpDir := t.TempDir()
	sha, err := cloneRef(context.Background(), tmpDir, repoURL, "master", nil)
	if err != nil {
		t.Fatalf("initial clone error: %v", err)
	}

	// Now clone by that commit SHA.
	cloneDir := t.TempDir()
	gotSHA, err := cloneRef(context.Background(), cloneDir, repoURL, sha, nil)
	if err != nil {
		t.Fatalf("cloneRef(SHA) error: %v", err)
	}
	if gotSHA != sha {
		t.Errorf("cloneRef(SHA) = %q, want %q", gotSHA, sha)
	}
}

func TestCloneRefAbbreviatedSHA(t *testing.T) {
	t.Parallel()
	repoDir := newLocalBareRepo(t)
	repoURL := "file://" + repoDir

	tmpDir := t.TempDir()
	sha, err := cloneRef(context.Background(), tmpDir, repoURL, "master", nil)
	if err != nil {
		t.Fatalf("initial clone error: %v", err)
	}
	abbrev := sha[:7]

	cloneDir := t.TempDir()
	gotSHA, err := cloneRef(context.Background(), cloneDir, repoURL, abbrev, nil)
	if err != nil {
		t.Fatalf("cloneRef(abbrev %q) error: %v", abbrev, err)
	}
	if gotSHA != sha {
		t.Errorf("cloneRef(abbrev %q) = %q, want full SHA %q", abbrev, gotSHA, sha)
	}
}

func TestCloneRefAnnotatedTag_ReturnsCommitSHA(t *testing.T) {
	t.Parallel()
	repoDir, wantSHA := newLocalBareRepoWithAnnotatedTag(t)
	repoURL := "file://" + repoDir
	cloneDir := t.TempDir()

	gotSHA, err := cloneRef(context.Background(), cloneDir, repoURL, "v2.0", nil)
	if err != nil {
		t.Fatalf("cloneRef(v2.0) error: %v", err)
	}
	if len(gotSHA) != 40 {
		t.Errorf("cloneRef(v2.0) SHA = %q, want 40-char hex", gotSHA)
	}
	// The returned SHA must be the commit SHA, not the tag object SHA.
	if gotSHA != wantSHA {
		t.Errorf("cloneRef(v2.0) SHA = %q, want commit SHA %q (annotated tag must be dereferenced)", gotSHA, wantSHA)
	}
	// Sanity: verify wantSHA looks like a commit (40 hex chars).
	_ = plumbing.NewHash(wantSHA)
}

func TestReadSecret_MissingKey(t *testing.T) {
	// scrtDir defaults to the real mount path which does not exist in unit tests.
	_, err := readSecret("no-such-key")
	if err == nil {
		t.Fatal("expected error for missing secret key file")
	}
	if !strings.Contains(err.Error(), "no-such-key") {
		t.Errorf("error should mention key name, got: %v", err)
	}
}

func TestReadSecret_PathTraversalRejected(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"../escape", "a/b", ".", "/"} {
		_, err := readSecret(key)
		if err == nil {
			t.Errorf("readSecret(%q) = nil, want error for path traversal", key)
		}
		if err != nil && !strings.Contains(err.Error(), "path separators") {
			t.Errorf("readSecret(%q) error = %v, want mention of path separators", key, err)
		}
	}
}

func TestReadSecret_EmptyValueRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "username"), []byte("   "), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := scrtDir
	scrtDir = dir
	t.Cleanup(func() { scrtDir = orig })

	_, err := readSecret("username")
	if err == nil {
		t.Fatal("expected error for empty secret value")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %v, want mention of empty", err)
	}
}

func TestWriteGitMetadataTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const sha = "abcdef0123456789"
	if err := writeGitMetadataTo(dir, sha); err != nil {
		t.Fatalf("writeGitMetadataTo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".git-metadata"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if strings.TrimSpace(string(got)) != sha {
		t.Errorf("metadata = %q, want %q", strings.TrimSpace(string(got)), sha)
	}
}

func TestCopyDirFromRoot_SkipsDotGitAndCopiesFiles(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "data"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data", "file.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".git", "objects"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "objects", "pack"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	if err := copyDirFromRoot(root, dst); err != nil {
		t.Fatalf("copyDirFromRoot: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "data", "file.txt"))
	if err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("copied content = %q, want hello", got)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git should not be copied, stat err = %v", err)
	}
}

func withRunGitTestEnv(t *testing.T, dest, meta, secret string) {
	t.Helper()
	origDest, origMeta, origSecret, origValidate := destDir, gitMetadataDir, scrtDir, validateGitCloneURL
	destDir, gitMetadataDir, scrtDir = dest, meta, secret
	validateGitCloneURL = func(string, func(string) ([]net.IP, error)) error { return nil }
	t.Cleanup(func() {
		destDir, gitMetadataDir, scrtDir, validateGitCloneURL = origDest, origMeta, origSecret, origValidate
		_ = os.Unsetenv(envGitURL)
		_ = os.Unsetenv(envGitRef)
		_ = os.Unsetenv(envGitSubPath)
		_ = os.Unsetenv(envGitTimeout)
	})
}

func TestRunGit_HappyPathAndSubPath(t *testing.T) {
	repoDir := newLocalBareRepo(t)
	dest := t.TempDir()
	meta := t.TempDir()
	secret := filepath.Join(t.TempDir(), "missing-secret")
	withRunGitTestEnv(t, dest, meta, secret)

	t.Setenv(envGitURL, "file://"+repoDir)
	t.Setenv(envGitRef, "master")
	if err := runGit(); err != nil {
		t.Fatalf("runGit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("README.md missing after clone: %v", err)
	}
	sha, err := os.ReadFile(filepath.Join(meta, ".git-metadata"))
	if err != nil {
		t.Fatalf("metadata missing: %v", err)
	}
	if len(strings.TrimSpace(string(sha))) != 40 {
		t.Errorf("metadata SHA = %q, want 40 hex chars", strings.TrimSpace(string(sha)))
	}

	// sub_path: only datasets/lm-eval content under dest
	dest2 := t.TempDir()
	meta2 := t.TempDir()
	withRunGitTestEnv(t, dest2, meta2, secret)
	t.Setenv(envGitURL, "file://"+repoDir)
	t.Setenv(envGitRef, "master")
	t.Setenv(envGitSubPath, "datasets/lm-eval")
	if err := runGit(); err != nil {
		t.Fatalf("runGit with sub_path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest2, "data.txt")); err != nil {
		t.Fatalf("sub_path data.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest2, "README.md")); !os.IsNotExist(err) {
		t.Error("README.md should not appear when using sub_path")
	}
}

func TestRunGit_ValidationErrors(t *testing.T) {
	dest := t.TempDir()
	meta := t.TempDir()
	secret := filepath.Join(t.TempDir(), "missing-secret")
	withRunGitTestEnv(t, dest, meta, secret)

	t.Run("missing url", func(t *testing.T) {
		_ = os.Unsetenv(envGitURL)
		t.Setenv(envGitRef, "master")
		if err := runGit(); err == nil {
			t.Fatal("expected error for missing URL")
		}
	})
	t.Run("missing ref", func(t *testing.T) {
		t.Setenv(envGitURL, "https://example.com/repo.git")
		_ = os.Unsetenv(envGitRef)
		if err := runGit(); err == nil {
			t.Fatal("expected error for missing ref")
		}
	})
	t.Run("invalid timeout", func(t *testing.T) {
		t.Setenv(envGitURL, "https://example.com/repo.git")
		t.Setenv(envGitRef, "master")
		t.Setenv(envGitTimeout, "not-a-duration")
		if err := runGit(); err == nil {
			t.Fatal("expected error for invalid timeout")
		}
	})
	t.Run("url validation failure", func(t *testing.T) {
		validateGitCloneURL = api.ValidateGitCloneURLResolved
		t.Cleanup(func() {
			validateGitCloneURL = func(string, func(string) ([]net.IP, error)) error { return nil }
		})
		t.Setenv(envGitURL, "https://localhost/repo.git")
		t.Setenv(envGitRef, "master")
		_ = os.Unsetenv(envGitTimeout)
		if err := runGit(); err == nil {
			t.Fatal("expected SSRF validation error")
		}
	})
	t.Run("sub_path escape", func(t *testing.T) {
		repoDir := newLocalBareRepo(t)
		t.Setenv(envGitURL, "file://"+repoDir)
		t.Setenv(envGitRef, "master")
		t.Setenv(envGitSubPath, "../escape")
		if err := runGit(); err == nil {
			t.Fatal("expected sub_path escape error")
		}
	})
	t.Run("sub_path missing", func(t *testing.T) {
		repoDir := newLocalBareRepo(t)
		t.Setenv(envGitURL, "file://"+repoDir)
		t.Setenv(envGitRef, "master")
		t.Setenv(envGitSubPath, "does-not-exist")
		if err := runGit(); err == nil {
			t.Fatal("expected missing sub_path error")
		}
	})
	t.Run("submodules rejected", func(t *testing.T) {
		repoDir := newLocalBareRepo(t)
		// Clone once into a temp worktree, add .gitmodules, push — easier: create non-bare with .gitmodules and push.
		wtDir := t.TempDir()
		wt, err := git.PlainClone(wtDir, false, &git.CloneOptions{URL: "file://" + repoDir})
		if err != nil {
			t.Fatalf("clone for submodule fixture: %v", err)
		}
		w, err := wt.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wtDir, ".gitmodules"), []byte("[submodule \"x\"]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Add(".gitmodules"); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Commit("add gitmodules", &git.CommitOptions{
			Author: &object.Signature{Name: "test", Email: "t@t.com"},
		}); err != nil {
			t.Fatal(err)
		}
		if err := wt.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
			t.Fatal(err)
		}
		t.Setenv(envGitURL, "file://"+repoDir)
		t.Setenv(envGitRef, "master")
		_ = os.Unsetenv(envGitSubPath)
		if err := runGit(); err == nil || !strings.Contains(err.Error(), "submodules") {
			t.Fatalf("expected submodules error, got %v", err)
		}
	})
}

func TestResolveGitAuth_WithSecretDir(t *testing.T) {
	secret := t.TempDir()
	if err := os.WriteFile(filepath.Join(secret, "username"), []byte("u\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "password"), []byte("p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := scrtDir
	scrtDir = secret
	t.Cleanup(func() { scrtDir = orig })

	auth, err := resolveGitAuth()
	if err != nil {
		t.Fatalf("resolveGitAuth: %v", err)
	}
	if auth == nil || auth.Username != "u" || auth.Password != "p" {
		t.Fatalf("auth = %+v, want u/p", auth)
	}
}

func TestResolveGitAuth_MissingPassword(t *testing.T) {
	secret := t.TempDir()
	if err := os.WriteFile(filepath.Join(secret, "username"), []byte("u\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := scrtDir
	scrtDir = secret
	t.Cleanup(func() { scrtDir = orig })

	if _, err := resolveGitAuth(); err == nil {
		t.Fatal("expected error when password key missing")
	}
}

func TestWriteGitMetadata_UsesPackageDir(t *testing.T) {
	dir := t.TempDir()
	orig := gitMetadataDir
	gitMetadataDir = dir
	t.Cleanup(func() { gitMetadataDir = orig })
	if err := writeGitMetadata("abc123"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".git-metadata"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "abc123" {
		t.Errorf("got %q", got)
	}
}

func TestReadSecret_ViaOpenRoot(t *testing.T) {
	secret := t.TempDir()
	if err := os.WriteFile(filepath.Join(secret, "AWS_ACCESS_KEY_ID"), []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := scrtDir
	scrtDir = secret
	t.Cleanup(func() { scrtDir = orig })
	got, err := readSecret("AWS_ACCESS_KEY_ID")
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if got != "key" {
		t.Errorf("readSecret = %q, want key", got)
	}
	if _, err := readSecret("../escape"); err == nil {
		t.Error("traversal should return error")
	}
}

func TestGitHTTPCheckRedirect(t *testing.T) {
	orig := gitHTTPRedirectLookup
	gitHTTPRedirectLookup = func(host string) ([]net.IP, error) {
		switch host {
		case "ok.example.com":
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		case "evil.example.com":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		default:
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	}
	t.Cleanup(func() { gitHTTPRedirectLookup = orig })

	t.Run("allows safe https redirect", func(t *testing.T) {
		check := gitHTTPCheckRedirect(false)
		req, err := http.NewRequest(http.MethodGet, "https://ok.example.com/repo.git", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := check(req, []*http.Request{{}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("blocks redirect to private IP host", func(t *testing.T) {
		check := gitHTTPCheckRedirect(false)
		req, err := http.NewRequest(http.MethodGet, "https://evil.example.com/repo.git", nil)
		if err != nil {
			t.Fatal(err)
		}
		err = check(req, []*http.Request{{}})
		if err == nil || !strings.Contains(err.Error(), "redirect target blocked") {
			t.Fatalf("got %v, want redirect target blocked", err)
		}
	})

	t.Run("blocks literal private IP redirect", func(t *testing.T) {
		check := gitHTTPCheckRedirect(false)
		req, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest", nil)
		if err != nil {
			t.Fatal(err)
		}
		err = check(req, []*http.Request{{}})
		if err == nil || !strings.Contains(err.Error(), "redirect target blocked") {
			t.Fatalf("got %v, want redirect target blocked", err)
		}
	})

	t.Run("blocks http redirect when credentials are used", func(t *testing.T) {
		check := gitHTTPCheckRedirect(true)
		req, err := http.NewRequest(http.MethodGet, "http://ok.example.com/repo.git", nil)
		if err != nil {
			t.Fatal(err)
		}
		err = check(req, []*http.Request{{}})
		if err == nil || !strings.Contains(err.Error(), "must use https") {
			t.Fatalf("got %v, want https required with credentials", err)
		}
	})
}
