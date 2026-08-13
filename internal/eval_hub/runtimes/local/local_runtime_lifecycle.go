package local

import (
	"context"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// NotifyJobPhaseTransition is a no-op for the local runtime: there are no Kubernetes objects
// to label or emit Events against.
func (r *LocalRuntime) NotifyJobPhaseTransition(_ context.Context, _ *api.EvaluationJobResource, _ int, _ api.State) {
}

// NotifyThresholdViolation is a no-op for the local runtime: there are no Kubernetes objects
// to label or emit Events against.
func (r *LocalRuntime) NotifyThresholdViolation(_ context.Context, _ *api.EvaluationJobResource, _ int, _ string, _, _ float32) {
}
