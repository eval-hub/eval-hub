package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/auth"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/kubernetes"
)

func writeError(w http.ResponseWriter, msg *messages.MessageCode, params ...any) {
	m := messages.GetErrorMessage(msg, params...)
	e := api.Error{Message: m, MessageCode: msg.GetCode()}
	json, _ := json.Marshal(e)
	http.Error(w, string(json), msg.GetStatusCode())
}

// Performs authorization on the configured endpoints. Other endpoints are allowed without authorization.
func WithAuthorization(next http.Handler, logger *slog.Logger, client *kubernetes.Clientset, config *auth.AuthConfig) (http.Handler, error) {

	auth, err := auth.NewSarAuthorizer(client, logger, config)

	if err != nil {
		logger.Error("Error creating authorizer", "error", err)
		return nil, err
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		user, ok := request.UserFrom(r.Context())
		if !ok {
			// If no user is found we assume that this endpoints is allowed with not authn/z
			next.ServeHTTP(w, r)
			return
		}
		decision, reason, err := auth.AuthorizeRequest(r.Context(), r)
		switch decision {

		case authorizer.DecisionNoOpinion:
			logger.Error("Bad request", "path", r.URL.Path, "method", r.Method, "reason", reason)
			writeError(w, messages.BadRequest, "Error", reason)
			return
		case authorizer.DecisionDeny:
			logger.Error("Request forbidden", "path", r.URL.Path, "method", r.Method, "reason", reason)
			writeError(w, messages.Forbidden)
			return
		default:
		}

		if err != nil {
			logger.Error("Error authorizing request", "error", err)
			writeError(w, messages.InternalServerError, "Error", err.Error())
			return
		}

		logger.Info("Request authorized", "path", r.URL.Path, "method", r.Method, "user", user.GetName())

		r.Header.Set(USER_HEADER, user.GetName())
		r.Header.Del("Authorization")
		next.ServeHTTP(w, r)

	})

	return handler, nil
}
