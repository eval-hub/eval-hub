package k8s

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestValidateHardwareProfiles(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "opendatahub")

	benchmarksWithProfile := func(name string) []api.EvaluationBenchmarkConfig {
		return []api.EvaluationBenchmarkConfig{{
			Ref:        api.Ref{ID: "bench-1"},
			ProviderID: "provider-1",
			HardwareConfig: &api.BenchmarkHardwareConfig{
				HardwareProfileName: name,
			},
		}}
	}

	t.Run("skips when no hardware config", func(t *testing.T) {
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
			},
			ctx: context.Background(),
		}
		if err := runtime.ValidateHardwareProfiles([]api.EvaluationBenchmarkConfig{{
			Ref:        api.Ref{ID: "bench-1"},
			ProviderID: "provider-1",
		}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("skips empty profile name", func(t *testing.T) {
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
			},
			ctx: context.Background(),
		}
		if err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("   ")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		profile := testHardwareProfileUnstructured("opendatahub", "cpu-profile")
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
			},
			ctx: context.Background(),
		}
		if err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("cpu-profile")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("uses background context when runtime ctx nil", func(t *testing.T) {
		profile := testHardwareProfileUnstructured("opendatahub", "cpu-profile")
		runtime := &K8sRuntime{
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
			},
		}
		if err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("cpu-profile")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("namespace env unset", func(t *testing.T) {
		t.Setenv(hardwareProfilesNamespaceEnv, "")
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
			},
			ctx: context.Background(),
		}
		err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("cpu-profile"))
		assertServiceErrorCode(t, err, messages.HardwareProfileFetchFailed)
	})

	t.Run("not found", func(t *testing.T) {
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
			},
			ctx: context.Background(),
		}
		err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("missing-profile"))
		assertServiceErrorCode(t, err, messages.HardwareProfileNotFound)
	})

	t.Run("fetch failed", func(t *testing.T) {
		dynamicClient := dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme())
		dynamicClient.PrependReactor("get", "hardwareprofiles", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("apiserver unavailable")
		})
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{dynamicClient: dynamicClient},
			ctx:    context.Background(),
		}
		err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("cpu-profile"))
		assertServiceErrorCode(t, err, messages.HardwareProfileFetchFailed)
		if apierrors.IsNotFound(err) {
			t.Fatal("expected non-not-found fetch failure")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		profile := testHardwareProfileUnstructured("opendatahub", "disabled-profile")
		profile.SetAnnotations(map[string]string{hardwareProfileDisabledAnnotation: "true"})
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
			},
			ctx: context.Background(),
		}
		err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("disabled-profile"))
		assertServiceErrorCode(t, err, messages.HardwareProfileDisabled)
	})

	t.Run("invalid profile", func(t *testing.T) {
		profile := testHardwareProfileUnstructured("opendatahub", "bad-profile")
		profile.Object["spec"] = map[string]any{
			"identifiers": []any{
				map[string]any{
					"identifier":   "nvidia.com/gpu",
					"resourceType": "Accelerator",
					"defaultCount": "not-a-number",
				},
			},
		}
		runtime := &K8sRuntime{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			helper: &KubernetesHelper{
				dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
			},
			ctx: context.Background(),
		}
		err := runtime.ValidateHardwareProfiles(benchmarksWithProfile("bad-profile"))
		assertServiceErrorCode(t, err, messages.HardwareProfileInvalid)
	})
}

func assertServiceErrorCode(t *testing.T, err error, want *messages.MessageCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil", want.GetCode())
	}
	var se *serviceerrors.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if se.MessageCode() != want {
		t.Fatalf("message code = %s, want %s", se.MessageCode().GetCode(), want.GetCode())
	}
}
