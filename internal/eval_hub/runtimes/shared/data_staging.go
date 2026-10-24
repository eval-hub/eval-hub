package shared

import "github.com/eval-hub/eval-hub/pkg/api"

// Internal runtime ABI. These paths and logical names are consumed by the
// post-processor; public result/calibration references remain unchanged.
const (
	DataStagingPlanFile       = "data-sources.json"
	DataStagingPlanEnv        = "EVALHUB_DATA_STAGING_PLAN"
	PostProcessorDataPathsEnv = "EVALHUB_POST_PROCESSOR_DATA_PATHS"
	PostProcessorStagingRoot  = "/mnt/evalhub/post-processing/staged"
	PostProcessorPVCRoot      = "/mnt/evalhub/post-processing/pvc"
	PostProcessorSecretRoot   = "/var/run/secrets/post-processor"
)

type DataStagingPlan struct {
	Sources []DataStagingSource `json:"sources"`
}

type DataStagingSource struct {
	Name   string          `json:"name"`
	Source api.TestDataRef `json:"source"`
}
