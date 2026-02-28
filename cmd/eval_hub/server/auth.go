package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/auth"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func writeError(w http.ResponseWriter, msg *messages.MessageCode, params ...any) {
	m := messages.GetErrorMessage(msg, params...)
	e := api.Error{Message: m, MessageCode: msg.GetCode()}
	json, _ := json.Marshal(e)
	http.Error(w, string(json), msg.GetStatusCode())
}

func AuthMiddleware(next http.Handler, logger *slog.Logger, auth *auth.SarAuthorizer) http.Handler {
	if auth == nil {
		return next
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		decision, reason, err := auth.AuthorizeRequest(r.Context(), r)

		if err != nil {
			logger.Error("Error authorizing request", "error", err)
			writeError(w, messages.InternalServerError, "Error", err.Error())
			return
		}

		if decision != authorizer.DecisionAllow {
			logger.Error("Request forbidden", "path", r.URL.Path, "method", r.Method, "reason", reason)
			writeError(w, messages.Forbidden, "Error", reason)
			return
		}

		next.ServeHTTP(w, r)

	})

	return handler
}
