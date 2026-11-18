package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// MinVersion is the oldest MLflow release exercised by the integration tests.
	MinVersion = "3.8.1"

	// WorkspacesMinVersion is the first MLflow release whose `mlflow server`
	// command accepts --enable-workspaces. Older servers reject the flag and
	// exit during startup, so workspace cases only start at this version.
	WorkspacesMinVersion = "3.10.0"

	// FallbackLatestVersion is the newest MLflow release used when the GitHub
	// API cannot be reached. Keep it in sync with the newest entry in
	// fallbackVersions and with MLFLOW_LATEST_VERSION_FALLBACK in
	// tests/mlflow/Makefile.
	FallbackLatestVersion = "3.18.0"

	releasesPerPage = 100
	maxReleasePages = 10
	requestTimeout  = 10 * time.Second
	errorBodyLimit  = 512
)

// gitHubAPIBaseURL is a variable so tests can point the client at an httptest
// server. In production it resolves
// https://api.github.com/repos/mlflow/mlflow/releases/latest.
var gitHubAPIBaseURL = "https://api.github.com"

// fallbackVersions pins one release per MLflow minor line from MinVersion to
// FallbackLatestVersion. It is used when the GitHub API is unreachable
// (offline, CI, rate limited), mirroring the Makefile fallback.
var fallbackVersions = []string{
	"3.8.1",
	"3.9.0",
	"3.10.1",
	"3.11.1",
	"3.12.0",
	"3.13.0",
	"3.14.0",
	"3.15.2",
	"3.16.1",
	"3.17.2",
	"3.18.0",
}

// FallbackVersions returns the pinned version list used when the GitHub API is
// unavailable. The caller owns the returned slice.
func FallbackVersions() []string {
	return append([]string(nil), fallbackVersions...)
}

// LatestVersion returns the newest stable MLflow version (for example "3.18.0")
// from https://api.github.com/repos/mlflow/mlflow/releases/latest.
func LatestVersion(ctx context.Context) (string, error) {
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return "", err
	}
	parsed, err := parseVersion(release.TagName)
	if err != nil {
		return "", fmt.Errorf("parse latest MLflow release tag %q: %w", release.TagName, err)
	}
	return parsed.String(), nil
}

// Versions returns one stable release per MLflow minor line from minVersion up
// to the latest stable GitHub release, using the newest patch of each line
// (for example 3.15.2 rather than 3.15.0). Prereleases and drafts are skipped.
// The result is sorted in ascending version order.
func Versions(ctx context.Context, minVersion string) ([]string, error) {
	minimum, err := parseVersion(minVersion)
	if err != nil {
		return nil, fmt.Errorf("parse minimum MLflow version %q: %w", minVersion, err)
	}
	latest, err := LatestVersion(ctx)
	if err != nil {
		return nil, err
	}
	latestParsed, err := parseVersion(latest)
	if err != nil {
		return nil, fmt.Errorf("parse latest MLflow version %q: %w", latest, err)
	}
	if latestParsed.less(minimum) {
		return nil, fmt.Errorf("latest MLflow version %s is older than minimum %s", latest, minVersion)
	}

	newestPerMinor, err := fetchNewestPerMinor(ctx, minimum, latestParsed)
	if err != nil {
		return nil, err
	}
	// The latest release is the upper bound, so a partial or paginated
	// response must never drop it from the matrix.
	newestPerMinor[minorKey{major: latestParsed.major, minor: latestParsed.minor}] = latestParsed

	versions := make([]version, 0, len(newestPerMinor))
	for _, v := range newestPerMinor {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].less(versions[j]) })

	result := make([]string, len(versions))
	for i, v := range versions {
		result[i] = v.String()
	}
	return result, nil
}

// VersionAtLeast reports whether version is greater than or equal to minimum.
// Versions that are not in MAJOR.MINOR.PATCH form report false.
func VersionAtLeast(version, minimum string) bool {
	v, err := parseVersion(version)
	if err != nil {
		return false
	}
	min, err := parseVersion(minimum)
	if err != nil {
		return false
	}
	return !v.less(min)
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (r githubRelease) version() (version, bool) {
	if r.Draft || r.Prerelease {
		return version{}, false
	}
	parsed, err := parseVersion(r.TagName)
	if err != nil {
		return version{}, false
	}
	return parsed, true
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	var release githubRelease
	url := gitHubAPIBaseURL + "/repos/mlflow/mlflow/releases/latest"
	if err := getJSON(ctx, url, &release); err != nil {
		return githubRelease{}, err
	}
	return release, nil
}

// fetchNewestPerMinor walks the GitHub releases pages (newest first) and keeps
// the highest patch release of every minor line within [minimum, latest].
func fetchNewestPerMinor(ctx context.Context, minimum, latest version) (map[minorKey]version, error) {
	newest := make(map[minorKey]version)
	for page := 1; page <= maxReleasePages; page++ {
		url := fmt.Sprintf(
			"%s/repos/mlflow/mlflow/releases?per_page=%d&page=%d",
			gitHubAPIBaseURL, releasesPerPage, page,
		)
		var releases []githubRelease
		if err := getJSON(ctx, url, &releases); err != nil {
			return nil, err
		}

		oldest := latest
		for _, release := range releases {
			v, ok := release.version()
			if !ok {
				continue
			}
			if v.less(oldest) {
				oldest = v
			}
			if v.less(minimum) || latest.less(v) {
				continue
			}
			key := minorKey{major: v.major, minor: v.minor}
			if current, ok := newest[key]; !ok || current.less(v) {
				newest[key] = v
			}
		}

		// Releases are ordered newest first, so a partial page or a page that
		// already reached below the minimum means there is nothing left to
		// collect.
		if len(releases) < releasesPerPage || oldest.less(minimum) {
			break
		}
	}
	return newest, nil
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build GitHub API request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "eval-hub-mlflow-integration-test")
	// Unauthenticated GitHub API requests are limited to 60/hour per IP.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return fmt.Errorf("fetch %s: unexpected status %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s: %w", url, err)
	}
	return nil
}

type version struct {
	major int
	minor int
	patch int
}

type minorKey struct {
	major int
	minor int
}

func parseVersion(value string) (version, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q is not in MAJOR.MINOR.PATCH form", value)
	}
	numbers := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, fmt.Errorf("version %q has a non-numeric component %q", value, part)
		}
		numbers[i] = n
	}
	return version{major: numbers[0], minor: numbers[1], patch: numbers[2]}, nil
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) less(other version) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}
