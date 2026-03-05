package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/logging"
)

type errorResponse struct {
	MessageCode string `json:"message_code"`
	Message     string `json:"message"`
}

func writeErrorJSON(w http.ResponseWriter, code int, messageCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{MessageCode: messageCode, Message: message})
}

func (h *Handlers) HandleUpdateEvaluation(ctx *executioncontext.ExecutionContext, w http.ResponseWriter, r *http.Request) {
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "missing_path_parameter", fmt.Sprintf("path parameter %s is required", constants.PATH_PARAMETER_JOB_ID))
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	if h.evalHubClient == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "eval_hub_not_configured", "EvalHub client is not configured (base URL not set)")
		return
	}

	res, err := h.evalHubClient.PostEvent(evaluationJobID, bodyBytes)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_server_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	if len(res.Body) > 0 {
		w.Write(res.Body)
	}
}
