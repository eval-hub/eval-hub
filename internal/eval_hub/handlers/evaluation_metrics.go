package handlers

import (
	"context"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func recordEvaluationJobTerminalStateAfterUpdate(
	ctx context.Context,
	getJob func() (*api.EvaluationJobResource, error),
	previousState api.OverallState,
) {
	job, err := getJob()
	if err != nil || job == nil || job.Status == nil {
		return
	}
	metrics.RecordEvaluationJobTerminalState(ctx, previousState, job.Status.State)
}
