package handlers

import (
	"fmt"

	evalhub "github.com/eval-hub/eval-hub/eval_runtime_sidecar/api/eval_hub"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/common"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/executioncontext"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/http_wrappers"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/logging"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/serialization"
)

func (h *Handlers) HandleUpdateEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(common.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		ctx.Logger.Error("path parameter %s is required", common.PATH_PARAMETER_JOB_ID, nil)
		w.Error(fmt.Errorf("path parameter %s is required", common.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	// get the body bytes from the context
	bodyBytes, err := r.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	status := &evalhub.StatusEvent{}
	err = serialization.Unmarshal(nil, ctx, bodyBytes, status)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	//TODO: Validate the status event

	err = h.evalHubClient.PostEvent(evaluationJobID, status)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	w.WriteJSON(nil, 204)
}
