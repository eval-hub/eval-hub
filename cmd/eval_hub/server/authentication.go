package server

import (
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/auth"
	"github.com/eval-hub/eval-hub/internal/messages"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/kubernetes"
)

// Performs authentication on the configured endpoints. Other endpoints are allowed without authentication.
func WithAuthentication(next http.Handler, logger *slog.Logger, client *kubernetes.Clientset, config *auth.AuthConfig) (http.Handler, error) {

	authn, err := auth.NewAuthenticator(client, logger)
	if err != nil {
		logger.Error("Error creating authenticator", "error", err)
		return nil, err
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Authenticating request", "path", r.URL.Path, "method", r.Method)
		rules := auth.FindRules(r, config)
		if len(rules) == 0 {
			logger.Info("No rules found for request", "path", r.URL.Path, "method", r.Method)
			// If the endpoint and method is not mentioned in the authorization config,
			// we skip authentication as well. Authorization will get no user info.
			next.ServeHTTP(w, r)
			return
		}

		resp, ok, err := authn.AuthenticateRequest(r)
		if err != nil {
			logger.Error("Error authenticating request", "error", err)
			writeError(w, messages.Unauthorized, "Error", err.Error())
			return
		}
		if !ok {
			logger.Error("Request not authenticated", "path", r.URL.Path, "method", r.Method)
			writeError(w, messages.Unauthorized)
			return
		}

		r = r.WithContext(request.WithUser(r.Context(), resp.User))
		next.ServeHTTP(w, r)
	})

	return handler, nil
}
