package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// useGitHubAPI points the version client at an httptest server and restores the
// real endpoint when the test finishes.
func useGitHubAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	previous := gitHubAPIBaseURL
	gitHubAPIBaseURL = srv.URL
	t.Cleanup(func() { gitHubAPIBaseURL = previous })
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func TestLatestVersion(t *testing.T) {
	useGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/mlflow/mlflow/releases/latest" {
			http.NotFound(w, r)
			return
		}
		writeJSON(t, w, githubRelease{TagName: "v3.18.0"})
	})

	version, err := LatestVersion(t.Context())
	if err != nil {
		t.Fatalf("LatestVersion() err = %v", err)
	}
	if version != "3.18.0" {
		t.Fatalf("LatestVersion() = %q, want 3.18.0", version)
	}
}

func TestLatestVersionErrors(t *testing.T) {
	t.Run("non-200 response", func(t *testing.T) {
		useGitHubAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
		})
		if _, err := LatestVersion(t.Context()); err == nil {
			t.Fatal("expected an error for a 403 response")
		}
	})

	t.Run("malformed tag", func(t *testing.T) {
		useGitHubAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, githubRelease{TagName: "not-a-version"})
		})
		if _, err := LatestVersion(t.Context()); err == nil {
			t.Fatal("expected an error for a malformed tag_name")
		}
	})
}

func TestVersions(t *testing.T) {
	useGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/mlflow/mlflow/releases/latest":
			writeJSON(t, w, githubRelease{TagName: "v3.18.0"})
		case "/repos/mlflow/mlflow/releases":
			writeJSON(t, w, []githubRelease{
				{TagName: "v3.18.0"},
				{TagName: "v3.17.2"},
				{TagName: "v3.17.1"},
				{TagName: "v3.16.1", Prerelease: true},
				{TagName: "v3.16.0"},
				{TagName: "v3.15.2"},
				{TagName: "v3.15.0"},
				{TagName: "v3.10.1"},
				{TagName: "v3.10.0"},
				{TagName: "v3.9.0"},
				{TagName: "v3.8.1"},
				{TagName: "v3.8.0"},
				{TagName: "v3.7.0"},
				{TagName: "v3.19.0", Draft: true},
				{TagName: "model-catalog/latest"},
			})
		default:
			http.NotFound(w, r)
		}
	})

	versions, err := Versions(t.Context(), MinVersion)
	if err != nil {
		t.Fatalf("Versions() err = %v", err)
	}
	want := []string{"3.8.1", "3.9.0", "3.10.1", "3.15.2", "3.16.0", "3.17.2", "3.18.0"}
	if len(versions) != len(want) {
		t.Fatalf("Versions() = %v, want %v", versions, want)
	}
	for i := range want {
		if versions[i] != want[i] {
			t.Fatalf("Versions() = %v, want %v", versions, want)
		}
	}
}

func TestVersionsPaginates(t *testing.T) {
	pageOne := make([]githubRelease, releasesPerPage)
	for i := range pageOne {
		pageOne[i] = githubRelease{TagName: "v3.18.0"}
	}
	var pages []string

	useGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/mlflow/mlflow/releases/latest":
			writeJSON(t, w, githubRelease{TagName: "v3.18.0"})
		case "/repos/mlflow/mlflow/releases":
			pages = append(pages, r.URL.Query().Get("page"))
			if r.URL.Query().Get("page") == "1" {
				writeJSON(t, w, pageOne)
				return
			}
			writeJSON(t, w, []githubRelease{{TagName: "v3.9.0"}, {TagName: "v3.8.1"}})
		default:
			http.NotFound(w, r)
		}
	})

	versions, err := Versions(t.Context(), MinVersion)
	if err != nil {
		t.Fatalf("Versions() err = %v", err)
	}
	want := []string{"3.8.1", "3.9.0", "3.18.0"}
	if fmt.Sprint(versions) != fmt.Sprint(want) {
		t.Fatalf("Versions() = %v, want %v", versions, want)
	}
	if fmt.Sprint(pages) != fmt.Sprint([]string{"1", "2"}) {
		t.Fatalf("requested pages = %v, want [1 2]", pages)
	}
}

func TestVersionsRejectsLatestBelowMinimum(t *testing.T) {
	useGitHubAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, githubRelease{TagName: "v3.7.0"})
	})
	if _, err := Versions(t.Context(), MinVersion); err == nil {
		t.Fatal("expected an error when the latest release predates the minimum")
	}
}

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		version string
		minimum string
		want    bool
	}{
		{version: "3.8.1", minimum: "3.8.1", want: true},
		{version: "3.9.0", minimum: "3.8.1", want: true},
		{version: "v3.10.0", minimum: "3.10.0", want: true},
		{version: "3.7.9", minimum: "3.8.1", want: false},
		{version: "3.10", minimum: "3.9.0", want: false},
		{version: "not-a-version", minimum: "3.8.1", want: false},
		{version: "3.8.1", minimum: "invalid", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.version+" >= "+tc.minimum, func(t *testing.T) {
			if got := VersionAtLeast(tc.version, tc.minimum); got != tc.want {
				t.Fatalf("VersionAtLeast(%q, %q) = %t, want %t", tc.version, tc.minimum, got, tc.want)
			}
		})
	}
}

func TestFallbackVersions(t *testing.T) {
	versions := FallbackVersions()
	if len(versions) == 0 {
		t.Fatal("FallbackVersions() returned no versions")
	}
	if versions[0] != MinVersion {
		t.Fatalf("first fallback version = %q, want %q", versions[0], MinVersion)
	}
	if versions[len(versions)-1] != FallbackLatestVersion {
		t.Fatalf("last fallback version = %q, want %q", versions[len(versions)-1], FallbackLatestVersion)
	}

	seenMinor := make(map[minorKey]bool, len(versions))
	previous := version{major: -1}
	for _, raw := range versions {
		parsed, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("fallback version %q does not parse: %v", raw, err)
		}
		if parsed.less(previous) {
			t.Fatalf("fallback versions are not in ascending order: %v", versions)
		}
		key := minorKey{major: parsed.major, minor: parsed.minor}
		if seenMinor[key] {
			t.Fatalf("fallback versions contain more than one release for %d.%d: %v", parsed.major, parsed.minor, versions)
		}
		seenMinor[key] = true
		previous = parsed
	}

	if !VersionAtLeast(FallbackLatestVersion, WorkspacesMinVersion) {
		t.Fatalf("fallback latest %q must support workspaces", FallbackLatestVersion)
	}

	// Callers own the returned slice, so mutating it must not change the fallback.
	versions[0] = "0.0.0"
	if FallbackVersions()[0] != MinVersion {
		t.Fatal("FallbackVersions() returned a shared slice")
	}
}
