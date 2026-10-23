package k8s

import (
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/pkg/api"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	postProcessorSecretMountPath    = "/var/run/secrets/post-processor"
	postProcessorSecretVolumePrefix = "post-processor-secret-"
	envPostProcessorSecretRoot      = "EVALHUB_POST_PROCESSOR_SECRET_ROOT"
)

// postProcessorSecretNames discovers credentials read by the adapter itself.
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
