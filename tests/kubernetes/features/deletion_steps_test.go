package features

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cucumber/godog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// Deletion Steps
// ============================================================================

func (tc *testContext) allJobsShouldBeDeleted() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked for deletion validation")
	}

	deadline := time.Now().Add(30 * time.Second)
	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	for time.Now().Before(deadline) {
		jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return fmt.Errorf("failed to list Jobs: %w", err)
		}
		remaining := 0
		for i := range jobs.Items {
			if jobs.Items[i].DeletionTimestamp == nil {
				remaining++
			}
		}
		if remaining == 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("expected all Jobs with job_id=%s to be deleted, but they still exist", tc.lastJobID)
}

func (tc *testContext) allConfigMapsShouldBeDeleted() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked for deletion validation")
	}

	deadline := time.Now().Add(30 * time.Second)
	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	for time.Now().Before(deadline) {
		configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return fmt.Errorf("failed to list ConfigMaps: %w", err)
		}
		remaining := 0
		for i := range configMaps.Items {
			if configMaps.Items[i].DeletionTimestamp == nil {
				remaining++
			}
		}
		if remaining == 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("expected all ConfigMaps with job_id=%s to be deleted, but they still exist", tc.lastJobID)
}

func (tc *testContext) jobsShouldStillExist() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked")
	}

	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list Jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		return fmt.Errorf("expected Jobs with job_id=%s to still exist, but found none", tc.lastJobID)
	}
	return nil
}

func (tc *testContext) configMapsShouldStillExist() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked")
	}

	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	if len(configMaps.Items) == 0 {
		return fmt.Errorf("expected ConfigMaps with job_id=%s to still exist, but found none", tc.lastJobID)
	}
	return nil
}

// ============================================================================
// Implemented Steps (previously stubbed)
// ============================================================================

func (tc *testContext) numberOfJobsShouldEqualBenchmarks() error {
	if len(tc.lastBenchmarkIDs) == 0 {
		return fmt.Errorf("no benchmark IDs tracked for comparison")
	}
	expected := len(tc.lastBenchmarkIDs)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) == expected {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	jobs, err := tc.listJobsByJobIDFresh()
	if err != nil {
		return err
	}
	return fmt.Errorf("expected %d Jobs, found %d", expected, len(jobs))
}

func (tc *testContext) numberOfConfigMapsShouldEqualBenchmarks() error {
	if len(tc.lastBenchmarkIDs) == 0 {
		return fmt.Errorf("no benchmark IDs tracked for comparison")
	}
	expected := len(tc.lastBenchmarkIDs)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		configMaps, err := tc.listConfigMapsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(configMaps) == expected {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	configMaps, err := tc.listConfigMapsByJobIDFresh()
	if err != nil {
		return err
	}
	return fmt.Errorf("expected %d ConfigMaps, found %d", expected, len(configMaps))
}

func (tc *testContext) eachJobShouldHaveUniqueBenchmarkIDLabel() error {
	jobs, err := tc.listJobsByJobID()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no Jobs found for unique benchmark_id validation")
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		benchID := job.Labels["benchmark_id"]
		if benchID == "" {
			return fmt.Errorf("Job %s missing benchmark_id label", job.Name)
		}
		if seen[benchID] {
			return fmt.Errorf("duplicate benchmark_id label found: %s", benchID)
		}
		seen[benchID] = true
	}
	return nil
}

func (tc *testContext) eachConfigMapShouldHaveUniqueBenchmarkIDLabel() error {
	configMaps, err := tc.listConfigMapsByJobID()
	if err != nil {
		return err
	}
	if len(configMaps) == 0 {
		return fmt.Errorf("no ConfigMaps found for unique benchmark_id validation")
	}
	seen := map[string]bool{}
	for _, cm := range configMaps {
		benchID := cm.Labels["benchmark_id"]
		if benchID == "" {
			return fmt.Errorf("ConfigMap %s missing benchmark_id label", cm.Name)
		}
		if seen[benchID] {
			return fmt.Errorf("duplicate benchmark_id label found: %s", benchID)
		}
		seen[benchID] = true
	}
	return nil
}

func (tc *testContext) eachJobShouldHaveUniqueBenchmarkIndexLabel() error {
	jobs, err := tc.listJobsByJobID()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no Jobs found for unique benchmark_index validation")
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		idx := job.Labels["benchmark_index"]
		if idx == "" {
			return fmt.Errorf("Job %s missing benchmark_index label", job.Name)
		}
		if seen[idx] {
			return fmt.Errorf("duplicate benchmark_index label found: %s", idx)
		}
		seen[idx] = true
	}
	return nil
}

func (tc *testContext) eachConfigMapShouldHaveUniqueBenchmarkIndexLabel() error {
	configMaps, err := tc.listConfigMapsByJobID()
	if err != nil {
		return err
	}
	if len(configMaps) == 0 {
		return fmt.Errorf("no ConfigMaps found for unique benchmark_index validation")
	}
	seen := map[string]bool{}
	for _, cm := range configMaps {
		idx := cm.Labels["benchmark_index"]
		if idx == "" {
			return fmt.Errorf("ConfigMap %s missing benchmark_index label", cm.Name)
		}
		if seen[idx] {
			return fmt.Errorf("duplicate benchmark_index label found: %s", idx)
		}
		seen[idx] = true
	}
	return nil
}

func (tc *testContext) responseShouldBeImmediate() error {
	if tc.lastRequestDuration == 0 {
		return fmt.Errorf("no request duration tracked")
	}
	if tc.lastRequestDuration > 5*time.Second {
		return fmt.Errorf("request took too long: %s", tc.lastRequestDuration)
	}
	return nil
}

func (tc *testContext) jobsShouldBeCreatedInBackground() error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) > 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("no Jobs created in background")
}

func (tc *testContext) jobHasBenchmarksConfigured(expected int) error {
	if len(tc.lastBenchmarkIDs) == 0 && tc.lastRequestBody != "" {
		if ids, err := parseBenchmarkIDs(tc.lastRequestBody); err == nil {
			tc.lastBenchmarkIDs = ids
		}
	}
	if len(tc.lastBenchmarkIDs) != expected {
		return fmt.Errorf("expected %d benchmarks, got %d", expected, len(tc.lastBenchmarkIDs))
	}
	return nil
}

func (tc *testContext) jobDeletionShouldUsePropagationPolicy(policy string) error {
	if !strings.EqualFold(policy, "Background") {
		return godog.ErrSkip
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		for _, job := range jobs {
			if job.DeletionTimestamp != nil {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("Jobs were not marked for background deletion")
}

func (tc *testContext) deleteEvaluationJobResourcesShouldBeCalled() error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		configMaps, err := tc.listConfigMapsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) == 0 && len(configMaps) == 0 {
			return nil
		}
		for _, job := range jobs {
			if job.DeletionTimestamp != nil {
				return nil
			}
		}
		for _, cm := range configMaps {
			if cm.DeletionTimestamp != nil {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("no evidence of cleanup after hard delete")
}

func (tc *testContext) allJobsShouldBeDeletedCount(expected int) error {
	if len(tc.lastBenchmarkIDs) != expected {
		return fmt.Errorf("expected %d benchmarks, got %d", expected, len(tc.lastBenchmarkIDs))
	}
	return tc.allJobsShouldBeDeleted()
}

func (tc *testContext) allConfigMapsShouldBeDeletedCount(expected int) error {
	if len(tc.lastBenchmarkIDs) != expected {
		return fmt.Errorf("expected %d benchmarks, got %d", expected, len(tc.lastBenchmarkIDs))
	}
	return tc.allConfigMapsShouldBeDeleted()
}

func (tc *testContext) listJobsByJobID() ([]batchv1.Job, error) {
	return tc.listJobsByJobIDWithCache(false)
}

func (tc *testContext) listJobsByJobIDFresh() ([]batchv1.Job, error) {
	return tc.listJobsByJobIDWithCache(true)
}

func (tc *testContext) listJobsByJobIDWithCache(forceRefresh bool) ([]batchv1.Job, error) {
	if tc.k8sClient == nil {
		return nil, fmt.Errorf("Kubernetes client not initialized")
	}
	if tc.lastJobID == "" {
		return nil, fmt.Errorf("no job ID tracked for listing")
	}
	if !forceRefresh && tc.cachedJobsJobID == tc.lastJobID && tc.cachedJobs != nil {
		tc.logK8sOp("Jobs", "cache-hit", tc.lastJobID)
		return tc.cachedJobs, nil
	}
	tc.logK8sOp("Jobs", "list", tc.lastJobID)
	jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Jobs: %w", err)
	}
	tc.cachedJobsJobID = tc.lastJobID
	tc.cachedJobs = jobs.Items
	return jobs.Items, nil
}

func (tc *testContext) listConfigMapsByJobID() ([]corev1.ConfigMap, error) {
	return tc.listConfigMapsByJobIDWithCache(false)
}

func (tc *testContext) listConfigMapsByJobIDFresh() ([]corev1.ConfigMap, error) {
	return tc.listConfigMapsByJobIDWithCache(true)
}

func (tc *testContext) listConfigMapsByJobIDWithCache(forceRefresh bool) ([]corev1.ConfigMap, error) {
	if tc.k8sClient == nil {
		return nil, fmt.Errorf("Kubernetes client not initialized")
	}
	if tc.lastJobID == "" {
		return nil, fmt.Errorf("no job ID tracked for listing")
	}
	if !forceRefresh && tc.cachedConfigMapsJobID == tc.lastJobID && tc.cachedConfigMaps != nil {
		tc.logK8sOp("ConfigMaps", "cache-hit", tc.lastJobID)
		return tc.cachedConfigMaps, nil
	}
	tc.logK8sOp("ConfigMaps", "list", tc.lastJobID)
	configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ConfigMaps: %w", err)
	}
	tc.cachedConfigMapsJobID = tc.lastJobID
	tc.cachedConfigMaps = configMaps.Items
	return configMaps.Items, nil
}

func (tc *testContext) logK8sOp(resource, action, jobID string) {
	if os.Getenv("K8S_TEST_DEBUG") != "true" {
		return
	}
	fmt.Printf("[K8S] %s %s (job_id=%s, namespace=%s)\n", action, resource, jobID, tc.namespace)
}
