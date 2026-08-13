package validation

import (
	"errors"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	validator "github.com/go-playground/validator/v10"
)

func newTestValidator(t *testing.T) *validator.Validate {
	t.Helper()
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewValidator(t *testing.T) {
	t.Parallel()

	validate, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() = %v, want nil error", err)
	}
	if validate == nil {
		t.Fatal("NewValidator() returned nil")
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithCollection(t *testing.T) {
	validate := newTestValidator(t)
	// When Collection is set with ID, empty Benchmarks is allowed
	cfg := api.EvaluationJobConfig{
		Name:       "test-evaluation-job",
		Model:      &api.ModelRef{URL: "http://test.com", Name: "model"},
		Collection: &api.CollectionRef{ID: "coll-1"},
		Benchmarks: []api.EvaluationBenchmarkConfig{},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Collection is set, got: %v", err)
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_EmptyBenchmarks(t *testing.T) {
	validate := newTestValidator(t)
	// When Collection is not set, Benchmarks must have at least 1 element
	cfg := api.EvaluationJobConfig{
		Name:       "test-evaluation-job",
		Model:      &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when Benchmarks is empty and Collection not set")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors with at least one error, got %T: %v", err, err)
	}
	if valErr[0].Tag() != "minimum one benchmark" || valErr[0].Param() != "1" || valErr[0].Field() != "benchmarks" {
		t.Errorf("expected first error Tag=\"minimum one benchmark\" Param=1 Field=Benchmarks, got Tag=%q Param=%q Field=%q",
			valErr[0].Tag(), valErr[0].Param(), valErr[0].Field())
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_WithBenchmark(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Benchmarks has 1+ elements, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentWithoutNameFails(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: &api.ExperimentConfig{},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when experiment is set but name is omitted")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range valErr {
		if e.Field() == "name" && e.Tag() == "notblank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notblank error on experiment name, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentNameEmptyStringFails(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: &api.ExperimentConfig{Name: ""},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when experiment name is present but empty")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range valErr {
		if e.Field() == "name" && e.Tag() == "notblank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notblank error on experiment name, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentNameWhitespaceOnlyFails(t *testing.T) {
	validate := newTestValidator(t)
	ws := " \t "
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: &api.ExperimentConfig{Name: ws},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when experiment name is only whitespace")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range valErr {
		if e.Field() == "name" && e.Tag() == "notblank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notblank error on experiment name, got: %v", err)
	}
}

func TestQueueConfig_InvalidNameRejected(t *testing.T) {
	validate := newTestValidator(t)
	invalid := []string{
		"user-queue!@#$%",
		"-starts-with-dash",
		"ends-with-dash-",
		"has spaces",
		".starts-with-dot",
		"ends-with-dot.",
		"Uppercase-Queue",
		"my_queue",
		"queue.name",
		strings.Repeat("a", 64),
	}
	for _, name := range invalid {
		t.Run("benchmark_hardware_config/"+name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{
						Ref:        api.Ref{ID: "b1"},
						ProviderID: "provider-1",
						HardwareConfig: &api.BenchmarkHardwareConfig{
							Queue: &api.QueueConfig{Kind: "kueue", Name: name},
						},
					},
				},
			}
			if err := validate.Struct(cfg); err == nil {
				t.Errorf("expected validation error for queue name %q", name)
			}
		})
		t.Run("evaluation_hardware_config/"+name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
				},
				HardwareConfig: &api.BenchmarkHardwareConfig{
					Queue: &api.QueueConfig{Kind: "kueue", Name: name},
				},
			}
			if err := validate.Struct(cfg); err == nil {
				t.Errorf("expected validation error for evaluation.hardware_config.queue name %q", name)
			}
		})
		t.Run("deprecated_evaluation_queue/"+name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
				},
				Queue: &api.QueueConfig{Kind: "kueue", Name: name},
			}
			if err := validate.Struct(cfg); err == nil {
				t.Errorf("expected validation error for evaluation.queue name %q", name)
			}
		})
	}
}

func TestQueueConfig_ValidNameAccepted(t *testing.T) {
	validate := newTestValidator(t)
	valid := []string{
		"my-queue",
		"queue1",
		"a",
		"gpu-profile-v1",
		strings.Repeat("a", 63),
	}
	for _, name := range valid {
		t.Run("benchmark_hardware_config/"+name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{
						Ref:        api.Ref{ID: "b1"},
						ProviderID: "provider-1",
						HardwareConfig: &api.BenchmarkHardwareConfig{
							Queue: &api.QueueConfig{Kind: "kueue", Name: name},
						},
					},
				},
			}
			if err := validate.Struct(cfg); err != nil {
				t.Errorf("expected no error for queue name %q, got: %v", name, err)
			}
		})
		t.Run("evaluation_hardware_config/"+name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
				},
				HardwareConfig: &api.BenchmarkHardwareConfig{
					Queue: &api.QueueConfig{Kind: "kueue", Name: name},
				},
			}
			if err := validate.Struct(cfg); err != nil {
				t.Errorf("expected no error for evaluation.hardware_config.queue name %q, got: %v", name, err)
			}
		})
		t.Run("deprecated_evaluation_queue/"+name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
				},
				Queue: &api.QueueConfig{Kind: "kueue", Name: name},
			}
			if err := validate.Struct(cfg); err != nil {
				t.Errorf("expected no error for evaluation.queue name %q, got: %v", name, err)
			}
		})
	}
}

func TestBenchmarkHardwareConfig_InvalidNameRejected(t *testing.T) {
	validate := newTestValidator(t)
	invalid := []string{
		"profile!@#$%",
		"-starts-with-dash",
		"ends-with-dash-",
		"has spaces",
		".starts-with-dot",
		"GPU-Profile",
		"cpu_profile",
		"gpu.profile.v1",
		strings.Repeat("a", 64),
	}
	for _, name := range invalid {
		cfg := api.EvaluationJobConfig{
			Name:  "test-job",
			Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "b1"},
					ProviderID: "provider-1",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						HardwareProfileName: name,
					},
				},
			},
		}
		err := validate.Struct(cfg)
		if err == nil {
			t.Errorf("expected validation error for hardware profile name %q", name)
		}
	}
}

func TestBenchmarkHardwareConfig_ValidNameAccepted(t *testing.T) {
	validate := newTestValidator(t)
	valid := []string{
		"default-profile",
		"gpu-profile-v1",
		"a",
		strings.Repeat("a", 63),
	}
	for _, name := range valid {
		cfg := api.EvaluationJobConfig{
			Name:  "test-job",
			Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "b1"},
					ProviderID: "provider-1",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						HardwareProfileName: name,
					},
				},
			},
		}
		err := validate.Struct(cfg)
		if err != nil {
			t.Errorf("expected no error for hardware profile name %q, got: %v", name, err)
		}
	}
}

func TestBenchmarkHardwareConfig_ProfileWithDirectFieldsRejected(t *testing.T) {
	validate := newTestValidator(t)
	cases := []struct {
		name string
		hw   *api.BenchmarkHardwareConfig
	}{
		{
			name: "with cpu",
			hw: &api.BenchmarkHardwareConfig{
				HardwareProfileName: "my-hw-spec",
				CPU:                 &api.HardwareResourceQuantity{Request: "1", Limit: "2"},
			},
		},
		{
			name: "with memory",
			hw: &api.BenchmarkHardwareConfig{
				HardwareProfileName: "my-hw-spec",
				Memory:              &api.HardwareResourceQuantity{Request: "1Gi"},
			},
		},
		{
			name: "with gpu",
			hw: &api.BenchmarkHardwareConfig{
				HardwareProfileName: "my-hw-spec",
				GPU:                 &api.HardwareGPUConfig{Name: "nvidia.com/gpu", Count: 1},
			},
		},
		{
			name: "with queue",
			hw: &api.BenchmarkHardwareConfig{
				HardwareProfileName: "my-hw-spec",
				Queue:               &api.QueueConfig{Kind: "kueue", Name: "my-queue"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := api.EvaluationJobConfig{
				Name:  "test-job",
				Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1", HardwareConfig: tc.hw},
				},
			}
			if err := validate.Struct(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestBenchmarkHardwareConfig_DirectFieldsAccepted(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{
				Ref:        api.Ref{ID: "b1"},
				ProviderID: "provider-1",
				HardwareConfig: &api.BenchmarkHardwareConfig{
					Queue:  &api.QueueConfig{Kind: "kueue", Name: "my-queue"},
					CPU:    &api.HardwareResourceQuantity{Request: "1", Limit: "2"},
					Memory: &api.HardwareResourceQuantity{Request: "1Gi", Limit: "2Gi"},
					GPU:    &api.HardwareGPUConfig{Name: "nvidia.com/gpu", Count: 1},
				},
			},
		},
	}
	if err := validate.Struct(cfg); err != nil {
		t.Fatalf("expected no error for direct hardware_config, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentOmittedOk(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: &api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: nil,
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when experiment is omitted, got: %v", err)
	}
}

func TestK8sRuntimeImagePullPolicy_InvalidValueRejected(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.ProviderConfig{
		Name: "test-provider",
		Runtime: &api.Runtime{
			K8s: &api.K8sRuntime{
				Image:           "quay.io/example/adapter:latest",
				Entrypoint:      []string{"/bin/true"},
				ImagePullPolicy: "random",
			},
		},
		Benchmarks: []api.BenchmarkResource{{ID: "bench-1", Name: "Bench 1"}},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for invalid image_pull_policy")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	if valErr[0].Field() != "image_pull_policy" || valErr[0].Tag() != "oneof" {
		t.Fatalf("expected oneof error on image_pull_policy, got: %v", err)
	}
}

func TestK8sRuntimeImagePullPolicy_ValidValuesAccepted(t *testing.T) {
	validate := newTestValidator(t)
	for _, policy := range []string{"", "if_not_present", "always"} {
		cfg := api.ProviderConfig{
			Name: "test-provider",
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:           "quay.io/example/adapter:latest",
					Entrypoint:      []string{"/bin/true"},
					ImagePullPolicy: policy,
				},
			},
			Benchmarks: []api.BenchmarkResource{{ID: "bench-1", Name: "Bench 1"}},
		}
		if err := validate.Struct(cfg); err != nil {
			t.Errorf("image_pull_policy %q: expected no error, got: %v", policy, err)
		}
	}
}

func TestValidateCollectionOverrides_InvalidProviderID(t *testing.T) {
	t.Parallel()
	overrides := []api.EvaluationBenchmarkConfig{
		{Ref: api.Ref{ID: "toxigen"}, ProviderID: "invalid_provider", Parameters: map[string]any{"limit": 5}},
	}
	collectionBenchmarks := []api.CollectionBenchmarkConfig{
		{Ref: api.Ref{ID: "toxigen"}, ProviderID: "lm_evaluation_harness"},
	}
	err := ValidateCollectionOverrides(overrides, collectionBenchmarks)
	var se *serviceerrors.ServiceError
	if !errors.As(err, &se) || se.MessageCode() != messages.ResourceDoesNotExist {
		t.Fatalf("err = %v, want ResourceDoesNotExist service error", err)
	}
}

func TestValidateCollectionOverrides_InvalidBenchmarkID(t *testing.T) {
	t.Parallel()
	overrides := []api.EvaluationBenchmarkConfig{
		{Ref: api.Ref{ID: "toxigen_typo"}, ProviderID: "lm_evaluation_harness", Parameters: map[string]any{"limit": 5}},
	}
	collectionBenchmarks := []api.CollectionBenchmarkConfig{
		{Ref: api.Ref{ID: "toxigen"}, ProviderID: "lm_evaluation_harness"},
	}
	err := ValidateCollectionOverrides(overrides, collectionBenchmarks)
	var se *serviceerrors.ServiceError
	if !errors.As(err, &se) || se.MessageCode() != messages.ResourceDoesNotExist {
		t.Fatalf("err = %v, want ResourceDoesNotExist service error", err)
	}
}

func TestValidateCollectionOverrides_InvalidProviderBenchmarkPair(t *testing.T) {
	t.Parallel()
	overrides := []api.EvaluationBenchmarkConfig{
		{Ref: api.Ref{ID: "b2"}, ProviderID: "p1"},
	}
	collectionBenchmarks := []api.CollectionBenchmarkConfig{
		{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
		{Ref: api.Ref{ID: "b2"}, ProviderID: "p2"},
	}
	err := ValidateCollectionOverrides(overrides, collectionBenchmarks)
	var se *serviceerrors.ServiceError
	if !errors.As(err, &se) || se.MessageCode() != messages.ResourceDoesNotExist {
		t.Fatalf("err = %v, want ResourceDoesNotExist service error", err)
	}
}

func TestValidateCollectionOverrides_EmptyOverrides(t *testing.T) {
	t.Parallel()
	collectionBenchmarks := []api.CollectionBenchmarkConfig{
		{Ref: api.Ref{ID: "toxigen"}, ProviderID: "lm_evaluation_harness"},
	}
	if err := ValidateCollectionOverrides(nil, collectionBenchmarks); err != nil {
		t.Fatalf("expected no error for empty overrides, got: %v", err)
	}
}

func TestTestDataRef_BothS3AndPVCRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		S3:  &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
		PVC: &api.PVCTestDataRef{ClaimName: "my-pvc"},
	}
	err := validate.Struct(ref)
	if err == nil {
		t.Fatal("expected validation error when both s3 and pvc are set")
	}
}

// TestRequiredWithoutAllFields exercises the required_without_all / excluded_with tags on TestDataRef.
func TestRequiredWithoutAllFields(t *testing.T) {
	validate := newTestValidator(t)
	{
		ref := api.TestDataRef{}
		err := validate.Struct(ref)
		if err == nil {
			t.Fatal("expected validation error when neither s3 nor pvc nor git is set")
		}
	}
	{
		ref := api.TestDataRef{
			S3: &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
		}
		err := validate.Struct(ref)
		if err != nil {
			t.Fatalf("expected no error when s3 is set, got: %v", err)
		}
	}
	{
		ref := api.TestDataRef{
			PVC: &api.PVCTestDataRef{ClaimName: "my-pvc"},
		}
		err := validate.Struct(ref)
		if err != nil {
			t.Fatalf("expected no error when pvc is set, got: %v", err)
		}
	}
	{
		ref := api.TestDataRef{
			Git: &api.GitTestDataRef{URL: "https://github.com/my-org/my-repo.git", Ref: "main"},
		}
		err := validate.Struct(ref)
		if err != nil {
			t.Fatalf("expected no error when git is set, got: %v", err)
		}
	}
	{
		ref := api.TestDataRef{
			S3:  &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
			Git: &api.GitTestDataRef{URL: "https://github.com/my-org/my-repo.git", Ref: "main"},
		}
		err := validate.Struct(ref)
		if err == nil {
			t.Fatal("expected validation error when both s3 and git are set")
		}
	}
}

func TestTestDataRef_NeitherS3NorPVCRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{}
	err := validate.Struct(ref)
	if err == nil {
		t.Fatal("expected validation error when neither s3 nor pvc is set")
	}
}

func TestTestDataRef_PVCOnlyAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		PVC: &api.PVCTestDataRef{ClaimName: "my-pvc"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for valid pvc-only TestDataRef, got: %v", err)
	}
}

func TestTestDataRef_S3OnlyAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		S3: &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for valid s3-only TestDataRef, got: %v", err)
	}
}

func TestPVCTestDataRef_InvalidClaimNameRejected(t *testing.T) {
	validate := newTestValidator(t)
	cases := []string{"", "My_PVC", "my pvc", "-leading-hyphen", "trailing-hyphen-"}
	for _, name := range cases {
		ref := api.PVCTestDataRef{ClaimName: name}
		if err := validate.Struct(ref); err == nil {
			t.Errorf("expected validation error for claim %q", name)
		}
	}
}

func TestPVCTestDataRef_ValidClaimNameAccepted(t *testing.T) {
	validate := newTestValidator(t)
	cases := []string{"my-pvc", "eval-datasets-pvc", "pvc123", "a"}
	for _, name := range cases {
		ref := api.PVCTestDataRef{ClaimName: name}
		if err := validate.Struct(ref); err != nil {
			t.Errorf("expected no error for claim %q, got: %v", name, err)
		}
	}
}

func TestTestDataRef_GitOnlyAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Git: &api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for valid git-only TestDataRef, got: %v", err)
	}
}

func TestTestDataRef_GitWithSecretRefAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Git: &api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main", SecretRef: "my-secret"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for git TestDataRef with secret_ref, got: %v", err)
	}
}

func TestTestDataRef_GitAndS3Rejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Git: &api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main"},
		S3:  &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
	}
	if err := validate.Struct(ref); err == nil {
		t.Fatal("expected validation error when both git and s3 are set")
	}
}

func TestTestDataRef_GitAndPVCRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Git: &api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main"},
		PVC: &api.PVCTestDataRef{ClaimName: "my-pvc"},
	}
	if err := validate.Struct(ref); err == nil {
		t.Fatal("expected validation error when both git and pvc are set")
	}
}

func TestGitTestDataRef_MissingURLRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{Ref: "main"}
	if err := validate.Struct(ref); err == nil {
		t.Fatal("expected validation error for missing git URL")
	}
}

func TestGitTestDataRef_MissingRefRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{URL: "https://github.com/org/repo.git"}
	if err := validate.Struct(ref); err == nil {
		t.Fatal("expected validation error for missing git ref")
	}
}

func TestGitTestDataRef_InvalidSecretRefRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main", SecretRef: "Invalid_Name"}
	if err := validate.Struct(ref); err == nil {
		t.Fatal("expected validation error for invalid secret_ref DNS label")
	}
}

func TestGitTestDataRef_BlockedHostsRejected(t *testing.T) {
	validate := newTestValidator(t)
	cases := []string{
		"https://127.0.0.1/repo.git",
		"https://10.0.0.1/repo.git",
		"https://localhost/repo.git",
		"https://git.default.svc.cluster.local/repo.git",
		"https://169.254.169.254/repo.git",
	}
	for _, u := range cases {
		ref := api.GitTestDataRef{URL: u, Ref: "main"}
		if err := validate.Struct(ref); err == nil {
			t.Errorf("expected validation error for git url %q", u)
		}
	}
}

func TestGitTestDataRef_PublicHostAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main"}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for public git host, got: %v", err)
	}
}

func TestGitTestDataRef_HTTPWithoutSecretAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{URL: "http://git.example.com/repo.git", Ref: "main"}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for http without secret_ref, got: %v", err)
	}
}

func TestGitTestDataRef_HTTPWithSecretRejected(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{
		URL:       "http://git.example.com/repo.git",
		Ref:       "main",
		SecretRef: "git-creds",
	}
	if err := validate.Struct(ref); err == nil {
		t.Fatal("expected validation error for http url with secret_ref")
	}
}

func TestGitTestDataRef_HTTPSWithSecretAccepted(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.GitTestDataRef{
		URL:       "https://github.com/org/repo.git",
		Ref:       "main",
		SecretRef: "git-creds",
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for https with secret_ref, got: %v", err)
	}
}

func TestStatusEvent_BenchmarkStatusEventRequired(t *testing.T) {
	validate := newTestValidator(t)
	ev := api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: "p1",
		ID:         "b1",
		Status:     "running",
	}}
	if err := validate.Struct(ev); err != nil {
		t.Fatalf("expected no error for valid StatusEvent, got: %v", err)
	}
}

func TestStatusEvent_MissingBenchmarkStatusEventRejected(t *testing.T) {
	validate := newTestValidator(t)
	ev := api.StatusEvent{}
	if err := validate.Struct(ev); err == nil {
		t.Fatal("expected validation error when BenchmarkStatusEvent is nil")
	}
}

// --- Tests for model struct tag validation ---

func TestEvaluationJobConfig_ModelNilRejected(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: nil,
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when Model is nil")
	}
}

func TestEvaluationJobConfig_ModelURLEmpty_AcceptedByValidator(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: &api.ModelRef{URL: "", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Fatalf("expected no validation error for empty model URL (handler enforces this), got: %v", err)
	}
}

func TestEvaluationJobConfig_ModelURLNotRequired_AllBenchmarksPreRecorded(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: &api.ModelRef{URL: "", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{
				Ref:        api.Ref{ID: "b1"},
				ProviderID: "provider-1",
				TestDataRef: &api.TestDataRef{
					Type: "pre_recorded_data",
					S3:   &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
				},
			},
			{
				Ref:        api.Ref{ID: "b2"},
				ProviderID: "provider-1",
				TestDataRef: &api.TestDataRef{
					Type: "pre_recorded_data",
					PVC:  &api.PVCTestDataRef{ClaimName: "my-pvc"},
				},
			},
		},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Fatalf("expected no error when all benchmarks have pre_recorded_data, got: %v", err)
	}
}

func TestEvaluationJobConfig_ModelURLNotRequired_Collection(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:       "test-job",
		Model:      &api.ModelRef{URL: "", Name: "model"},
		Collection: &api.CollectionRef{ID: "coll-1"},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Fatalf("expected no error when using a collection (URL check is in handler), got: %v", err)
	}
}

func TestEvaluationJobConfig_ModelURLValid_NonPreRecordedBenchmarks(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: &api.ModelRef{URL: "http://model.example.com/v1", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Fatalf("expected no error when model URL is valid, got: %v", err)
	}
}

func TestEvaluationJobConfig_ModelURLInvalid_Rejected(t *testing.T) {
	validate := newTestValidator(t)
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: &api.ModelRef{URL: "not-a-url", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for invalid model URL format")
	}
}

// --- Tests for TestDataRef.Type field validation ---

func TestTestDataRef_TypeValidValues(t *testing.T) {
	validate := newTestValidator(t)
	validTypes := []string{"", "data_set", "pre_recorded_data"}
	for _, typ := range validTypes {
		t.Run("type="+typ, func(t *testing.T) {
			ref := api.TestDataRef{
				Type: typ,
				S3:   &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
			}
			if err := validate.Struct(ref); err != nil {
				t.Fatalf("expected no error for type %q, got: %v", typ, err)
			}
		})
	}
}

func TestTestDataRef_TypeInvalidValues(t *testing.T) {
	validate := newTestValidator(t)
	invalidTypes := []string{"unknown", "recorded", "dataset", "PRE_RECORDED_DATA"}
	for _, typ := range invalidTypes {
		t.Run("type="+typ, func(t *testing.T) {
			ref := api.TestDataRef{
				Type: typ,
				S3:   &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
			}
			if err := validate.Struct(ref); err == nil {
				t.Fatalf("expected validation error for type %q", typ)
			}
		})
	}
}

func TestTestDataRef_PreRecordedDataWithS3(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Type: "pre_recorded_data",
		S3:   &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for pre_recorded_data with S3, got: %v", err)
	}
}

func TestTestDataRef_PreRecordedDataWithPVC(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Type: "pre_recorded_data",
		PVC:  &api.PVCTestDataRef{ClaimName: "my-pvc"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for pre_recorded_data with PVC, got: %v", err)
	}
}

func TestTestDataRef_PreRecordedDataWithGit(t *testing.T) {
	validate := newTestValidator(t)
	ref := api.TestDataRef{
		Type: "pre_recorded_data",
		Git:  &api.GitTestDataRef{URL: "https://github.com/org/repo.git", Ref: "main"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for pre_recorded_data with Git, got: %v", err)
	}
}
