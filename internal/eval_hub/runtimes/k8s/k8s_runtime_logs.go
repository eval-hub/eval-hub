package k8s

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func (r *K8sRuntime) StreamEvaluationLogs(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	benchmarkIndex *int,
	opts api.EvaluationLogOptions,
	w io.Writer,
) error {
	if r.ctx == nil {
		return fmt.Errorf("kubernetes runtime: nil context — WithContext must be called before StreamEvaluationLogs")
	}
	if len(benchmarks) == 0 {
		return serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}
	if benchmarkIndex != nil {
		if *benchmarkIndex < 0 || *benchmarkIndex >= len(benchmarks) {
			return serviceerrors.NewServiceError(
				messages.ResourceNotFound,
				"Type", "benchmark",
				"ResourceId", fmt.Sprintf("%d", *benchmarkIndex),
			)
		}
		return r.streamBenchmarkLogs(evaluation, benchmarks[*benchmarkIndex], *benchmarkIndex, opts, false, w)
	}

	for i, bench := range benchmarks {
		if i > 0 {
			if _, err := fmt.Fprint(w, "\n"); err != nil {
				return err
			}
		}
		if err := r.streamBenchmarkLogs(evaluation, bench, i, opts, true, w); err != nil {
			return err
		}
	}
	return nil
}

func (r *K8sRuntime) streamBenchmarkLogs(
	evaluation *api.EvaluationJobResource,
	bench api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	opts api.EvaluationLogOptions,
	includeHeader bool,
	w io.Writer,
) error {
	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	labelSelector := fmt.Sprintf(
		"%s=%s,%s=%s",
		labelJobIDKey, sanitizeLabelValue(evaluation.Resource.ID),
		labelBenchmarkIndexKey, sanitizeLabelValue(strconv.Itoa(benchmarkIndex)),
	)
	jobs, err := r.helper.ListJobs(r.ctx, namespace, labelSelector)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		if includeHeader {
			_, err = fmt.Fprint(w, shared.FormatLogSectionHeader("unknown", adapterContainerName, bench.ID))
			return err
		}
		return nil
	}

	job := jobs[0]
	pod, err := r.latestJobPod(namespace, job.Name)
	if err != nil {
		return err
	}
	if pod == nil {
		if includeHeader {
			_, err = fmt.Fprint(w, shared.FormatLogSectionHeader(job.Name, adapterContainerName, bench.ID))
			return err
		}
		return nil
	}

	if includeHeader {
		header := shared.FormatLogSectionHeader(pod.Name, adapterContainerName, bench.ID)
		if _, err = fmt.Fprint(w, header+"\n"); err != nil {
			return err
		}
	}

	logOpts := &corev1.PodLogOptions{
		Container:  adapterContainerName,
		Timestamps: opts.Timestamps,
	}
	if opts.TailLines > 0 {
		tail := int64(opts.TailLines)
		logOpts.TailLines = &tail
	}
	if opts.SinceSeconds != nil {
		since := int64(*opts.SinceSeconds)
		logOpts.SinceSeconds = &since
	}

	err = r.helper.StreamPodLogs(r.ctx, namespace, pod.Name, logOpts, w)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *K8sRuntime) latestJobPod(namespace, jobName string) (*corev1.Pod, error) {
	pods, err := r.helper.ListPods(r.ctx, namespace, fmt.Sprintf("job-name=%s", jobName))
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, nil
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].CreationTimestamp.After(pods[j].CreationTimestamp.Time)
	})
	return &pods[0], nil
}
