package features

import (
	"testing"
)

func TestResolveEnvSubstitutionValue(t *testing.T) {
	const shaKey = "TEST_DATA_HF_SHA_REVISION"
	const revKey = "TEST_DATA_HF_REVISION"

	t.Run("uses first set env in comma-separated chain", func(t *testing.T) {
		t.Setenv(shaKey, "deadbeef")
		t.Setenv(revKey, "develop")
		got := resolveEnvSubstitutionValue(shaKey+","+revKey, "main", true)
		if got != "deadbeef" {
			t.Fatalf("got %q, want deadbeef", got)
		}
	})

	t.Run("falls back to second env when first is unset", func(t *testing.T) {
		t.Setenv(revKey, "develop")
		got := resolveEnvSubstitutionValue(shaKey+","+revKey, "main", true)
		if got != "develop" {
			t.Fatalf("got %q, want develop", got)
		}
	})

	t.Run("falls back to literal default when env chain unset", func(t *testing.T) {
		got := resolveEnvSubstitutionValue(shaKey+","+revKey, "main", true)
		if got != "main" {
			t.Fatalf("got %q, want main", got)
		}
	})

	t.Run("single env name remains supported", func(t *testing.T) {
		t.Setenv(revKey, "feature-branch")
		got := resolveEnvSubstitutionValue(revKey, "main", true)
		if got != "feature-branch" {
			t.Fatalf("got %q, want feature-branch", got)
		}
	})
}
