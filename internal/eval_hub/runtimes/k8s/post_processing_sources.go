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

func postProcessorReferences(evaluation *api.EvaluationJobResource) ([]shared.NamedDataSource, error) {
	if evaluation == nil || !postprocessing.IsPostProcessingJob(&evaluation.EvaluationJobConfig) {
		return nil, nil
	}
	refs, err := shared.PostProcessingDataSources(evaluation.Benchmarks[0].Parameters)
	if err != nil {
		return nil, fmt.Errorf("read post-processing calibration data refs: %w", err)
	}
	return refs, nil
}

func postProcessorDownloadSources(evaluation *api.EvaluationJobResource) ([]shared.NamedDataSource, error) {
	refs, err := postProcessorReferences(evaluation)
	if err != nil {
		return nil, err
	}
	var sources []shared.NamedDataSource
	for _, ref := range refs {
		if ref.Source.S3 != nil || ref.Source.Git != nil || ref.Source.HF != nil {
			sources = append(sources, ref)
		}
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return sources, nil
}

func postProcessorInitContainer(cfg *jobConfig, resources corev1.ResourceRequirements) (*corev1.Container, []corev1.Volume, error) {
	if cfg.postProcessorDownloads == nil {
		return nil, nil, nil
	}
	if cfg.testDataInitImage == "" {
		return nil, nil, fmt.Errorf("init image is required for post-processing source staging")
	}
	volumes := []corev1.Volume{{Name: postProcessorDataVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: testDataEmptyDirVolumeSource()}}}
	mounts := []corev1.VolumeMount{
		{Name: postProcessorDataVolumeName, MountPath: shared.PostProcessorStagingRoot},
		{Name: jobSpecVolumeName, MountPath: "/meta/" + jobSpecFileName, SubPath: jobSpecFileName, ReadOnly: true},
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
		Env:             []corev1.EnvVar{{Name: shared.InitJobSpecPathEnv, Value: "/meta/" + jobSpecFileName}},
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
