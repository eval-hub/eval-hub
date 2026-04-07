package k8s

import (
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

// sidecarForJobPod builds sidecar_config.json for the job ConfigMap from server
// sidecar YAML plus per-job fields. Omits sidecar_container (image/resources); that is only for job spec.
// evaluationModelURL is the model endpoint from the evaluation request (upstream for the sidecar proxy).
// jobSpec.Model.URL may be rewritten to the in-pod /model path for adapters; it is not used here.
func sidecarForJobPod(cfg *config.Config, jc *jobConfig, evaluationModelURL string) (*config.SidecarConfig, error) {
	if cfg != nil && cfg.Sidecar == nil && jc != nil && jc.evalHubURL == "" && jc.mlflowTrackingURI == "" && strings.TrimSpace(evaluationModelURL) == "" {
		return nil, nil
	}

	var export *config.SidecarConfig
	if cfg != nil && cfg.Sidecar != nil {
		export = cloneSidecarConfig(cfg.Sidecar)
	} else {
		export = &config.SidecarConfig{}
	}
	if export.Port == 0 {
		export.Port = int(defaultSidecarPort)
	}

	if jc != nil {
		if jc.evalHubURL != "" {
			if export.EvalHub == nil {
				export.EvalHub = &config.EvalHubClientConfig{}
			}
			export.EvalHub.BaseURL = jc.evalHubURL
			if jc.serviceCAConfigMap != "" {
				export.EvalHub.CACertPath = serviceCAMountPath + "/" + serviceCABundleFile
				export.EvalHub.InsecureSkipVerify = false
			}
		}
		if jc.mlflowTrackingURI != "" {
			if export.MLFlow == nil {
				export.MLFlow = &config.SidecarMLFlowConfig{}
			}
			export.MLFlow.TrackingURI = jc.mlflowTrackingURI
			export.MLFlow.TokenPath = mlflowTokenMountPath + "/" + mlflowTokenFile
			export.MLFlow.Workspace = jc.mlflowWorkspace
			if cfg != nil && cfg.MLFlow != nil {
				export.MLFlow.HTTPTimeout = cfg.MLFlow.HTTPTimeout
				export.MLFlow.InsecureSkipVerify = cfg.MLFlow.InsecureSkipVerify
				if jc.serviceCAConfigMap != "" {
					export.MLFlow.CACertPath = serviceCAMountPath + "/" + serviceCABundleFile
				} else {
					export.MLFlow.CACertPath = cfg.MLFlow.CACertPath
				}
			} else if jc.serviceCAConfigMap != "" {
				export.MLFlow.CACertPath = serviceCAMountPath + "/" + serviceCABundleFile
			}
		}

		if strings.TrimSpace(evaluationModelURL) != "" {
			if export.Model == nil {
				export.Model = &config.SidecarModelConfig{}
			}
			export.Model.URL = strings.TrimSpace(evaluationModelURL)
			if jc.modelAuthSecretRef != "" {
				export.Model.AuthAPIKeyPath = modelAuthMountPath + "/" + modelAuthSecretAPIKeyFile
				export.Model.AuthCACertPath = modelAuthMountPath + "/" + modelAuthSecretCACertFile
			}
			if cfg != nil && cfg.MLFlow != nil && cfg.MLFlow.HTTPTimeout > 0 {
				export.Model.HTTPTimeout = cfg.MLFlow.HTTPTimeout
			}
		}
	}

	return export, nil
}

func cloneSidecarConfig(sc *config.SidecarConfig) *config.SidecarConfig {
	if sc == nil {
		return nil
	}
	out := &config.SidecarConfig{Port: sc.Port, BaseURL: sc.BaseURL}
	if sc.EvalHub != nil {
		eh := *sc.EvalHub
		out.EvalHub = &eh
	}
	if sc.MLFlow != nil {
		mf := *sc.MLFlow
		out.MLFlow = &mf
	}
	if sc.Model != nil {
		md := *sc.Model
		out.Model = &md
	}
	if sc.OCI != nil {
		oci := *sc.OCI
		out.OCI = &oci
	}
	// SidecarContainer (image/resources) is for eval-hub job scheduling only, not the sidecar process.
	return out
}
