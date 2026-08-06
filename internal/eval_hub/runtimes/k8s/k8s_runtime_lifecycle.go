package k8s

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

const (
	// lifecycleSignalTimeout caps each NotifyJobPhaseTransition call. Lifecycle signals are
	// best-effort; slow API operations should not hold the caller indefinitely.
	lifecycleSignalTimeout = 10 * time.Second

	// Evaluation phase label values — written to the trustyai.opendatahub.io/evaluation-phase
	// label on the backing Kubernetes Job. Pending is stamped at creation; the rest are patched
	// on each lifecycle transition via PatchJobPhaseLabel.
	EvaluationPhasePending   = "Pending"
	EvaluationPhaseRunning   = "Running"
	EvaluationPhaseCompleted = "Completed"
	EvaluationPhaseFailed    = "Failed"

	// Kubernetes Event reasons emitted on lifecycle transitions.
	eventReasonRunning   = "EvaluationRunning"
	eventReasonCompleted = "EvaluationCompleted"
	eventReasonFailed    = "EvaluationFailed"
)

// NotifyJobPhaseTransition patches the evaluation-phase label on the backing Kubernetes Job and
// emits a Kubernetes Event for the transition. Both operations are best-effort: failures are
// logged at Warn level and never surfaced to the caller.
func (r *K8sRuntime) NotifyJobPhaseTransition(ctx context.Context, evaluation *api.EvaluationJobResource, benchmarkIndex int, state api.State) {
	phase, eventtype, reason, ok := lifecycleSignal(state)
	if !ok {
		return
	}
	signalCtx, cancel := context.WithTimeout(ctx, lifecycleSignalTimeout)
	defer cancel()

	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	labelSelector := fmt.Sprintf(
		"%s=%s,%s=%s",
		labelJobIDKey, sanitizeLabelValue(evaluation.Resource.ID),
		labelBenchmarkIndexKey, sanitizeLabelValue(strconv.Itoa(benchmarkIndex)),
	)
	jobs, err := r.helper.ListJobs(signalCtx, namespace, labelSelector)
	if err != nil {
		r.logger.WarnContext(signalCtx, "lifecycle signal: list jobs failed",
			"job_id", evaluation.Resource.ID,
			"benchmark_index", benchmarkIndex,
			"phase", phase,
			"error", err,
		)
		return
	}
	for i := range jobs {
		job := &jobs[i]
		if patchErr := r.helper.PatchJobPhaseLabel(signalCtx, namespace, job.Name, phase); patchErr != nil {
			r.logger.WarnContext(signalCtx, "lifecycle signal: patch phase label failed",
				"job_name", job.Name,
				"phase", phase,
				"error", patchErr,
			)
		}
		messageFmt := "benchmark %d phase transition: %s"
		if emitErr := r.helper.EmitEvent(job, eventtype, reason, messageFmt, benchmarkIndex, phase); emitErr != nil {
			r.logger.WarnContext(signalCtx, "lifecycle signal: emit event failed",
				"job_name", job.Name,
				"reason", reason,
				"error", emitErr,
			)
		}
	}
}

// lifecycleSignal maps an evaluation benchmark state to the Kubernetes label value, event type,
// and event reason for a lifecycle transition. Returns ok=false for states that do not produce
// signals: Pending is stamped at Job creation (see EvaluationPhasePending in job_builders.go);
// Cancelled has no corresponding Kubernetes job phase.
func lifecycleSignal(state api.State) (phase, eventtype, reason string, ok bool) {
	switch state {
	case api.StateRunning:
		return EvaluationPhaseRunning, corev1.EventTypeNormal, eventReasonRunning, true
	case api.StateCompleted:
		return EvaluationPhaseCompleted, corev1.EventTypeNormal, eventReasonCompleted, true
	case api.StateFailed:
		return EvaluationPhaseFailed, corev1.EventTypeWarning, eventReasonFailed, true
	default:
		return "", "", "", false
	}
}
