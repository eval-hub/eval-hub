package k8s

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func (r *K8sRuntime) GetEvaluationLogs(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	benchmarkIndex *int,
	opts api.EvaluationLogOptions,
) (string, error) {
	if r.ctx == nil {
		return "", fmt.Errorf("kubernetes runtime: nil context — WithContext must be called before GetEvaluationLogs")
	}
	if len(benchmarks) == 0 {
		return "", serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}
	if benchmarkIndex != nil {
		if *benchmarkIndex < 0 || *benchmarkIndex >= len(benchmarks) {
			return "", serviceerrors.NewServiceError(
				messages.ResourceNotFound,
				"Type", "benchmark",
				"ResourceId", fmt.Sprintf("%d", *benchmarkIndex),
			)
		}
		return r.readBenchmarkLogs(evaluation, benchmarks[*benchmarkIndex], *benchmarkIndex, opts, false)
	}

	var sections []string
	for i, bench := range benchmarks {
		section, err := r.readBenchmarkLogs(evaluation, bench, i, opts, true)
		if err != nil {
			return "", err
		}
		if section != "" {
			sections = append(sections, section)
		}
	}
	return strings.Join(sections, "\n"), nil
}

func (r *K8sRuntime) readBenchmarkLogs(
	evaluation *api.EvaluationJobResource,
	bench api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	opts api.EvaluationLogOptions,
	includeHeader bool,
) (string, error) {
	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	labelSelector := fmt.Sprintf(
		"%s=%s,%s=%s",
		labelJobIDKey, sanitizeLabelValue(evaluation.Resource.ID),
		labelBenchmarkIndexKey, sanitizeLabelValue(strconv.Itoa(benchmarkIndex)),
	)
	jobs, err := r.helper.ListJobs(r.ctx, namespace, labelSelector)
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		if includeHeader {
			return shared.FormatLogSectionHeader("unknown", adapterContainerName, bench.ID), nil
		}
		return "", nil
	}

	job := jobs[0]
	pod, err := r.latestJobPod(namespace, job.Name)
	if err != nil {
		return "", err
	}
	if pod == nil {
		if includeHeader {
			return shared.FormatLogSectionHeader(job.Name, adapterContainerName, bench.ID), nil
		}
		return "", nil
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

	logs, err := r.helper.GetPodLogs(r.ctx, namespace, pod.Name, logOpts)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logs = ""
		} else {
			return "", err
		}
	}
	logs = strings.TrimRight(logs, "\n")
	if !includeHeader {
		return logs, nil
	}
	header := shared.FormatLogSectionHeader(pod.Name, adapterContainerName, bench.ID)
	if logs == "" {
		return header, nil
	}
	return header + "\n" + logs, nil
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
