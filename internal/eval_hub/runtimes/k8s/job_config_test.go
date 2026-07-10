package k8s

import (
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildJobConfigDefaults(t *testing.T) {
	callbackURL := "http://localhost:8080"
	benchmark := api.EvaluationBenchmarkConfig{
		Ref: api.Ref{ID: "bench-1"},
		Parameters: map[string]any{
			"num_examples": 50,
			"max_tokens":   128,
			"temperature":  0.2,
		},
	}
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				benchmark,
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.jobID != "job-123" {
		t.Fatalf("expected job id to be set")
	}
	if cfg.adapterImage != "adapter:latest" {
		t.Fatalf("expected adapter image to be set")
	}
	if cfg.adapterPullPolicy != corev1.PullIfNotPresent {
		t.Fatalf("expected default adapterPullPolicy to be IfNotPresent, got %q", cfg.adapterPullPolicy)
	}
	if cfg.namespace == "" {
		t.Fatalf("expected namespace to be set")
	}
	if cfg.cpuRequest != defaultCPURequest {
		t.Fatalf("expected cpu request %s, got %s", defaultCPURequest, cfg.cpuRequest)
	}
	if cfg.memoryRequest != defaultMemoryRequest {
		t.Fatalf("expected memory request %s, got %s", defaultMemoryRequest, cfg.memoryRequest)
	}
	if cfg.cpuLimit != defaultCPULimit {
		t.Fatalf("expected cpu limit %s, got %s", defaultCPULimit, cfg.cpuLimit)
	}
	if cfg.memoryLimit != defaultMemoryLimit {
		t.Fatalf("expected memory limit %s, got %s", defaultMemoryLimit, cfg.memoryLimit)
	}

	spec := cfg.jobSpec
	jobID := spec.JobID
	if jobID != "job-123" {
		t.Fatalf("expected job spec json id to be %q, got %v", "job-123", jobID)
	}
	benchmarkID := spec.BenchmarkID
	if benchmarkID != "bench-1" {
		t.Fatalf("expected job spec json benchmark_id to be %q, got %v", "bench-1", benchmarkID)
	}
	numExamples := spec.NumExamples
	if numExamples == nil || *numExamples != 50 {
		t.Fatalf("expected job spec json num_examples to be %d, got %v", 50, numExamples)
	}
	parameters := spec.Parameters

	if _, exists := parameters["num_examples"]; exists {
		t.Fatalf("expected parameters not to include num_examples")
	}
	if parameters["max_tokens"] != 128 {
		t.Fatalf("expected parameters.max_tokens to be %d, got %v", 128, parameters["max_tokens"])
	}
	if parameters["temperature"] != 0.2 {
		t.Fatalf("expected parameters.temperature to be 0.2, got %v", parameters["temperature"])
	}
	callback := spec.CallbackURL
	if callback == nil || *callback != callbackURL {
		t.Fatalf("expected job spec json callback_url to be %q, got %v", callbackURL, callback)
	}
}

func TestBuildJobConfigRejectsInvalidSidecarPort(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-123"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}
	serviceConfig := &config.Config{
		Sidecar: &config.SidecarConfig{Port: 70000},
	}

	_, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, serviceConfig, nil)
	if err == nil {
		t.Fatal("buildJobConfig() = nil, want sidecar port error")
	}
	if !strings.Contains(err.Error(), "sidecar port") {
		t.Fatalf("buildJobConfig() error = %v, want sidecar port error", err)
	}
}

func TestBuildJobConfigUsesValidSidecarPort(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-123"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}
	serviceConfig := &config.Config{
		Sidecar: &config.SidecarConfig{Port: 9090},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, serviceConfig, nil)
	if err != nil {
		t.Fatalf("buildJobConfig() = %v, want nil error", err)
	}
	if cfg.sidecarBaseURL != "http://localhost:9090" {
		t.Fatalf("sidecarBaseURL = %q, want http://localhost:9090", cfg.sidecarBaseURL)
	}
}

func TestBuildJobConfigModelAuthSecretRefPresent(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-789"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
				Auth: &api.ModelAuth{SecretRef: "my-secret"},
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.modelAuthSecretRef != "my-secret" {
		t.Fatalf("expected modelAuthSecretRef %q, got %q", "my-secret", cfg.modelAuthSecretRef)
	}
}

func TestBuildJobConfigModelAuthSecretRefEmptyWhenNil(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-790"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.modelAuthSecretRef != "" {
		t.Fatalf("expected modelAuthSecretRef to be empty, got %q", cfg.modelAuthSecretRef)
	}
}

func TestBuildJobConfigTestDataS3(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-901"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
					TestDataRef: &api.TestDataRef{
						S3: &api.S3TestDataRef{
							Bucket:    "bucket-1",
							Key:       "/a/b",
							SecretRef: "s3-secret",
						},
					},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.testDataS3.bucket != "bucket-1" {
		t.Fatalf("expected testDataS3Bucket %q, got %q", "bucket-1", cfg.testDataS3.bucket)
	}
	if cfg.testDataS3.key != "/a/b" {
		t.Fatalf("expected testDataS3Key %q, got %q", "/a/b", cfg.testDataS3.key)
	}
	if cfg.testDataS3.secretRef != "s3-secret" {
		t.Fatalf("expected testDataS3SecretRef %q, got %q", "s3-secret", cfg.testDataS3.secretRef)
	}
}

func TestBuildJobConfigAllowsNumExamplesOnly(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-456"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					Parameters: map[string]any{"num_examples": 10},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("expected no error for num_examples-only parameters, got %v", err)
	}

	spec := cfg.jobSpec
	numExamples := spec.NumExamples
	if numExamples == nil || *numExamples != 10 {
		t.Fatalf("expected job spec json num_examples to be %d, got %v", 10, numExamples)
	}

	parameters := spec.Parameters

	if len(parameters) != 0 {
		t.Fatalf("expected empty parameters, got %v", parameters)
	}
}

func TestBuildJobConfigMissingRuntime(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{},
		},
	}

	_, err := buildJobConfig(evaluation, provider, &api.EvaluationBenchmarkConfig{}, 0, nil, nil)
	if err == nil {
		t.Fatalf("expected error for missing runtime")
	}
}

func TestBuildJobConfigMissingAdapterImage(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{},
		},
	}

	_, err := buildJobConfig(evaluation, provider, nil, 0, nil, nil)
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobConfigAllowsEmptyBenchmarkConfig(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("expected no error for empty parameters, got %v", err)
	}

	spec := cfg.jobSpec
	parameters := spec.Parameters

	if len(parameters) != 0 {
		t.Fatalf("expected empty parameters, got %v", parameters)
	}
}

func TestBuildJobConfigWithOCIExports(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-oci"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					Parameters: map[string]any{},
				},
			},
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       "quay.io",
						OCIRepository: "my-org/my-repo",
						OCITag:        "eval-123",
					},
					K8s: &api.OCIConnectionConfig{
						Connection: "my-pull-secret",
					},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}

	// ociCredentialsSecret should be extracted from k8s.connection
	if cfg.ociCredentialsSecret != "my-pull-secret" {
		t.Fatalf("expected ociCredentialsSecret %q, got %q", "my-pull-secret", cfg.ociCredentialsSecret)
	}

	// jobSpecJSON should contain coordinates but NOT k8s connection

	spec := cfg.jobSpec
	exports := spec.Exports
	if exports == nil {
		t.Fatalf("expected exports object, got %v", exports)
	}
	oci := exports.OCI
	if oci == nil {
		t.Fatalf("expected exports.oci, got %v", oci)
	}
	coords := oci.Coordinates

	if coords.OCIHost != "quay.io" {
		t.Fatalf("expected oci_host %q, got %v", "quay.io", coords.OCIHost)
	}
	if coords.OCIRepository != "my-org/my-repo" {
		t.Fatalf("expected oci_repository %q, got %v", "my-org/my-repo", coords.OCIRepository)
	}

}

func TestNumExamplesFromParametersTypes(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]any
		want       *int
	}{
		{"nil map", nil, nil},
		{"missing", map[string]any{"other": 1}, nil},
		{"int", map[string]any{"num_examples": 3}, intPtr(3)},
		{"int32", map[string]any{"num_examples": int32(4)}, intPtr(4)},
		{"int64", map[string]any{"num_examples": int64(5)}, intPtr(5)},
		{"float64", map[string]any{"num_examples": float64(6)}, intPtr(6)},
		{"invalid", map[string]any{"num_examples": "bad"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shared.NumExamplesFromParameters(tt.parameters)
			if tt.want == nil && got != nil {
				t.Fatalf("expected nil, got %v", *got)
			}
			if tt.want != nil && (got == nil || *got != *tt.want) {
				if got == nil {
					t.Fatalf("expected %d, got nil", *tt.want)
				}
				t.Fatalf("expected %d, got %d", *tt.want, *got)
			}
		})
	}
}

func TestCopyParamsCreatesCopy(t *testing.T) {
	original := map[string]any{"num_examples": 1, "temp": 0.2}
	copied := shared.CopyParams(original)
	if len(copied) != len(original) {
		t.Fatalf("expected copy size %d, got %d", len(original), len(copied))
	}
	copied["temp"] = 0.3
	if original["temp"] == copied["temp"] {
		t.Fatalf("expected copy to be independent of original")
	}
}

func TestResolveNamespaceUsesConfigured(t *testing.T) {
	ns := resolveNamespace("my-tenant")
	if ns != "my-tenant" {
		t.Fatalf("expected %q, got %q", "my-tenant", ns)
	}
}

func TestResolveNamespaceEmptyFallsBack(t *testing.T) {
	ns := resolveNamespace("")
	if ns == "" {
		t.Fatalf("expected non-empty fallback namespace")
	}
}

func TestBuildJobConfigUsesTenantNamespace(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-tenant", Tenant: "team-a"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.namespace != "team-a" {
		t.Fatalf("expected namespace %q, got %q", "team-a", cfg.namespace)
	}
}

func TestBuildJobConfigEmptyTenantFallsBack(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-no-tenant"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.namespace == "" {
		t.Fatalf("expected non-empty fallback namespace when tenant is empty")
	}
}

func TestBuildJobConfigKueueQueueNameWhenSpecified(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-kueue"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
			Queue: &api.QueueConfig{
				Kind: "kueue",
				Name: "my-queue",
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.queueKind != "kueue" || cfg.queueName != "my-queue" {
		t.Fatalf("expected queueKind kueue and queueName my-queue, got kind %q name %q", cfg.queueKind, cfg.queueName)
	}
}

func TestBuildJobConfigKueueQueueNameWhenNoQueue(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-no-queue"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.queueKind != "" || cfg.queueName != "" {
		t.Fatalf("expected empty queue kind and name when no queue specified, got kind %q name %q", cfg.queueKind, cfg.queueName)
	}
}

func TestBuildJobConfigKueueQueueNameIgnoresNonKueueKind(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-other-kind"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
			Queue: &api.QueueConfig{
				Kind: "other",
				Name: "my-queue",
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.queueKind != "other" || cfg.queueName != "my-queue" {
		t.Fatalf("expected queueKind other and queueName my-queue, got kind %q name %q", cfg.queueKind, cfg.queueName)
	}
}

func TestBuildJobConfigBenchmarkIndexPropagated(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-idx"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "http://model", Name: "m"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "b0"}},
				{Ref: api.Ref{ID: "b1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "p"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{K8s: &api.K8sRuntime{Image: "img:latest"}},
		},
	}

	for _, idx := range []int{0, 1, 5} {
		cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], idx, nil, nil)
		if err != nil {
			t.Fatalf("buildJobConfig(%d) error: %v", idx, err)
		}
		if cfg.benchmarkIndex != idx {
			t.Fatalf("expected benchmarkIndex %d, got %d", idx, cfg.benchmarkIndex)
		}
	}
}

func TestRewriteModelURLForSidecar(t *testing.T) {
	tests := []struct {
		name           string
		sidecarBaseURL string
		modelURL       string
		want           string
		wantErr        bool
	}{
		{
			name:           "preserves deep KServe path prefix",
			sidecarBaseURL: "http://localhost:8080",
			modelURL:       "http://llm-inference.apps.example.com/llm/llama-3-1-8b-instruct/v1",
			want:           "http://localhost:8080/llm/llama-3-1-8b-instruct/v1",
		},
		{
			name:           "model URL with no path — no path added",
			sidecarBaseURL: "http://localhost:8080",
			modelURL:       "http://llm-inference.apps.example.com",
			want:           "http://localhost:8080",
		},
		{
			name:           "model URL with /v1 only",
			sidecarBaseURL: "http://localhost:8080",
			modelURL:       "http://llm-inference.apps.example.com/v1",
			want:           "http://localhost:8080/v1",
		},
		{
			name:           "custom sidecar port preserved",
			sidecarBaseURL: "http://localhost:9090",
			modelURL:       "http://llm-inference.apps.example.com/llm/my-model/v1",
			want:           "http://localhost:9090/llm/my-model/v1",
		},
		{
			name:           "trailing slash on model URL stripped",
			sidecarBaseURL: "http://localhost:8080",
			modelURL:       "http://llm-inference.apps.example.com/llm/my-model/v1/",
			want:           "http://localhost:8080/llm/my-model/v1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteModelURLForSidecar(tc.sidecarBaseURL, tc.modelURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("rewriteModelURLForSidecar(%q, %q) = %q, want %q", tc.sidecarBaseURL, tc.modelURL, got, tc.want)
			}
		})
	}
}

func TestBuildJobConfigModelURLPreservesRealURLAndTargetURL(t *testing.T) {
	// buildJobConfig does NOT rewrite model.url — that happens in k8s_runtime.go
	// via rewriteModelURLForSidecar. Verify modelTargetURL holds the real upstream URL
	// and sidecarBaseURL is plain localhost for the runtime to use.
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-rewrite"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://llm-inference.apps.example.com/llm/llama-3-1-8b-instruct/v1",
				Name: "llama-3-1-8b-instruct",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "bench-1"}}},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{K8s: &api.K8sRuntime{Image: "adapter:latest"}},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig: %v", err)
	}

	// modelTargetURL must hold the real upstream URL for the sidecar proxy config.
	wantTargetURL := "http://llm-inference.apps.example.com/llm/llama-3-1-8b-instruct/v1"
	if cfg.modelTargetURL != wantTargetURL {
		t.Errorf("modelTargetURL = %q, want %q (real upstream)", cfg.modelTargetURL, wantTargetURL)
	}

	// sidecarBaseURL is plain localhost — the runtime rewrites model.url using this + model path.
	if cfg.sidecarBaseURL != "http://localhost:8080" {
		t.Errorf("sidecarBaseURL = %q, want http://localhost:8080", cfg.sidecarBaseURL)
	}
}

func intPtr(value int) *int {
	return &value
}

func TestResolveGPUConfig(t *testing.T) {
	tests := []struct {
		name      string
		gpu       *api.GPUConfig
		wantRes   string
		wantCount int
	}{
		{
			name:      "nil gpu config yields no GPU",
			gpu:       nil,
			wantRes:   "",
			wantCount: 0,
		},
		{
			name:      "zero count yields no GPU",
			gpu:       &api.GPUConfig{Resource: "nvidia.com/gpu", Count: 0},
			wantRes:   "",
			wantCount: 0,
		},
		{
			name:      "nvidia gpu with explicit resource",
			gpu:       &api.GPUConfig{Resource: "nvidia.com/gpu", Count: 2},
			wantRes:   "nvidia.com/gpu",
			wantCount: 2,
		},
		{
			name:      "amd gpu with explicit resource",
			gpu:       &api.GPUConfig{Resource: "amd.com/gpu", Count: 1},
			wantRes:   "amd.com/gpu",
			wantCount: 1,
		},
		{
			name:      "empty resource leaves resource empty",
			gpu:       &api.GPUConfig{Resource: "", Count: 1},
			wantRes:   "",
			wantCount: 1,
		},
		{
			name:      "whitespace-only resource leaves resource empty",
			gpu:       &api.GPUConfig{Resource: "  ", Count: 1},
			wantRes:   "",
			wantCount: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, count := resolveGPUConfig(tc.gpu)
			if res != tc.wantRes {
				t.Errorf("resource = %q, want %q", res, tc.wantRes)
			}
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}
		})
	}
}

func TestBuildJobConfigGPU(t *testing.T) {
	benchmark := api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "bench-1"}}
	newEvaluation := func(queue *api.QueueConfig) *api.EvaluationJobResource {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-gpu"}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Model:      api.ModelRef{URL: "http://model", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{benchmark},
				Queue:      queue,
			},
		}
	}
	gpuProvider := &api.ProviderResource{
		Resource: api.Resource{ID: "gpu-provider"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
					GPU: &api.GPUConfig{
						Resource:     "nvidia.com/gpu",
						Count:        2,
						NodeSelector: map[string]string{"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-80GB"},
					},
				},
			},
		},
	}
	cpuProvider := &api.ProviderResource{
		Resource: api.Resource{ID: "cpu-provider"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{Image: "adapter:latest"},
			},
		},
	}

	t.Run("gpu config and node_selector propagated when no queue", func(t *testing.T) {
		cfg, err := buildJobConfig(newEvaluation(nil), gpuProvider, &benchmark, 0, nil, nil)
		if err != nil {
			t.Fatalf("buildJobConfig: %v", err)
		}
		if cfg.gpuResource != "nvidia.com/gpu" {
			t.Errorf("gpuResource = %q, want nvidia.com/gpu", cfg.gpuResource)
		}
		if cfg.gpuCount != 2 {
			t.Errorf("gpuCount = %d, want 2", cfg.gpuCount)
		}
		if cfg.nodeSelector["nvidia.com/gpu.product"] != "NVIDIA-H100-SXM5-80GB" {
			t.Errorf("nodeSelector = %v, want H100 label", cfg.nodeSelector)
		}
	})

	t.Run("node_selector suppressed when queue is set but GPU resources remain", func(t *testing.T) {
		q := &api.QueueConfig{Kind: "kueue", Name: "gpu-queue"}
		cfg, err := buildJobConfig(newEvaluation(q), gpuProvider, &benchmark, 0, nil, nil)
		if err != nil {
			t.Fatalf("buildJobConfig: %v", err)
		}
		// GPU resource requests must be preserved so Kueue can account for GPU quota.
		if cfg.gpuResource != "nvidia.com/gpu" {
			t.Errorf("gpuResource = %q, want nvidia.com/gpu (GPU must be set even with queue)", cfg.gpuResource)
		}
		if cfg.gpuCount != 2 {
			t.Errorf("gpuCount = %d, want 2 (GPU must be set even with queue)", cfg.gpuCount)
		}
		// nodeSelector must be suppressed — Kueue ResourceFlavors govern node selection.
		if len(cfg.nodeSelector) != 0 {
			t.Errorf("expected no nodeSelector when queue set, got %v", cfg.nodeSelector)
		}
		if cfg.queueName != "gpu-queue" {
			t.Errorf("queueName = %q, want gpu-queue", cfg.queueName)
		}
	})

	t.Run("nil gpu config leaves jobConfig without GPU", func(t *testing.T) {
		cfg, err := buildJobConfig(newEvaluation(nil), cpuProvider, &benchmark, 0, nil, nil)
		if err != nil {
			t.Fatalf("buildJobConfig: %v", err)
		}
		if cfg.gpuResource != "" || cfg.gpuCount != 0 {
			t.Errorf("expected no GPU for CPU-only provider, got resource=%q count=%d", cfg.gpuResource, cfg.gpuCount)
		}
		if len(cfg.nodeSelector) != 0 {
			t.Errorf("expected no nodeSelector for CPU-only provider, got %v", cfg.nodeSelector)
		}
	})
}

func TestResolveNodeSelector(t *testing.T) {
	tests := []struct {
		name string
		gpu  *api.GPUConfig
		want map[string]string
	}{
		{
			name: "nil gpu returns nil",
			gpu:  nil,
			want: nil,
		},
		{
			name: "empty node_selector returns nil",
			gpu:  &api.GPUConfig{Count: 1},
			want: nil,
		},
		{
			name: "node_selector copied",
			gpu:  &api.GPUConfig{Count: 1, NodeSelector: map[string]string{"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-80GB"}},
			want: map[string]string{"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-80GB"},
		},
		{
			name: "multiple labels copied",
			gpu:  &api.GPUConfig{Count: 1, NodeSelector: map[string]string{"k1": "v1", "k2": "v2"}},
			want: map[string]string{"k1": "v1", "k2": "v2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveNodeSelector(tc.gpu)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestBuildJobConfigImagePullPolicyAlways(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-123"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "http://model", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:           "adapter:dev",
					Entrypoint:      []string{"python", "main.py"},
					ImagePullPolicy: "always",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.adapterPullPolicy != corev1.PullAlways {
		t.Fatalf("adapterPullPolicy = %q, want Always", cfg.adapterPullPolicy)
	}
}

func TestResolveImagePullPolicy(t *testing.T) {
	tests := []struct {
		input string
		want  corev1.PullPolicy
	}{
		{"", corev1.PullIfNotPresent},
		{"if_not_present", corev1.PullIfNotPresent},
		{"always", corev1.PullAlways},
	}
	for _, tc := range tests {
		got := resolveImagePullPolicy(tc.input)
		if got != tc.want {
			t.Errorf("resolveImagePullPolicy(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
