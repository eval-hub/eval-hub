package shared

import (
	"encoding/json"
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Internal runtime paths consumed by init and the post-processor.
const (
	InitJobSpecPathEnv        = "EVALHUB_JOB_SPEC_PATH"
	PostProcessorDataPathsEnv = "EVALHUB_POST_PROCESSOR_DATA_PATHS"
	PostProcessorStagingRoot  = "/mnt/evalhub/post-processing/staged"
	PostProcessorPVCRoot      = "/mnt/evalhub/post-processing/pvc"
	PostProcessorSecretRoot   = "/var/run/secrets/post-processor"
)

// NamedDataSource identifies an input's local destination; it is not a wire format.
type NamedDataSource struct {
	Name   string
	Source api.TestDataRef
}

// PostProcessingDataSources reads the existing adapter parameters. MLflow, OCI
// and eval_job results stay on demand; PVC references are mounted, not downloaded.
func PostProcessingDataSources(parameters map[string]any) ([]NamedDataSource, error) {
	raw, ok := parameters["operations"]
	if !ok {
		return nil, fmt.Errorf("post-processing job has no operations")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal post-processing operations: %w", err)
	}
	var operations api.StandalonePostProcessingOperations
	if err := json.Unmarshal(data, &operations); err != nil {
		return nil, fmt.Errorf("decode post-processing operations: %w", err)
	}
	ci := operations.ConfidenceInterval
	if ci == nil {
		return nil, fmt.Errorf("post-processing job has no supported operations")
	}
	var refs []NamedDataSource
	if result := ci.ResultsDataRef; result != nil {
		if result.S3 != nil || result.PVC != nil || result.Git != nil || result.HF != nil {
			refs = append(refs, NamedDataSource{Name: "results", Source: api.TestDataRef{S3: result.S3, PVC: result.PVC, Git: result.Git, HF: result.HF}})
		}
	}
	for i, ref := range ci.CalibrationDataRef {
		refs = append(refs, NamedDataSource{Name: fmt.Sprintf("calibration-%d", i), Source: api.TestDataRef{S3: ref.S3, PVC: ref.PVC, Git: ref.Git, HF: ref.HF}})
	}
	for _, ref := range refs {
		count := 0
		if ref.Source.S3 != nil {
			count++
		}
		if ref.Source.Git != nil {
			count++
		}
		if ref.Source.HF != nil {
			count++
		}
		if ref.Source.PVC != nil {
			count++
		}
		if count != 1 {
			return nil, fmt.Errorf("%s: exactly one data source is required", ref.Name)
		}
	}
	return refs, nil
}
