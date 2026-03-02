package server

import (
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/auth"
	"github.com/eval-hub/eval-hub/internal/messages"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/kubernetes"
)

func WithAuthentication(next http.Handler, logger *slog.Logger, client *kubernetes.Clientset) http.Handler {

	auth, err := auth.NewAuthenticator(client, logger)
	if err != nil {
		logger.Error("Error creating authenticator", "error", err)
		return next
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok, err := auth.AuthenticateRequest(r)
		if err != nil {
			logger.Error("Error authenticating request", "error", err)
			writeError(w, messages.InternalServerError, "Error", err.Error())
			return
		}
		if !ok {
			logger.Error("Request not authenticated", "path", r.URL.Path, "method", r.Method)
			writeError(w, messages.Unauthorized, "Error", "Request not authenticated")
			return
		}

		r = r.WithContext(request.WithUser(r.Context(), resp.User))
		next.ServeHTTP(w, r)
	})

	return handler
}
