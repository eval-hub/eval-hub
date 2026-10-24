package k8s

import (
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
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
				cfg.testDataInitImage = "evalhub:test"
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
							init := findContainer(job.Spec.Template.Spec.InitContainers, initContainerName)
							mount := findVolumeMount(init.VolumeMounts, volume.Name)
							if findVolumeMount(adapter.VolumeMounts, volume.Name) != nil {
								t.Fatal("source credentials exposed to adapter")
							}
							found = mount != nil && mount.ReadOnly && mount.MountPath == postProcessorSecretMountPath+"/"+requirement.secret
						}
					}
					if !found {
						t.Fatalf("referenced S3 secret %q is not accessible to the post-processor", requirement.secret)
					}
				}
				var mounts map[string]string
				for _, env := range adapter.Env {
					if env.Name == shared.PostProcessorDataPathsEnv {
						if err := json.Unmarshal([]byte(env.Value), &mounts); err != nil {
							t.Fatal(err)
						}
					}
				}
				if resultName == "pvc" && mounts["results"] == "" {
					t.Fatal("missing results PVC mapping")
				}
				if calibrationName == "pvc" && mounts["calibration-0"] == "" {
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
	evaluation := sourceTestJob(&api.PostProcessingResultsDataRef{Git: &api.GitTestDataRef{URL: "https://example.org/data", Ref: "main", SecretRef: "shared"}}, []api.CalibrationDataRef{{S3: &api.S3TestDataRef{Bucket: "data", Key: "labels", SecretRef: "shared"}}, {HF: &api.HFTestDataRef{RepoID: "org/data", SecretRef: "hf-auth"}}})
	cfg, err := buildJobConfig(evaluation, postprocessing.RuntimeProvider(), &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.testDataInitImage = "evalhub:test"
	cfg.defaultEnv = []api.EnvVar{{Name: shared.PostProcessorDataPathsEnv, Value: "untrusted-provider-value"}}
	job, err := buildJob(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	init := findContainer(job.Spec.Template.Spec.InitContainers, initContainerName)
	adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
	pathsCount := 0
	for _, env := range adapter.Env {
		if env.Name == shared.PostProcessorDataPathsEnv {
			pathsCount++
			var paths map[string]string
			if err := json.Unmarshal([]byte(env.Value), &paths); err != nil {
				t.Fatal(err)
			}
			if len(paths) != 3 || paths["results"] != shared.PostProcessorStagingRoot+"/results" {
				t.Fatalf("incorrect staged-path contract: %v", paths)
			}
		}
	}
	if pathsCount != 1 {
		t.Fatalf("staged mapping count = %d", pathsCount)
	}
	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if init == nil || init.Image != "evalhub:test" || !reflect.DeepEqual(init.Command, []string{defaultTestDataInitCmd}) {
		t.Fatal("must reuse eval-runtime-init")
	}
	if job.Spec.Template.Spec.InitContainers[0].Name != initContainerName {
		t.Fatal("staging must complete before the adapter starts")
	}
	for _, name := range []string{"shared", "hf-auth"} {
		count := 0
		for _, volume := range job.Spec.Template.Spec.Volumes {
			if volume.Secret != nil && volume.Secret.SecretName == name {
				count++
				mount := findVolumeMount(init.VolumeMounts, volume.Name)
				if mount == nil || !mount.ReadOnly || mount.MountPath != postProcessorSecretMountPath+"/"+name {
					t.Fatal("missing init credential mount")
				}
				if findVolumeMount(adapter.VolumeMounts, volume.Name) != nil || findVolumeMount(sidecar.VolumeMounts, volume.Name) != nil {
					t.Fatal("source credentials must be confined to init")
				}
			}
		}
		if count != 1 {
			t.Fatalf("secret %s mounted %d times", name, count)
		}
	}
	cm, err := buildConfigMap(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var plan shared.DataStagingPlan
	if err := json.Unmarshal([]byte(cm.Data[shared.DataStagingPlanFile]), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Sources) != 3 {
		t.Fatalf("staging plan: %+v", plan)
	}
	staged := findVolumeMount(adapter.VolumeMounts, postProcessorDataVolumeName)
	if staged == nil || !staged.ReadOnly {
		t.Fatal("adapter staging mount must be read-only")
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

func TestPostProcessorPVCFileSubpath(t *testing.T) {
	for _, subpath := range []string{"nested/results.json", "../escape", "/absolute"} {
		t.Run(subpath, func(t *testing.T) {
			job := sourceTestJob(&api.PostProcessingResultsDataRef{PVC: &api.PVCTestDataRef{ClaimName: "data", SubPath: subpath}}, nil)
			pvcs, err := postProcessorPVCConfigs(job)
			if subpath != "nested/results.json" {
				if err == nil {
					t.Fatal("unsafe subpath accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(pvcs) != 1 || pvcs[0].mountPath != shared.PostProcessorPVCRoot+"/results/results.json" || pvcs[0].subPath != subpath {
				t.Fatalf("file subpath must preserve its basename: %+v", pvcs)
			}
		})
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
			if calibrationClaim != "" {
				want = 2
			}
			if len(cfg.postProcessorPVCs) != want {
				t.Fatalf("PVC count = %d, want %d", len(cfg.postProcessorPVCs), want)
			}
			cfg.testDataInitImage = "evalhub:test"
			job, err := buildJob(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
			var mapping map[string]string
			for _, env := range adapter.Env {
				if env.Name == shared.PostProcessorDataPathsEnv {
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
				var mount *corev1.VolumeMount
				for i := range adapter.VolumeMounts {
					if adapter.VolumeMounts[i].MountPath == pvc.mountPath {
						mount = &adapter.VolumeMounts[i]
					}
				}
				if volume == nil || volume.PersistentVolumeClaim == nil || !volume.PersistentVolumeClaim.ReadOnly {
					t.Fatal("PVC must be mounted read-only")
				}
				if mount == nil || !mount.ReadOnly || mount.SubPath != pvc.subPath {
					t.Fatal("PVC must apply subpaths directly")
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
				cfg.testDataInitImage = "evalhub:test"
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
