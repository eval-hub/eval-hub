package server

import (
	"net/http"
	"strings"
)

const (
	collectionClonesRoutePattern             = "/api/v1/evaluations/collections/{collection_id}/clones"
	collectionGenerationResourceRoutePattern = "/api/v1/evaluations/collections/generation/{id}"
	collectionSubresourceRoutePattern        = "/api/v1/evaluations/collections/{collection_id}/{id}"
)

// collectionSubresourceRoute resolves the concrete route for paths dispatched
// through the shared ServeMux pattern. Keeping the route labels stable avoids
// merging clone traffic with collection-generation polling in metrics and spans.
func collectionSubresourceRoute(method, path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "evaluations" || parts[3] != "collections" {
		return "", false
	}

	collectionID, subresourceID := parts[4], parts[5]
	if collectionID == "generation" && (method != http.MethodPost || subresourceID != "clones") {
		return collectionGenerationResourceRoutePattern, true
	}
	if subresourceID == "clones" {
		return collectionClonesRoutePattern, true
	}
	return "", false
}

// collectionSubresourcePatternMiddleware restores the concrete route pattern
// before instrumentation reads Request.Pattern from the shared ServeMux route.
func collectionSubresourcePatternMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route, ok := collectionSubresourceRoute(r.Method, r.URL.Path); ok {
			r.Pattern = route
		}
		next.ServeHTTP(w, r)
	})
}
