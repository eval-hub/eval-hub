// Package worker implements the isolated collection-generation workload process.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/llm"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation/orchestration"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
)

const (
	DefaultJobSpecPath = "/meta/job.json"
	maxJobSpecBytes    = 1 << 20
	maxEventBytes      = 4096
	workerTimeout      = 15 * time.Second
	podNamespacePath   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// jobSpec is the subset of the shared runtime JobSpec used by the generation
// worker. Its reserved provider and benchmark identify the single work item
// whose lifecycle the worker reports through the evaluation events route.
type jobSpec struct {
	JobID          string         `json:"id"`
	ProviderID     string         `json:"provider_id"`
	BenchmarkID    string         `json:"benchmark_id"`
	BenchmarkIndex int            `json:"benchmark_index"`
	Model          *api.ModelRef  `json:"model"`
	Parameters     map[string]any `json:"parameters"`
	CallbackURL    *string        `json:"callback_url"`
}

// Run reads the trusted mounted job specification and runs the workload.
func Run(ctx context.Context, specPath string, client *http.Client) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if specPath == "" {
		specPath = DefaultJobSpecPath
	}
	file, err := openSpec(specPath)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxJobSpecBytes+1))
	closeErr := file.Close()
	if err != nil {
		return errors.New("read generation job specification")
	}
	if closeErr != nil {
		return errors.New("close generation job specification")
	}
	if len(data) > maxJobSpecBytes {
		return errors.New("generation job specification exceeds the size limit")
	}
	return RunSpec(ctx, data, client)
}

// RunSpec validates the mounted job identity/configuration and sends only fixed,
// service-owned lifecycle events through the loopback sidecar.
func RunSpec(ctx context.Context, data []byte, client *http.Client) error {
	// RHAI-17753 supplies the deployment-owned safety client. Until it is
	// configured, the runner cannot send the goal to the model.
	return runSpec(ctx, data, client, nil)
}

func runSpec(ctx context.Context, data []byte, client *http.Client, guardrail orchestration.Guardrail) error {
	if len(data) == 0 || len(data) > maxJobSpecBytes {
		return errors.New("generation job specification is empty or exceeds the size limit")
	}
	var spec jobSpec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return errors.New("generation job specification is malformed")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("generation job specification must contain one JSON object")
	}
	snapshot, endpoint, err := validateSpec(spec)
	if err != nil {
		return err
	}
	tenant, err := readPodNamespace()
	if err != nil {
		return errors.New("read generation pod namespace")
	}
	sidecarClient := localClient(client)
	event := api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0, Status: api.StateRunning,
	}
	startErr := postEvent(ctx, sidecarClient, endpoint, tenant, event)

	// Always attempt to record terminal state, even if the first callback failed.
	event.Status = api.StateFailed
	if startErr == nil {
		modelHTTP := *sidecarClient
		modelHTTP.Timeout = snapshot.Limits.MaxDuration
		model, err := llm.NewClient(llm.Config{BaseURL: spec.Model.URL, HTTPClient: &modelHTTP})
		if err == nil {
			var sequence uint64
			runner := orchestration.Runner{
				Catalog: sidecarCatalog{client: sidecarClient, base: *endpoint, tenant: tenant},
				Model:   model, Guardrail: guardrail,
				Progress: func(ctx context.Context, phase, action string) error {
					sequence++
					progress := api.BenchmarkStatusEvent{
						ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0,
						Status: api.StateRunning,
						CollectionGenerationProgress: &api.CollectionGenerationProgressEvent{
							Sequence: sequence, Phase: phase, LastCompletedAction: action,
						},
					}
					return postEvent(ctx, sidecarClient, endpoint, tenant, progress)
				},
			}
			proposal, failure := runner.Run(ctx, snapshot)
			if failure != nil {
				event.AdditionalInfo = map[string]any{"collection_generation_error": failure}
			} else {
				event.Status = api.StateCompleted
				event.AdditionalInfo = map[string]any{"collection_generation_result": proposal}
			}
		}
	}
	if event.AdditionalInfo == nil {
		failure, _ := generation.SafeFailure("workload_failed", "")
		event.AdditionalInfo = map[string]any{"collection_generation_error": failure}
	}
	terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerTimeout)
	defer cancel()
	terminalErr := postEvent(terminalCtx, sidecarClient, endpoint, tenant, event)
	if startErr != nil {
		return fmt.Errorf("report generation start: %w", startErr)
	}
	return terminalErr
}

func localClient(client *http.Client) *http.Client {
	local := http.Client{Timeout: workerTimeout, Transport: &http.Transport{}}
	if client != nil {
		local = *client
		if local.Transport == nil {
			local.Transport = &http.Transport{}
		}
		if transport, ok := local.Transport.(*http.Transport); ok {
			transport = transport.Clone()
			transport.Proxy = nil
			local.Transport = transport
		}
	}
	local.Jar = nil
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &local
}

func validateSpec(spec jobSpec) (generation.JobConfig, *url.URL, error) {
	if _, err := uuid.Parse(spec.JobID); err != nil {
		return generation.JobConfig{}, nil, errors.New("generation job ID is invalid")
	}
	if spec.ProviderID != generation.ProviderID || spec.BenchmarkID != generation.BenchmarkID || spec.BenchmarkIndex != 0 {
		return generation.JobConfig{}, nil, errors.New("job does not identify the collection-generation workload")
	}
	if spec.Model == nil || spec.CallbackURL == nil {
		return generation.JobConfig{}, nil, errors.New("generation model and callback are required")
	}
	snapshot, err := generation.ConfigFromParameters(spec.Parameters)
	if err != nil {
		return generation.JobConfig{}, nil, errors.New("generation snapshot is invalid")
	}
	if spec.Model.Name != snapshot.Request.Model.Name || !isLoopbackURL(spec.Model.URL) || spec.Model.CardURL != "" || len(spec.Model.Parameters) > 0 {
		return generation.JobConfig{}, nil, errors.New("job model does not match the trusted generation connection")
	}
	if !sameSecretReference(spec.Model.Auth, snapshot.Request.Model.Auth) {
		return generation.JobConfig{}, nil, errors.New("job model Secret reference does not match the trusted generation request")
	}
	callback, err := parseCallbackURL(*spec.CallbackURL, spec.JobID)
	if err != nil {
		return generation.JobConfig{}, nil, err
	}
	return snapshot, callback, nil
}

func sameSecretReference(actual, expected *api.ModelAuth) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return actual.SecretRef == expected.SecretRef
}

func isLoopbackURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "http" && parsed.User == nil && parsed.Fragment == "" &&
		parsed.Port() != "" && isLoopbackHost(parsed.Hostname())
}

func parseCallbackURL(raw, jobID string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.Port() == "" || !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("generation callback must use the local sidecar URL")
	}
	parsed.Path = "/api/v1/evaluations/jobs/" + url.PathEscape(jobID) + "/events"
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func postEvent(ctx context.Context, client *http.Client, endpoint *url.URL, tenant string, event api.BenchmarkStatusEvent) error {
	data, err := json.Marshal(api.StatusEvent{BenchmarkStatusEvent: &event})
	limit := maxEventBytes
	if event.Status == api.StateCompleted {
		limit += generation.MaxResultBytes
	}
	if err != nil || len(data) > limit {
		return errors.New("generation event is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return errors.New("create generation event request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tenant", tenant)
	response, err := client.Do(request)
	if err != nil {
		return errors.New("send generation event through the local sidecar")
	}
	_, copyErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	closeErr := response.Body.Close()
	if copyErr != nil || closeErr != nil {
		return errors.New("read generation event response")
	}
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("generation event endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

// openSpec is replaceable in tests. Production reads only the fixed mounted file
// beneath /meta, using a rooted open to prevent path traversal through configuration.
var openSpec = func(path string) (io.ReadCloser, error) {
	if path != DefaultJobSpecPath {
		return nil, errors.New("generation job specification path is not allowed")
	}
	return os.OpenInRoot("/meta", "job.json")
}

var readPodNamespace = func() (string, error) {
	data, err := os.ReadFile(podNamespacePath)
	if err != nil {
		return "", err
	}
	namespace := strings.TrimSpace(string(data))
	if namespace == "" || strings.ContainsAny(namespace, "\r\n") {
		return "", errors.New("pod namespace is empty or invalid")
	}
	return namespace, nil
}
