package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	mockTemplateLabel   = "__MOCK_TEMPLATE"
	jobIDLabel          = "job_id"
	mockTemplatesDir    = "tests/mock_templates"
	defaultMockTemplate = "success_default.json"
	eventsBaseURL       = "http://localhost:8080/api/v1/evaluations/jobs"
)

// MockKubernetesHelper implements KubernetesHelperInterface for local/testing.
// ConfigMap methods are no-ops and return success. CreateJob, when the job has
// label __MOCK_TEMPLATE, loads the named file and POSTs benchmark_status_events
// to the events API with the configured delay between each.
var _ KubernetesHelperInterface = (*MockKubernetesHelper)(nil)

type MockKubernetesHelper struct {
	logger *slog.Logger
}

// NewMockKubernetesHelper returns a mock helper that does not call the cluster.
func NewMockKubernetesHelper(logger *slog.Logger) *MockKubernetesHelper {
	return &MockKubernetesHelper{logger: logger}
}

// ListConfigMaps implements [KubernetesHelperInterface].
func (h *MockKubernetesHelper) ListConfigMaps(ctx context.Context, namespace string, labelSelector string) ([]corev1.ConfigMap, error) {
	//return an empty list of configmaps
	return []corev1.ConfigMap{}, nil
}

// ListJobs implements [KubernetesHelperInterface].
func (h *MockKubernetesHelper) ListJobs(ctx context.Context, namespace string, labelSelector string) ([]batchv1.Job, error) {
	//return an empty list of jobs
	return []batchv1.Job{}, nil
}

func (h *MockKubernetesHelper) CreateConfigMap(
	_ context.Context,
	_, _ string,
	_ map[string]string,
	_ *CreateConfigMapOptions,
) (*corev1.ConfigMap, error) {
	return &corev1.ConfigMap{}, nil
}

func (h *MockKubernetesHelper) DeleteConfigMap(_ context.Context, _, _ string) error {
	return nil
}

func (h *MockKubernetesHelper) SetConfigMapOwner(_ context.Context, _, _ string, _ metav1.OwnerReference) error {
	return nil
}

func (h *MockKubernetesHelper) DeleteJob(_ context.Context, _, _ string, _ metav1.DeleteOptions) error {
	return nil
}

func (h *MockKubernetesHelper) CreateJob(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	if job == nil {
		return nil, fmt.Errorf("mock CreateJob: job must not be nil")
	}
	if job.Spec.Template.Labels == nil {
		return job, nil
	}
	h.logger.Debug("In Mock create job for job", "job", job.Spec.Template.Labels[jobIDLabel])
	templatePath, _ := job.Spec.Template.Labels[mockTemplateLabel]
	if strings.TrimSpace(templatePath) == "" {
		templatePath = defaultMockTemplate
	}
	jobID, ok := job.Spec.Template.Labels[jobIDLabel]
	if !ok || jobID == "" {
		return nil, fmt.Errorf("job_id label missing on job %s/%s", job.Namespace, job.Name)
	}

	raw, err := readMockTemplateFile(templatePath)
	if err != nil {
		return nil, err
	}
	var tpl struct {
		DelaySeconds          int              `json:"delay_seconds"`
		SimulateKubeError     map[string]any   `json:"simulate_kube_error,omitempty"`
		BenchmarkStatusEvents []map[string]any `json:"benchmark_status_events"`
	}
	if err := json.Unmarshal(raw, &tpl); err != nil {
		return nil, fmt.Errorf("mock template JSON: %w", err)
	}
	if len(tpl.SimulateKubeError) > 0 {
		msg := extractSimulateErrorMessage(tpl.SimulateKubeError)
		return nil, fmt.Errorf("%s", msg)
	}
	delay := time.Duration(tpl.DelaySeconds) * time.Second
	providerID := job.Spec.Template.Labels["provider_id"]
	benchmarkID := job.Spec.Template.Labels["benchmark_id"]

	for i, evt := range tpl.BenchmarkStatusEvents {
		statusEvt := buildStatusEvent(providerID, benchmarkID, evt)
		body, err := json.Marshal(statusEvt)
		if err != nil {
			return nil, fmt.Errorf("mock event %d: %w", i, err)
		}
		url := eventsBaseURL + "/" + jobID + "/events"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("mock POST %s: %w", url, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("mock POST %s: status %d", url, resp.StatusCode)
		}
		if i < len(tpl.BenchmarkStatusEvents)-1 && delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return job, nil
}

func extractSimulateErrorMessage(simulateKubeError map[string]any) string {
	em, ok := simulateKubeError["error_message"].(map[string]any)
	if !ok {
		return "simulated Kubernetes error"
	}
	msg, _ := em["message"].(string)
	if msg != "" {
		return msg
	}
	return "simulated Kubernetes error"
}

func readMockTemplateFile(name string) ([]byte, error) {
	// Prevent directory traversal from user-supplied label values.
	name = filepath.Base(name)
	tries := []string{filepath.Join(mockTemplatesDir, name), name}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		withJSON := name + ".json"
		tries = append(tries, filepath.Join(mockTemplatesDir, withJSON), withJSON)
	}
	for _, p := range tries {
		raw, err := os.ReadFile(p)
		if err == nil {
			return raw, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("mock template file not found: %s (tried %s and %s)", name, filepath.Join(mockTemplatesDir, name), name)
}

func buildStatusEvent(providerID, benchmarkID string, evt map[string]any) *api.StatusEvent {
	status := api.StatePending
	if s, ok := evt["status"].(string); ok && s != "" {
		status = api.State(s)
	}
	ev := &api.BenchmarkStatusEvent{
		ProviderID: providerID,
		ID:         benchmarkID,
		Status:     status,
	}
	if m, ok := evt["metrics"].(map[string]any); ok {
		ev.Metrics = m
	}
	if a, ok := evt["artifacts"].(map[string]any); ok {
		ev.Artifacts = a
	}
	if em, ok := evt["error_message"].(map[string]any); ok {
		if msg, _ := em["message"].(string); msg != "" {
			ev.ErrorMessage = &api.MessageInfo{Message: msg}
			if code, _ := em["message_code"].(string); code != "" {
				ev.ErrorMessage.MessageCode = code
			}
		}
	}
	return &api.StatusEvent{BenchmarkStatusEvent: ev}
}
