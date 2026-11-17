package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectionSubresourceRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{
			name:   "clone request retains its route",
			method: http.MethodPost,
			path:   "/api/v1/evaluations/collections/custom-collection/clones",
			want:   collectionClonesRoutePattern,
		},
		{
			name:   "generation get retains its route",
			method: http.MethodGet,
			path:   "/api/v1/evaluations/collections/generation/generation-id",
			want:   collectionGenerationResourceRoutePattern,
		},
		{
			name:   "generation delete retains its route",
			method: http.MethodDelete,
			path:   "/api/v1/evaluations/collections/generation/generation-id",
			want:   collectionGenerationResourceRoutePattern,
		},
		{
			name:   "generation collections clone path remains a clone route for post",
			method: http.MethodPost,
			path:   "/api/v1/evaluations/collections/generation/clones",
			want:   collectionClonesRoutePattern,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := collectionSubresourceRoute(test.method, test.path)
			if !ok || got != test.want {
				t.Fatalf("collectionSubresourceRoute(%q, %q) = %q, %t; want %q, true", test.method, test.path, got, ok, test.want)
			}

			request := httptest.NewRequest(test.method, test.path, nil)
			request.Pattern = collectionSubresourceRoutePattern
			if got := httpRoutePattern(request); got != test.want {
				t.Errorf("httpRoutePattern() = %q, want %q", got, test.want)
			}
			if got := spanNameFormatter(collectionSubresourceRoutePattern, request); got != test.method+" "+test.want {
				t.Errorf("spanNameFormatter() = %q, want %q", got, test.method+" "+test.want)
			}
		})
	}
}

func TestCollectionSubresourceRouteRejectsUnmatchedPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v1/evaluations/jobs/job-id",
		"/api/v1/other/collections/custom/benchmarks",
		"/api/v1/evaluations/collections/custom/benchmarks",
	} {
		if got, ok := collectionSubresourceRoute(http.MethodGet, path); ok || got != "" {
			t.Errorf("collectionSubresourceRoute(%q) = %q, %t; want empty route, false", path, got, ok)
		}
	}
}

func TestHTTPRoutePatternLeavesUndispatchedPathsUnlabeled(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/collections/custom-collection/clones/", nil)
	if got := httpRoutePattern(request); got != "" {
		t.Fatalf("httpRoutePattern() = %q, want empty route for an undispatched path", got)
	}
}

func TestCollectionSubresourcePatternMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{
			name:   "clone route",
			method: http.MethodPost,
			path:   "/api/v1/evaluations/collections/custom-collection/clones",
			want:   collectionClonesRoutePattern,
		},
		{
			name:   "generation route",
			method: http.MethodGet,
			path:   "/api/v1/evaluations/collections/generation/generation-id",
			want:   collectionGenerationResourceRoutePattern,
		},
		{
			name:   "unmatched route keeps mux pattern",
			method: http.MethodGet,
			path:   "/api/v1/evaluations/jobs/job-id",
			want:   collectionSubresourceRoutePattern,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var got string
			handler := collectionSubresourcePatternMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = r.Pattern
			}))
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Pattern = collectionSubresourceRoutePattern
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if got != test.want {
				t.Fatalf("downstream Request.Pattern = %q, want %q", got, test.want)
			}
		})
	}
}
