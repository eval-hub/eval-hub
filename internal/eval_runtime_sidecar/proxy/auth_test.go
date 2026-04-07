package proxy

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func resetAuthTokenCachesForTest() {
	evalHubCachedToken.Store(nil)
	mlflowCachedToken.Store(nil)
	modelCachedToken.Store(nil)
	ociCachedToken.Store(nil)
}

func TestResolveAuthToken(t *testing.T) {
	logger := slog.Default()

	t.Run("returns static token when no path and token set", func(t *testing.T) {
		resetAuthTokenCachesForTest()
		input := AuthTokenInput{
			TargetEndpoint: "eval-hub",
			AuthTokenPath:  "",
			AuthToken:      "my-token",
		}
		got := ResolveAuthToken(logger, input)
		if got != "my-token" {
			t.Errorf("ResolveAuthToken() = %q, want %q", got, "my-token")
		}
	})

	t.Run("returns token from file when path exists and has content", func(t *testing.T) {
		resetAuthTokenCachesForTest()
		dir := t.TempDir()
		tokenFile := filepath.Join(dir, "token")
		if err := os.WriteFile(tokenFile, []byte(" file-token \n"), 0600); err != nil {
			t.Fatal(err)
		}
		input := AuthTokenInput{
			TargetEndpoint: "eval-hub",
			AuthTokenPath:  tokenFile,
			AuthToken:      "fallback",
		}
		got := ResolveAuthToken(logger, input)
		if got != "file-token" {
			t.Errorf("ResolveAuthToken() = %q, want %q", got, "file-token")
		}
	})

	t.Run("falls back to static token when file missing", func(t *testing.T) {
		resetAuthTokenCachesForTest()
		input := AuthTokenInput{
			TargetEndpoint: "eval-hub",
			AuthTokenPath:  filepath.Join(t.TempDir(), "nonexistent"),
			AuthToken:      "fallback-token",
		}
		got := ResolveAuthToken(logger, input)
		if got != "fallback-token" {
			t.Errorf("ResolveAuthToken() = %q, want %q", got, "fallback-token")
		}
	})

	t.Run("falls back to static token when file empty after trim", func(t *testing.T) {
		resetAuthTokenCachesForTest()
		dir := t.TempDir()
		tokenFile := filepath.Join(dir, "empty")
		if err := os.WriteFile(tokenFile, []byte("   \n"), 0600); err != nil {
			t.Fatal(err)
		}
		input := AuthTokenInput{
			TargetEndpoint: "eval-hub",
			AuthTokenPath:  tokenFile,
			AuthToken:      "static",
		}
		got := ResolveAuthToken(logger, input)
		if got != "static" {
			t.Errorf("ResolveAuthToken() = %q, want %q", got, "static")
		}
	})

	t.Run("cache returns same token on second call for same endpoint", func(t *testing.T) {
		resetAuthTokenCachesForTest()
		input := AuthTokenInput{
			TargetEndpoint:    "mlflow",
			AuthTokenPath:     "",
			AuthToken:         "cached-token",
			TokenCacheTimeout: time.Minute,
		}
		got1 := ResolveAuthToken(logger, input)
		got2 := ResolveAuthToken(logger, input)
		if got1 != "cached-token" || got2 != "cached-token" {
			t.Errorf("ResolveAuthToken() = %q, %q, want cached-token both", got1, got2)
		}
	})

	t.Run("empty target endpoint does not use cache", func(t *testing.T) {
		resetAuthTokenCachesForTest()
		input := AuthTokenInput{
			TargetEndpoint: "",
			AuthTokenPath:  "",
			AuthToken:      "no-cache-token",
		}
		got := ResolveAuthToken(logger, input)
		if got != "no-cache-token" {
			t.Errorf("ResolveAuthToken() = %q, want %q", got, "no-cache-token")
		}
	})
}

func TestUpdateCachedToken(t *testing.T) {
	resetAuthTokenCachesForTest()
	input := AuthTokenInput{
		TargetEndpoint:    "eval-hub",
		TokenCacheTimeout: time.Hour,
	}
	UpdateCachedToken(input, "injected")
	got := ResolveAuthToken(slog.Default(), AuthTokenInput{
		TargetEndpoint:    "eval-hub",
		AuthTokenPath:     filepath.Join(t.TempDir(), "missing"),
		AuthToken:         "would-read-if-not-cached",
		TokenCacheTimeout: time.Hour,
	})
	if got != "injected" {
		t.Errorf("after UpdateCachedToken, ResolveAuthToken = %q, want injected", got)
	}
	UpdateCachedToken(input, "")
	got2 := ResolveAuthToken(slog.Default(), AuthTokenInput{
		TargetEndpoint:    "eval-hub",
		AuthToken:         "after-clear",
		TokenCacheTimeout: time.Hour,
	})
	if got2 != "after-clear" {
		t.Errorf("after clear, ResolveAuthToken = %q, want after-clear", got2)
	}
}
