package k8s

import (
	"encoding/json"
	"fmt"

	"github.com/eval-hub/eval-hub/internal/config"
)

// marshalSidecarForJobPod builds sidecar_config.json for the job ConfigMap from server
// sidecar YAML plus per-job fields (eval-hub URL, MLflow tracking URI, service CA paths).
func marshalSidecarForJobPod(cfg *config.Config, jc *jobConfig) ([]byte, error) {
	if cfg != nil && cfg.Sidecar == nil && jc != nil && jc.evalHubURL == "" && jc.mlflowTrackingURI == "" {
		return []byte("{}"), nil
	}

	var export *config.SidecarConfig
	if cfg != nil && cfg.Sidecar != nil {
		export = cloneSidecarConfig(cfg.Sidecar)
	} else {
		export = &config.SidecarConfig{}
	}
	if export.Port == 0 {
		export.Port = 8080
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
	}

	b, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sidecar config: %w", err)
	}
	return b, nil
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
	if sc.SidecarContainer != nil {
		scc := *sc.SidecarContainer
		if sc.SidecarContainer.Resources != nil {
			res := *sc.SidecarContainer.Resources
			scc.Resources = &res
			if sc.SidecarContainer.Resources.Requests != nil {
				req := *sc.SidecarContainer.Resources.Requests
				scc.Resources.Requests = &req
			}
			if sc.SidecarContainer.Resources.Limits != nil {
				lim := *sc.SidecarContainer.Resources.Limits
				scc.Resources.Limits = &lim
			}
		}
		out.SidecarContainer = &scc
	}
	return out
}
