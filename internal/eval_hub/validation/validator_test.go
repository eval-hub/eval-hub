package validation

import (
	"errors"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

func TestNewValidator(t *testing.T) {
	validate := NewValidator()
	if validate == nil {
		t.Fatal("NewValidator() returned nil validator")
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithCollection(t *testing.T) {
	validate := NewValidator()
	// When Collection is set with ID, empty Benchmarks is allowed
	cfg := api.EvaluationJobConfig{
		Name:       "test-evaluation-job",
		Model:      api.ModelRef{URL: "http://test.com", Name: "model"},
		Collection: &api.CollectionRef{ID: "coll-1"},
		Benchmarks: []api.EvaluationBenchmarkConfig{},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Collection is set, got: %v", err)
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_EmptyBenchmarks(t *testing.T) {
	validate := NewValidator()
	// When Collection is not set, Benchmarks must have at least 1 element
	cfg := api.EvaluationJobConfig{
		Name:       "test-evaluation-job",
		Model:      api.ModelRef{URL: "http://test.com", Name: "model"},
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
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
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
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
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
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
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
	validate := NewValidator()
	ws := " \t "
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
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
	validate := NewValidator()
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
	}
	for _, name := range invalid {
		cfg := api.EvaluationJobConfig{
			Name:  "test-job",
			Model: api.ModelRef{URL: "http://test.com", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
			},
			Queue: &api.QueueConfig{Kind: "kueue", Name: name},
		}
		err := validate.Struct(cfg)
		if err == nil {
			t.Errorf("expected validation error for queue name %q", name)
		}
	}
}

func TestQueueConfig_ValidNameAccepted(t *testing.T) {
	validate := NewValidator()
	valid := []string{
		"my-queue",
		"queue1",
		"a",
		"gpu-profile-v1",
	}
	for _, name := range valid {
		cfg := api.EvaluationJobConfig{
			Name:  "test-job",
			Model: api.ModelRef{URL: "http://test.com", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
			},
			Queue: &api.QueueConfig{Kind: "kueue", Name: name},
		}
		err := validate.Struct(cfg)
		if err != nil {
			t.Errorf("expected no error for queue name %q, got: %v", name, err)
		}
	}
}

func TestBenchmarkHardwareConfig_InvalidNameRejected(t *testing.T) {
	validate := NewValidator()
	invalid := []string{
		"profile!@#$%",
		"-starts-with-dash",
		"ends-with-dash-",
		"has spaces",
		".starts-with-dot",
		"GPU-Profile",
		"cpu_profile",
		"gpu.profile.v1",
	}
	for _, name := range invalid {
		cfg := api.EvaluationJobConfig{
			Name:  "test-job",
			Model: api.ModelRef{URL: "http://test.com", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "b1"},
					ProviderID: "provider-1",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						HardwareProfileRef: api.HardwareProfileRef{Name: name},
					},
				},
			},
		}
		err := validate.Struct(cfg)
		if err == nil {
			t.Errorf("expected validation error for hardware profile ref %q", name)
		}
	}
}

func TestBenchmarkHardwareConfig_ValidNameAccepted(t *testing.T) {
	validate := NewValidator()
	valid := []struct {
		name      string
		namespace string
	}{
		{name: "default-profile"},
		{name: "gpu-profile-v1", namespace: "opendatahub"},
		{name: "a"},
	}
	for _, tc := range valid {
		cfg := api.EvaluationJobConfig{
			Name:  "test-job",
			Model: api.ModelRef{URL: "http://test.com", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "b1"},
					ProviderID: "provider-1",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						HardwareProfileRef: api.HardwareProfileRef{
							Name:      tc.name,
							Namespace: tc.namespace,
						},
					},
				},
			},
		}
		err := validate.Struct(cfg)
		if err != nil {
			t.Errorf("expected no error for hardware profile ref %#v, got: %v", tc, err)
		}
	}
}

func TestBenchmarkHardwareConfig_InvalidNamespaceRejected(t *testing.T) {
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{
				Ref:        api.Ref{ID: "b1"},
				ProviderID: "provider-1",
				HardwareConfig: &api.BenchmarkHardwareConfig{
					HardwareProfileRef: api.HardwareProfileRef{
						Name:      "valid-profile",
						Namespace: "invalid namespace",
					},
				},
			},
		},
	}
	if err := validate.Struct(cfg); err == nil {
		t.Fatal("expected validation error for invalid hardware profile namespace")
	}
}

func TestEvaluationJobConfig_ExperimentOmittedOk(t *testing.T) {
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
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
	validate := NewValidator()
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
	validate := NewValidator()
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
	validate := NewValidator()
	ref := api.TestDataRef{
		S3:  &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
		PVC: &api.PVCTestDataRef{ClaimName: "my-pvc"},
	}
	err := validate.Struct(ref)
	if err == nil {
		t.Fatal("expected validation error when both s3 and pvc are set")
	}
}

func TestTestDataRef_NeitherS3NorPVCRejected(t *testing.T) {
	validate := NewValidator()
	ref := api.TestDataRef{}
	err := validate.Struct(ref)
	if err == nil {
		t.Fatal("expected validation error when neither s3 nor pvc is set")
	}
}

func TestTestDataRef_PVCOnlyAccepted(t *testing.T) {
	validate := NewValidator()
	ref := api.TestDataRef{
		PVC: &api.PVCTestDataRef{ClaimName: "my-pvc"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for valid pvc-only TestDataRef, got: %v", err)
	}
}

func TestTestDataRef_S3OnlyAccepted(t *testing.T) {
	validate := NewValidator()
	ref := api.TestDataRef{
		S3: &api.S3TestDataRef{Bucket: "b", Key: "k", SecretRef: "s"},
	}
	if err := validate.Struct(ref); err != nil {
		t.Fatalf("expected no error for valid s3-only TestDataRef, got: %v", err)
	}
}

func TestPVCTestDataRef_InvalidClaimNameRejected(t *testing.T) {
	validate := NewValidator()
	cases := []string{"", "My_PVC", "my pvc", "-leading-hyphen", "trailing-hyphen-"}
	for _, name := range cases {
		ref := api.PVCTestDataRef{ClaimName: name}
		if err := validate.Struct(ref); err == nil {
			t.Errorf("expected validation error for claim %q", name)
		}
	}
}

func TestPVCTestDataRef_ValidClaimNameAccepted(t *testing.T) {
	validate := NewValidator()
	cases := []string{"my-pvc", "eval-datasets-pvc", "pvc123", "a"}
	for _, name := range cases {
		ref := api.PVCTestDataRef{ClaimName: name}
		if err := validate.Struct(ref); err != nil {
			t.Errorf("expected no error for claim %q, got: %v", name, err)
		}
	}
}
