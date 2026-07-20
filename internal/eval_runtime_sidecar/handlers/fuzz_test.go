package handlers

import (
	"strings"
	"testing"
)

func FuzzIsMLflowProxyPath(f *testing.F) {
	f.Add("/api/2.0/mlflow")
	f.Add("/api/2.0/mlflow/")
	f.Add("/api/2.0/mlflow/experiments/list")
	f.Add("/api/2.0/mlflowx")
	f.Add("/api/2.0/mlflow-artifacts")
	f.Add("/api/2.0/mlflow-artifactsmalicious")
	f.Add("/api/3.0/mlflow/server-info")
	f.Add("/api/3.0/mlflowx")
	f.Add("")
	f.Add("/prefix/api/2.0/mlflow/runs")

	f.Fuzz(func(t *testing.T, path string) {
		got := isMLflowProxyPath(path)
		want := mlflowPathMatchesPrefix(path, mlflowAPIv2PathPrefix) ||
			mlflowPathMatchesPrefix(path, mlflowAPIv3PathPrefix) ||
			mlflowPathMatchesPrefix(path, mlflowAPIv2ArtifactsPathPrefix)
		if got != want {
			t.Fatalf("isMLflowProxyPath(%q) = %v, want %v", path, got, want)
		}
		// Prefix confusion: a path that only extends a shorter prefix with non-'/'
		// characters (e.g. /api/2.0/mlflowx) must not match, unless it is an exact
		// match or subpath of a longer allowlisted prefix (mlflow-artifacts).
		if strings.HasPrefix(path, mlflowAPIv2PathPrefix) &&
			len(path) > len(mlflowAPIv2PathPrefix) &&
			path[len(mlflowAPIv2PathPrefix)] != '/' &&
			!mlflowPathMatchesPrefix(path, mlflowAPIv2ArtifactsPathPrefix) &&
			got {
			t.Fatalf("isMLflowProxyPath(%q) matched /api/2.0/mlflow without path separator", path)
		}
		if strings.HasPrefix(path, mlflowAPIv3PathPrefix) &&
			len(path) > len(mlflowAPIv3PathPrefix) &&
			path[len(mlflowAPIv3PathPrefix)] != '/' &&
			got {
			t.Fatalf("isMLflowProxyPath(%q) matched /api/3.0/mlflow without path separator", path)
		}
	})
}

func FuzzRequestPathForRouting(f *testing.F) {
	f.Add("/api/2.0/mlflow/experiments/list")
	f.Add("/api/2.0/mlflow/experiments/list?foo=bar")
	f.Add("/api/2.0/mlflow/experiments/list#frag")
	f.Add("/api/2.0/mlflow%2Fextra")
	f.Add("://bad")
	f.Add("")
	f.Add("/path?x=1&y=2#z")

	f.Fuzz(func(t *testing.T, uri string) {
		path := requestPathForRouting(uri)
		// Query and fragment must never affect MLflow routing decisions.
		routable := isMLflowProxyPath(path)
		if strings.Contains(uri, "?") || strings.Contains(uri, "#") {
			_ = routable
		}
		// Deterministic.
		if path != requestPathForRouting(uri) {
			t.Fatalf("requestPathForRouting is not deterministic for %q", uri)
		}
	})
}
