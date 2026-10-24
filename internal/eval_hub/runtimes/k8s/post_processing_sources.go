package k8s

import (
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	postProcessorSecretMountPath    = shared.PostProcessorSecretRoot
	postProcessorSecretVolumePrefix = "post-processor-secret-"
	postProcessorDataVolumeName     = "post-processor-data"
)

func postProcessorReferences(evaluation *api.EvaluationJobResource) ([]shared.DataStagingSource, error) {
	if evaluation == nil || !postprocessing.IsPostProcessingJob(&evaluation.EvaluationJobConfig) {
		return nil, nil
	}
	operations, err := postprocessing.OperationsFromJob(&evaluation.EvaluationJobConfig)
	if err != nil {
		return nil, fmt.Errorf("read post-processing calibration data refs: %w", err)
	}
	var refs []shared.DataStagingSource
	ci := operations.ConfidenceInterval
	if result := ci.ResultsDataRef; result != nil {
		if result.S3 != nil || result.PVC != nil || result.Git != nil || result.HF != nil {
			refs = append(refs, shared.DataStagingSource{Name: "results", Source: api.TestDataRef{S3: result.S3, PVC: result.PVC, Git: result.Git, HF: result.HF}})
		}
	}
	for i, ref := range ci.CalibrationDataRef {
		refs = append(refs, shared.DataStagingSource{Name: fmt.Sprintf("calibration-%d", i), Source: api.TestDataRef{S3: ref.S3, PVC: ref.PVC, Git: ref.Git, HF: ref.HF}})
	}
	return refs, nil
}

func postProcessorStagingPlan(evaluation *api.EvaluationJobResource) (*shared.DataStagingPlan, error) {
	refs, err := postProcessorReferences(evaluation)
	if err != nil {
		return nil, err
	}
	var sources []shared.DataStagingSource
	for _, ref := range refs {
		if ref.Source.S3 != nil || ref.Source.Git != nil || ref.Source.HF != nil {
			sources = append(sources, ref)
		}
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return &shared.DataStagingPlan{Sources: sources}, nil
}

func postProcessorInitContainer(cfg *jobConfig, resources corev1.ResourceRequirements) (*corev1.Container, []corev1.Volume, error) {
	if cfg.postProcessorStaging == nil {
		return nil, nil, nil
	}
	if cfg.testDataInitImage == "" {
		return nil, nil, fmt.Errorf("init image is required for post-processing source staging")
	}
	volumes := []corev1.Volume{{Name: postProcessorDataVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: testDataEmptyDirVolumeSource()}}}
	mounts := []corev1.VolumeMount{
		{Name: postProcessorDataVolumeName, MountPath: shared.PostProcessorStagingRoot},
		{Name: jobSpecVolumeName, MountPath: "/meta/" + shared.DataStagingPlanFile, SubPath: shared.DataStagingPlanFile, ReadOnly: true},
	}
	for i, name := range cfg.postProcessorSecrets {
		volume := fmt.Sprintf("%s%d", postProcessorSecretVolumePrefix, i)
		volumes = append(volumes, corev1.Volume{Name: volume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name}}})
		mounts = append(mounts, corev1.VolumeMount{Name: volume, MountPath: postProcessorSecretMountPath + "/" + name, ReadOnly: true})
	}
	return &corev1.Container{
		Name:            initContainerName,
		Image:           cfg.testDataInitImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{defaultTestDataInitCmd},
		Resources:       resources,
		SecurityContext: defaultSecurityContext(),
		Env:             []corev1.EnvVar{{Name: shared.DataStagingPlanEnv, Value: "/meta/" + shared.DataStagingPlanFile}},
		VolumeMounts:    mounts,
	}, volumes, nil
}

// postProcessorSecretNames discovers credentials mounted only in the existing init container.
// OCI credentials are configured separately on the registry sidecar.
func postProcessorSecretNames(evaluation *api.EvaluationJobResource) ([]string, error) {
	if evaluation == nil || !postprocessing.IsPostProcessingJob(&evaluation.EvaluationJobConfig) {
		return nil, nil
	}
	operations, err := postprocessing.OperationsFromJob(&evaluation.EvaluationJobConfig)
	if err != nil {
		return nil, fmt.Errorf("read post-processing source secrets: %w", err)
	}
	var names []string
	addRefs := func(s3 *api.S3TestDataRef, git *api.GitTestDataRef, hf *api.HFTestDataRef) {
		if s3 != nil {
			names = append(names, s3.SecretRef)
		}
		if git != nil {
			names = append(names, git.SecretRef)
		}
		if hf != nil {
			names = append(names, hf.SecretRef)
		}
	}
	ci := operations.ConfidenceInterval
	if result := ci.ResultsDataRef; result != nil {
		addRefs(result.S3, result.Git, result.HF)
	}
	for _, ref := range ci.CalibrationDataRef {
		addRefs(ref.S3, ref.Git, ref.HF)
	}
	var secrets []string
	seen := make(map[string]bool)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		if len(validation.IsDNS1123Subdomain(name)) != 0 {
			return nil, fmt.Errorf("post-processing secret_ref must name a Kubernetes Secret")
		}
		seen[name] = true
		secrets = append(secrets, name)
	}
	return secrets, nil
}
