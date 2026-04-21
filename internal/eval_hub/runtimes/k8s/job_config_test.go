package k8s

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.jobID != "job-123" {
		t.Fatalf("expected job id to be set")
	}
	if cfg.adapterImage != "adapter:latest" {
		t.Fatalf("expected adapter image to be set")
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
	if spec.Model.URL != "http://localhost:8080/model/v1" {
		t.Fatalf("expected job spec model url to be sidecar proxy base, got %q", spec.Model.URL)
	}
	if cfg.sidecarConfig == nil || cfg.sidecarConfig.Model == nil || cfg.sidecarConfig.Model.URL != "http://model" {
		t.Fatalf("expected sidecar_config model.url to be evaluation upstream, got %+v", cfg.sidecarConfig)
	}
	if cfg.sidecarConfig.HuggingFace == nil || cfg.sidecarConfig.HuggingFace.URL != "https://huggingface.co" {
		t.Fatalf("expected sidecar huggingface default url, got %+v", cfg.sidecarConfig.HuggingFace)
	}
	if cfg.sidecarConfig.HuggingFace.TokenPath != "" {
		t.Fatalf("expected no huggingface token_path without model auth secret, got %q", cfg.sidecarConfig.HuggingFace.TokenPath)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.modelAuthSecretRef != "my-secret" {
		t.Fatalf("expected modelAuthSecretRef %q, got %q", "my-secret", cfg.modelAuthSecretRef)
	}
	if cfg.jobSpec.Model.Auth != nil {
		t.Fatalf("expected auth omitted from job spec (handled by sidecar), got %+v", cfg.jobSpec.Model.Auth)
	}
	if cfg.sidecarConfig == nil || cfg.sidecarConfig.Model == nil || cfg.sidecarConfig.Model.AuthAPIKeyPath == "" {
		t.Fatalf("expected sidecar model auth mount path when secret_ref set, got %+v", cfg.sidecarConfig)
	}
	if cfg.sidecarConfig.HuggingFace == nil || cfg.sidecarConfig.HuggingFace.URL != "https://huggingface.co" {
		t.Fatalf("expected sidecar huggingface default url, got %+v", cfg.sidecarConfig.HuggingFace)
	}
	wantHFToken := modelAuthMountPath + "/" + modelAuthSecretHFTokenFile
	if cfg.sidecarConfig.HuggingFace.TokenPath != wantHFToken {
		t.Fatalf("expected sidecar huggingface token_path %q, got %q", wantHFToken, cfg.sidecarConfig.HuggingFace.TokenPath)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	_, err := buildJobConfig(evaluation, provider, &api.EvaluationBenchmarkConfig{}, 0, nil)
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

	_, err := buildJobConfig(evaluation, provider, nil, 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.namespace != "team-a" {
		t.Fatalf("expected namespace %q, got %q", "team-a", cfg.namespace)
	}
	if cfg.jobSpec.Tenant != "team-a" {
		t.Fatalf("expected job spec tenant %q, got %q", "team-a", cfg.jobSpec.Tenant)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil)
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
		cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], idx, nil)
		if err != nil {
			t.Fatalf("buildJobConfig(%d) error: %v", idx, err)
		}
		if cfg.benchmarkIndex != idx {
			t.Fatalf("expected benchmarkIndex %d, got %d", idx, cfg.benchmarkIndex)
		}
	}
}

func intPtr(value int) *int {
	return &value
}

func TestSidecarModelProxyURL(t *testing.T) {
	tests := []struct {
		sidecarBase string
		upstream    string
		want        string
	}{
		{"http://localhost:8080", "https://vllm.example.com/v1", "http://localhost:8080/model/v1"},
		{"http://localhost:8080", "http://model", "http://localhost:8080/model/v1"},
		{"http://127.0.0.1:9090", "https://host/path/v1", "http://127.0.0.1:9090/model/path/v1"},
	}
	for _, tt := range tests {
		got := sidecarModelProxyURL(tt.sidecarBase, tt.upstream)
		if got != tt.want {
			t.Errorf("sidecarModelProxyURL(%q, %q) = %q, want %q", tt.sidecarBase, tt.upstream, got, tt.want)
		}
	}
}
