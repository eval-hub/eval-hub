package k8s

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func sourceTestJob(result *api.PostProcessingResultsDataRef, calibration []api.CalibrationDataRef) *api.EvaluationJobResource {
	request := &api.StandalonePostProcessingRequest{Operations: api.StandalonePostProcessingOperations{
		ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
			ResultsDataRef:                 result,
			ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{CalibrationDataRef: calibration, SignificanceLevel: 0.05},
		},
	}}
	if result != nil && result.EvalJob == nil {
		request.Operations.ConfidenceInterval.PrimaryScore = &api.PrimaryScore{Metric: "accuracy"}
	}
	job := &api.EvaluationJobResource{EvaluationJobConfig: *postprocessing.ToEvaluationJob(request)}
	job.Resource.ID = "source-mount-regression"
	return job
}

// Reproduce the complete 7 x 4 source matrix at the API-to-pod boundary.
// These cases include all 15 previously failing source combinations.
func TestPostProcessorSourceCombinations(t *testing.T) {
	results := map[string]*api.PostProcessingResultsDataRef{
		"eval_job": {EvalJob: &api.EvaluationJobDataRef{ID: "source"}},
		"s3":       {S3: &api.S3TestDataRef{Bucket: "data", Key: "results", SecretRef: "results-s3"}},
		"pvc":      {PVC: &api.PVCTestDataRef{ClaimName: "results-pvc", SubPath: "predictions"}},
		"git":      {Git: &api.GitTestDataRef{URL: "https://example.org/data.git", Ref: "main"}},
		"hf":       {HF: &api.HFTestDataRef{RepoID: "org/results"}},
		"mlflow":   {MLFlow: &api.MLflowDataRef{RunID: "run", ArtifactPath: "results"}},
		"oci":      {OCI: &api.OCIDataRef{Coordinates: api.OCICoordinates{OCIHost: "registry.example", OCIRepository: "org/results", OCITag: "samples"}, ArtifactPath: "results", K8s: &api.OCIConnectionConfig{Connection: "registry-auth"}}},
	}
	calibrations := map[string]api.CalibrationDataRef{
		"s3":  {S3: &api.S3TestDataRef{Bucket: "data", Key: "labels", SecretRef: "calibration-s3"}},
		"pvc": {PVC: &api.PVCTestDataRef{ClaimName: "calibration-pvc", SubPath: "labels"}},
		"git": {Git: &api.GitTestDataRef{URL: "https://example.org/data.git", Ref: "main"}},
		"hf":  {HF: &api.HFTestDataRef{RepoID: "org/calibration"}},
	}
	for resultName, result := range results {
		for calibrationName, calibration := range calibrations {
			t.Run(resultName+"/"+calibrationName, func(t *testing.T) {
				evaluation := sourceTestJob(result, []api.CalibrationDataRef{calibration})
				cfg, err := buildJobConfig(evaluation, postprocessing.RuntimeProvider(), &evaluation.Benchmarks[0], 0, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				job, err := buildJob(cfg, nil)
				if err != nil {
					t.Fatal(err)
				}
				adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
				for _, requirement := range []struct {
					needed bool
					secret string
				}{{resultName == "s3", "results-s3"}, {calibrationName == "s3", "calibration-s3"}} {
					if !requirement.needed {
						continue
					}
					found := false
					for _, volume := range job.Spec.Template.Spec.Volumes {
						if volume.Secret != nil && volume.Secret.SecretName == requirement.secret {
							mount := findVolumeMount(adapter.VolumeMounts, volume.Name)
							found = mount != nil && mount.ReadOnly && mount.MountPath == postProcessorSecretMountPath+"/"+requirement.secret
						}
					}
					if !found {
						t.Fatalf("referenced S3 secret %q is not accessible to the post-processor", requirement.secret)
					}
				}
				var mounts map[string]string
				for _, env := range adapter.Env {
					if env.Name == envPostProcessorPVCMountsName {
						if err := json.Unmarshal([]byte(env.Value), &mounts); err != nil {
							t.Fatal(err)
						}
					}
				}
				if resultName == "pvc" && mounts["results-pvc"] == "" {
					t.Fatal("missing results PVC mapping")
				}
				if calibrationName == "pvc" && mounts["calibration-pvc"] == "" {
					t.Fatal("missing calibration PVC mapping")
				}
				if resultName == "oci" && (cfg.jobSpec.Exports == nil || cfg.jobSpec.Exports.OCI == nil || cfg.jobSpec.Exports.OCI.Coordinates.OCIRepository != "org/results" || cfg.ociCredentialsSecret != "registry-auth") {
					t.Fatal("missing OCI sidecar configuration")
				}
			})
		}
	}
}

func TestPostProcessorSecretProjection(t *testing.T) {
	tests := []struct {
		name        string
		result      *api.PostProcessingResultsDataRef
		calibration []api.CalibrationDataRef
		want        []string
	}{
		{name: "results S3 only", result: &api.PostProcessingResultsDataRef{S3: &api.S3TestDataRef{SecretRef: "results-s3"}}, want: []string{"results-s3"}},
		{name: "calibration S3 only", calibration: []api.CalibrationDataRef{{S3: &api.S3TestDataRef{SecretRef: "calibration-s3"}}}, want: []string{"calibration-s3"}},
		{name: "shared S3 secret", result: &api.PostProcessingResultsDataRef{S3: &api.S3TestDataRef{SecretRef: "shared"}}, calibration: []api.CalibrationDataRef{{S3: &api.S3TestDataRef{SecretRef: "shared"}}, {S3: &api.S3TestDataRef{SecretRef: "other"}}}, want: []string{"shared", "other"}},
		{name: "authenticated Git and HF", result: &api.PostProcessingResultsDataRef{Git: &api.GitTestDataRef{SecretRef: "git-auth"}}, calibration: []api.CalibrationDataRef{{HF: &api.HFTestDataRef{SecretRef: "hf-auth"}}}, want: []string{"git-auth", "hf-auth"}},
		{name: "public Git and HF", result: &api.PostProcessingResultsDataRef{HF: &api.HFTestDataRef{RepoID: "org/data"}}, calibration: []api.CalibrationDataRef{{Git: &api.GitTestDataRef{URL: "https://example.org/repo"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluation := sourceTestJob(tt.result, tt.calibration)
			cfg, err := buildJobConfig(evaluation, postprocessing.RuntimeProvider(), &evaluation.Benchmarks[0], 0, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.postProcessorSecrets, tt.want) {
				t.Fatalf("secrets = %v, want %v", cfg.postProcessorSecrets, tt.want)
			}
			cfg.defaultEnv = []api.EnvVar{{Name: envPostProcessorSecretRoot, Value: "must-not-override"}}
			job, err := buildJob(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
			sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
			for _, name := range tt.want {
				count := 0
				for _, volume := range job.Spec.Template.Spec.Volumes {
					if volume.Secret == nil || volume.Secret.SecretName != name {
						continue
					}
					count++
					mount := findVolumeMount(adapter.VolumeMounts, volume.Name)
					if mount == nil || !mount.ReadOnly || mount.MountPath != postProcessorSecretMountPath+"/"+name || mount.SubPath != "" {
						t.Fatalf("incorrect secret mount: %#v", mount)
					}
					if findVolumeMount(sidecar.VolumeMounts, volume.Name) != nil {
						t.Fatal("source secret must not be mounted in the sidecar")
					}
				}
				if count != 1 {
					t.Fatalf("secret %q has %d volumes, want 1", name, count)
				}
			}
			if len(tt.want) > 0 {
				count := 0
				for _, env := range adapter.Env {
					if env.Name == envPostProcessorSecretRoot {
						count++
						if env.Value != postProcessorSecretMountPath {
							t.Fatal("provider overrides trusted secret root")
						}
					}
				}
				if count != 1 {
					t.Fatalf("secret root env count = %d", count)
				}
			}
		})
	}
}

func TestPostProcessorSecretNamesRejectUnsafeNames(t *testing.T) {
	for _, name := range []string{"../secret", "/secret", ".", "..", "bad/name"} {
		t.Run(name, func(t *testing.T) {
			job := sourceTestJob(&api.PostProcessingResultsDataRef{S3: &api.S3TestDataRef{SecretRef: name}}, nil)
			if _, err := postProcessorSecretNames(job); err == nil {
				t.Fatal("unsafe secret name accepted")
			}
		})
	}
	for _, job := range []*api.EvaluationJobResource{nil, {}} {
		refs, err := postProcessorSecretNames(job)
		if err != nil || len(refs) != 0 {
			t.Fatalf("ordinary job secrets = %v, error = %v", refs, err)
		}
	}
}

func TestPostProcessorResultsPVCMounts(t *testing.T) {
	for _, calibrationClaim := range []string{"", "results", "other"} {
		t.Run("calibration-"+calibrationClaim, func(t *testing.T) {
			var calibration []api.CalibrationDataRef
			if calibrationClaim != "" {
				calibration = []api.CalibrationDataRef{{PVC: &api.PVCTestDataRef{ClaimName: calibrationClaim, SubPath: "labels"}}}
			}
			evaluation := sourceTestJob(&api.PostProcessingResultsDataRef{PVC: &api.PVCTestDataRef{ClaimName: "results", SubPath: "predictions"}}, calibration)
			cfg, err := buildJobConfig(evaluation, postprocessing.RuntimeProvider(), &evaluation.Benchmarks[0], 0, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if calibrationClaim == "other" {
				want = 2
			}
			if len(cfg.postProcessorPVCs) != want {
				t.Fatalf("PVC count = %d, want %d", len(cfg.postProcessorPVCs), want)
			}
			job, err := buildJob(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
			var mapping map[string]string
			for _, env := range adapter.Env {
				if env.Name == envPostProcessorPVCMountsName {
					if err := json.Unmarshal([]byte(env.Value), &mapping); err != nil {
						t.Fatal(err)
					}
				}
			}
			if len(mapping) != want || mapping["results"] == "" {
				t.Fatalf("PVC mapping = %v", mapping)
			}
			for _, pvc := range cfg.postProcessorPVCs {
				volume := findVolume(job.Spec.Template.Spec.Volumes, pvc.volumeName)
				mount := findVolumeMount(adapter.VolumeMounts, pvc.volumeName)
				if volume == nil || volume.PersistentVolumeClaim == nil || !volume.PersistentVolumeClaim.ReadOnly {
					t.Fatal("PVC must be mounted read-only")
				}
				if mount == nil || !mount.ReadOnly || mount.SubPath != "" {
					t.Fatal("PVC must expose its read-only root; subpaths are applied by the adapter")
				}
			}
		})
	}
}

func TestPostProcessorExternalOCIConfiguresSidecar(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(map[bool]string{false: "public", true: "authenticated"}[authenticated], func(t *testing.T) {
			source := &api.OCIDataRef{Coordinates: api.OCICoordinates{OCIHost: "registry.example", OCIRepository: "org/results", OCITag: "source-tag"}, Digest: "sha256:" + strings.Repeat("a", 64), ArtifactPath: "samples"}
			if authenticated {
				source.K8s = &api.OCIConnectionConfig{Connection: "registry-auth"}
			}
			evaluation := sourceTestJob(&api.PostProcessingResultsDataRef{OCI: source}, nil)
			cfg, err := buildJobConfig(evaluation, postprocessing.RuntimeProvider(), &evaluation.Benchmarks[0], 0, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			cm, err := buildConfigMap(cfg)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "job.json")
			if err := os.WriteFile(path, []byte(cm.Data[jobSpecFileName]), 0o600); err != nil {
				t.Fatal(err)
			}
			host, repo, err := proxy.GetOCICoordinatesFromJobSpec(path)
			if err != nil || host != "https://registry.example" || repo != "org/results" {
				t.Fatalf("sidecar registry target = %q/%q, error = %v", host, repo, err)
			}
			if authenticated {
				if cfg.ociCredentialsSecret != "registry-auth" {
					t.Fatalf("missing registry credentials: %q", cfg.ociCredentialsSecret)
				}
				job, err := buildJob(cfg, nil)
				if err != nil {
					t.Fatal(err)
				}
				sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
				volume := findVolume(job.Spec.Template.Spec.Volumes, ociCredentialsVolumeName)
				if volume == nil || volume.Secret == nil || volume.Secret.SecretName != "registry-auth" || findVolumeMount(sidecar.VolumeMounts, ociCredentialsVolumeName) == nil {
					t.Fatal("registry credentials missing from sidecar")
				}
			}
		})
	}
}
