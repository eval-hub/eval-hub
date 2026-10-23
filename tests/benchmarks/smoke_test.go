package benchmarks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	validator "github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

// These tests intentionally use the live provider catalog, not the checked-in YAML.
// Fixture coverage of the checked-in catalog is checked separately in config_test.go.
func TestBenchmarkSmoke(t *testing.T) {
	if os.Getenv("BENCHMARK_SMOKE") != "1" {
		t.Skip("set BENCHMARK_SMOKE=1 or use make test-benchmarks to run live evaluations")
	}
	baseURL := requiredEnv(t, "SERVER_URL")
	model := api.ModelRef{Name: requiredEnv(t, "MODEL_NAME"), URL: requiredEnv(t, "MODEL_URL")}
	if secret := os.Getenv("MODEL_AUTH_SECRET_REF"); secret != "" {
		model.Auth = &api.ModelAuth{SecretRef: secret}
	}
	httpClient, err := newHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	client := evalhubclient.NewClient(baseURL).WithHTTPClient(httpClient).WithMaxRetries(0)
	validate, err := validation.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	providers, err := discover(ctx, client)
	cancel()
	if err != nil {
		t.Fatalf("discover providers: %v", err)
	}
	fixtureRoot := os.Getenv("BENCHMARK_CONFIG_DIR")
	if fixtureRoot == "" {
		fixtureRoot = "testdata"
	}
	poll := 2 * time.Second
	if value := os.Getenv("BENCHMARK_POLL_INTERVAL"); value != "" {
		poll, err = positiveDuration(value)
		if err != nil {
			t.Fatalf("BENCHMARK_POLL_INTERVAL: %v", err)
		}
	}
	var configured, skipped, submitted atomic.Int64
	t.Cleanup(func() {
		t.Logf("benchmarks: configured=%d skipped=%d submitted=%d", configured.Load(), skipped.Load(), submitted.Load())
		if submitted.Load() == 0 {
			t.Error("no configured benchmarks were submitted")
		}
	})
	// A single level of parallel subtests gives one global -parallel bound even
	// when benchmarks belong to different providers.
	for _, provider := range providers {
		for _, benchmark := range provider.Benchmarks {
			t.Run(provider.Resource.ID+"/"+benchmark.ID, func(t *testing.T) {
				t.Parallel()
				path := fixturePath(fixtureRoot, provider.Resource.ID, benchmark.ID)
				fixture, err := loadFixture(path, smokeEnv)
				if errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing JSON fixture for provider %q benchmark %q: expected %s; run make generate-benchmark-configs from the repository root or add a reviewed fixture at this path", provider.Resource.ID, benchmark.ID, path)
				}
				if errors.Is(err, errMissingInput) {
					skipped.Add(1)
					t.Skipf("fixture %s: %v; supply the missing input to run this benchmark", path, err)
				}
				if err != nil {
					t.Fatalf("fixture %s: %v", path, err)
				}
				configured.Add(1)
				if fixture.Unbounded && os.Getenv("BENCHMARK_ALLOW_UNBOUNDED") != "1" {
					t.Fatal("adapter has no sample limit; review fixture notes and set BENCHMARK_ALLOW_UNBOUNDED=1 explicitly")
				}
				if fixture.Notes != "" {
					t.Log(fixture.Notes)
				}
				jobConfig := fixture.jobConfig(provider.Resource.ID, benchmark.ID, model)
				if err := validate.Struct(jobConfig); err != nil {
					var fields validator.ValidationErrors
					if errors.As(err, &fields) {
						missingOnly := true
						for _, field := range fields {
							if field.Tag() != "required" {
								missingOnly = false
							}
						}
						if missingOnly {
							skipped.Add(1)
							t.Skipf("fixture %s: missing required request inputs: %v; complete these fields to run this benchmark", path, err)
						}
					}
					t.Fatalf("invalid evaluation request: %v", err)
				}
				timeout, _ := positiveDuration(fixture.Timeout) // validated by loadFixture
				ctx, cancel := context.WithTimeout(t.Context(), timeout)
				defer cancel()
				started := time.Now()
				job, err := client.WithContext(ctx).CreateJob(jobConfig)
				if err != nil {
					t.Fatalf("submit: %v", err)
				}
				if job.Resource.ID == "" {
					t.Fatal("create response has no job ID")
				}
				submitted.Add(1)
				id := job.Resource.ID
				t.Cleanup(func() {
					// t.Context() is cancelled before cleanup; use an independent deadline.
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := cleanupJob(cleanupCtx, httpClient, baseURL, id); err != nil {
						t.Errorf("cleanup job %s: %v", id, err)
					}
				})
				t.Logf("job=%s provider=%s benchmark=%s", id, provider.Resource.ID, benchmark.ID)
				if err := waitForCompletion(ctx, client, id, provider.Resource.ID, benchmark.ID, poll); err != nil {
					t.Fatalf("job=%s elapsed=%s: %v", id, time.Since(started).Round(time.Second), err)
				}
				t.Logf("job=%s completed in %s", id, time.Since(started).Round(time.Second))
			})
		}
	}
}

// smokeEnv accepts the aliases exported by the Shepard benchmark pipeline.
// The primary name wins when it is nonempty.
func smokeEnv(key string) (string, bool) {
	names := []string{key}
	switch key {
	case "EVALHUB_TOKEN":
		names = append(names, "AUTH_TOKEN")
	case "JUDGE_MODEL_NAME":
		names = append(names, "JUDGE_MODEL")
	case "JUDGE_BASE_URL":
		names = append(names, "JUDGE_API_BASE", "JUDGE_PROXY_URL")
	}
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	return "", false
}

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		t.Fatalf("%s is required for an explicitly enabled smoke run", key)
	}
	return value
}

type fixture struct {
	Parameters     map[string]any               `json:"parameters"`
	Timeout        string                       `json:"timeout"`
	TestDataRef    *api.TestDataRef             `json:"test_data_ref,omitempty"`
	TestDataRefEnv string                       `json:"test_data_ref_env,omitempty"`
	HardwareConfig *api.BenchmarkHardwareConfig `json:"hardware_config,omitempty"`
	Model          *api.ModelRef                `json:"model,omitempty"`
	Experiment     bool                         `json:"experiment,omitempty"`
	ExperimentName string                       `json:"experiment_name,omitempty"`
	Unbounded      bool                         `json:"unbounded,omitempty"`
	Notes          string                       `json:"notes,omitempty"`
}

// PathEscape encodes '/' and '%' so IDs cannot create directory traversals.
// Dot-only IDs need special handling because PathEscape leaves dots unchanged.
func fileID(id string) string {
	return strings.ReplaceAll(url.PathEscape(id), ".", "%2E")
}

func fixturePath(root, provider, benchmark string) string {
	return filepath.Join(root, fileID(provider), fileID(benchmark)+".json")
}

var errMissingInput = errors.New("missing required input")

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandValues(value any, lookup func(string) (string, bool)) (any, error) {
	switch v := value.(type) {
	case string:
		var missing string
		result := envPattern.ReplaceAllStringFunc(v, func(match string) string {
			key := match[2 : len(match)-1]
			value, ok := lookup(key)
			if !ok || strings.TrimSpace(value) == "" {
				missing = key
			}
			return value
		})
		if missing != "" {
			return nil, fmt.Errorf("%w: environment variable %s must be nonempty", errMissingInput, missing)
		}
		return result, nil
	case map[string]any:
		for key, item := range v {
			expanded, err := expandValues(item, lookup)
			if err != nil {
				return nil, err
			}
			v[key] = expanded
		}
	case []any:
		for i, item := range v {
			expanded, err := expandValues(item, lookup)
			if err != nil {
				return nil, err
			}
			v[i] = expanded
		}
	}
	return value, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON document")
	}
	return nil
}

func loadFixture(path string, lookup func(string) (string, bool)) (fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fixture{}, err
	}
	var raw map[string]any
	if err := decodeStrict(data, &raw); err != nil {
		return fixture{}, err
	}
	if _, err := expandValues(raw, lookup); err != nil {
		return fixture{}, err
	}
	data, err = json.Marshal(raw)
	if err != nil {
		return fixture{}, err
	}
	var f fixture
	if err := decodeStrict(data, &f); err != nil {
		return f, err
	}
	if _, ok := raw["parameters"]; !ok {
		return f, fmt.Errorf("%w: fixture field parameters must be a JSON object", errMissingInput)
	}
	if f.Parameters == nil {
		return f, errors.New("parameters must be a JSON object")
	}
	if f.ExperimentName != "" && strings.TrimSpace(f.ExperimentName) == "" {
		return f, errors.New("experiment_name must not be blank")
	}
	if strings.TrimSpace(f.Timeout) == "" {
		return f, fmt.Errorf("%w: fixture field timeout must be a positive Go duration (for example 5m)", errMissingInput)
	}
	if _, err := positiveDuration(f.Timeout); err != nil {
		return f, fmt.Errorf("timeout: %w", err)
	}
	if f.TestDataRefEnv != "" {
		if f.TestDataRef != nil {
			return f, errors.New("use either test_data_ref or test_data_ref_env")
		}
		value, ok := lookup(f.TestDataRefEnv)
		if !ok || strings.TrimSpace(value) == "" {
			return f, fmt.Errorf("%w: environment variable %s must contain a TestDataRef JSON object for a tiny staged dataset", errMissingInput, f.TestDataRefEnv)
		}
		if err := decodeStrict([]byte(value), &f.TestDataRef); err != nil {
			return f, fmt.Errorf("%s: invalid TestDataRef: %w", f.TestDataRefEnv, err)
		}
		if f.TestDataRef == nil {
			return f, errors.New("test data reference cannot be null")
		}
	}
	return f, nil
}

func positiveDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("expected a positive duration, got %q", value)
	}
	return duration, nil
}

func (f fixture) jobConfig(provider, benchmark string, model api.ModelRef) api.EvaluationJobConfig {
	if f.Model != nil {
		model = *f.Model
	}
	name := "benchmark-smoke-" + uuid.NewString()
	job := api.EvaluationJobConfig{
		Name: name, Model: &model, Tags: []string{"benchmark-smoke"},
		Benchmarks: []api.EvaluationBenchmarkConfig{{
			Ref: api.Ref{ID: benchmark}, ProviderID: provider, Parameters: f.Parameters,
			TestDataRef: f.TestDataRef, HardwareConfig: f.HardwareConfig,
		}},
	}
	if f.ExperimentName != "" {
		job.Experiment = &api.ExperimentConfig{Name: f.ExperimentName}
	} else if f.Experiment {
		job.Experiment = &api.ExperimentConfig{Name: name}
	}
	return job
}

func discover(ctx context.Context, client *evalhubclient.Client) ([]api.ProviderResource, error) {
	const limit = 100
	var providers []api.ProviderResource
	seen := make(map[string]bool)
	for offset := 0; ; {
		page, err := client.WithContext(ctx).ListProviders(evalhubclient.WithLimit(limit), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			id := item.Resource.ID
			if id == "" || seen[id] {
				return nil, fmt.Errorf("empty or repeated provider ID %q during pagination", id)
			}
			seen[id] = true
			provider, err := client.WithContext(ctx).GetProvider(id)
			if err != nil {
				return nil, fmt.Errorf("provider %s: %w", id, err)
			}
			if provider.Resource.ID != id {
				return nil, fmt.Errorf("provider %s returned mismatched ID %q", id, provider.Resource.ID)
			}
			benchmarks := make(map[string]bool)
			for _, b := range provider.Benchmarks {
				if b.ID == "" || benchmarks[b.ID] {
					return nil, fmt.Errorf("provider %s: empty or duplicate benchmark ID %q", id, b.ID)
				}
				benchmarks[b.ID] = true
			}
			providers = append(providers, *provider)
		}
		offset += len(page.Items)
		if page.Next == nil && offset >= page.TotalCount {
			break
		}
		if len(page.Items) == 0 {
			return nil, errors.New("provider pagination did not advance")
		}
	}
	return providers, nil
}

func waitForCompletion(ctx context.Context, client *evalhubclient.Client, id, provider, benchmark string, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	last := "no status received"
	for {
		job, err := client.WithContext(ctx).GetJob(id)
		if err != nil {
			return fmt.Errorf("poll: %w; last status: %s", err, last)
		}
		if job.Status != nil {
			diagnostic, _ := json.Marshal(job.Status)
			last = string(diagnostic)
			if job.Status.State.IsTerminalState() {
				if job.Status.State != api.OverallStateCompleted {
					return fmt.Errorf("evaluation did not complete: %s", last)
				}
				completed := false
				for _, b := range job.Status.Benchmarks {
					if b.ID == benchmark && b.ProviderID == provider && b.Status == api.StateCompleted {
						completed = true
					}
				}
				if !completed {
					return fmt.Errorf("matching benchmark is not completed: %s", last)
				}
				if job.Results != nil {
					for _, result := range job.Results.Benchmarks {
						if result.ID == benchmark && result.ProviderID == provider {
							return nil
						}
					}
				}
				return errors.New("completed job has no matching benchmark result")
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w; last status: %s", ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

type identityTransport struct {
	base                http.RoundTripper
	token, tenant, user string
}

func (tr identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if tr.token != "" {
		req.Header.Set("Authorization", "Bearer "+tr.token)
	}
	if tr.tenant != "" {
		req.Header.Set("X-Tenant", tr.tenant)
	}
	if tr.user != "" {
		req.Header.Set("X-User", tr.user)
	}
	return tr.base.RoundTrip(req)
}

func newHTTPClient() (*http.Client, error) {
	baseURL, err := url.Parse(os.Getenv("SERVER_URL"))
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.RawQuery != "" || baseURL.Fragment != "" || baseURL.User != nil {
		return nil, errors.New("SERVER_URL must be an HTTP(S) base URL without credentials, query, or fragment")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if path := os.Getenv("EVALHUB_CA_CERT_PATH"); path != "" {
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("EVALHUB_CA_CERT_PATH contains no PEM certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	token, _ := smokeEnv("EVALHUB_TOKEN")
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: identityTransport{base: transport, token: token, tenant: os.Getenv("X_TENANT"), user: os.Getenv("X_USER")},
		// Do not forward identity headers to redirects, including a different host.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

func cleanupJob(ctx context.Context, client *http.Client, baseURL, id string) error {
	// Hard deletion also removes runtime resources; it does not leave cancelled
	// evaluation records behind. Use the same authenticated transport as discovery.
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/evaluations/jobs/" + url.PathEscape(id) + "?hard_delete=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("hard delete returned HTTP %d", response.StatusCode)
	}
	return nil
}
