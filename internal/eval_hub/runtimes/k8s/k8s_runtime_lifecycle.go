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
	EvaluationPhasePending           = "Pending"
	EvaluationPhaseRunning           = "Running"
	EvaluationPhaseCompleted         = "Completed"
	EvaluationPhaseFailed            = "Failed"
	EvaluationPhaseThresholdViolated = "ThresholdViolated"

	// Kubernetes Event reasons emitted on lifecycle transitions.
	eventReasonRunning           = "EvaluationRunning"
	eventReasonCompleted         = "EvaluationCompleted"
	eventReasonFailed            = "EvaluationFailed"
	eventReasonThresholdViolated = "EvaluationThresholdViolated"
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
		statusPayload := map[string]any{
			"phase":           phase,
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
			"evaluation_id":   evaluation.Resource.ID,
			"benchmark_index": benchmarkIndex,
		}
		if annotErr := r.helper.PatchJobStatusAnnotation(signalCtx, namespace, job.Name, statusPayload); annotErr != nil {
			r.logger.WarnContext(signalCtx, "lifecycle signal: patch status annotation failed",
				"job_name", job.Name,
				"phase", phase,
				"error", annotErr,
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

// NotifyThresholdViolation patches the evaluation-phase label to ThresholdViolated on the backing
// Kubernetes Job and emits an EvaluationThresholdViolated Warning Event containing the metric name,
// actual measured value, and configured threshold. Both operations are best-effort: failures are
// logged at Warn level and never surfaced to the caller.
func (r *K8sRuntime) NotifyThresholdViolation(ctx context.Context, evaluation *api.EvaluationJobResource, benchmarkIndex int, metricName string, actualValue, threshold float32) {
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
		r.logger.WarnContext(signalCtx, "threshold violation signal: list jobs failed",
			"job_id", evaluation.Resource.ID,
			"benchmark_index", benchmarkIndex,
			"error", err,
		)
		return
	}
	for i := range jobs {
		job := &jobs[i]
		if patchErr := r.helper.PatchJobPhaseLabel(signalCtx, namespace, job.Name, EvaluationPhaseThresholdViolated); patchErr != nil {
			r.logger.WarnContext(signalCtx, "threshold violation signal: patch phase label failed",
				"job_name", job.Name,
				"error", patchErr,
			)
		}
		messageFmt := "metric=%s actual=%.4f threshold=%.4f"
		if emitErr := r.helper.EmitEvent(job, corev1.EventTypeWarning, eventReasonThresholdViolated, messageFmt, metricName, actualValue, threshold); emitErr != nil {
			r.logger.WarnContext(signalCtx, "threshold violation signal: emit event failed",
				"job_name", job.Name,
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
