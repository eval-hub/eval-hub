package server

import (
	"net/http"

	"github.com/eval-hub/eval-hub/internal/config"
)

// CorsMiddleware provides CORS headers for local development.
//
// This middleware is intended for LOCAL MODE ONLY and should be enabled
// by starting the server with the --local flag. It sets permissive CORS
// headers to allow cross-origin requests from tools like the Swagger editor.
//
// WARNING: This configuration is NOT suitable for production environments.
func CorsMiddleware(next http.Handler, cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Global-Transaction-Id")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// Handle preflight OPTIONS requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}
