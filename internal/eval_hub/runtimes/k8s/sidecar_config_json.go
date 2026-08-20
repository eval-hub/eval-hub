package k8s

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/otel"
)

// sidecarForJobPod builds sidecar_config.json for the job ConfigMap from server
// sidecar YAML plus per-job fields. Omits sidecar_container (image/resources); that is only for job spec.
func sidecarForJobPod(cfg *config.Config, jc *jobConfig) (*config.SidecarConfig, error) {
	if cfg != nil && cfg.Sidecar == nil && jc != nil && jc.evalHubURL == "" && jc.mlflowTrackingURI == "" && jc.modelTargetURL == "" {
		return nil, nil
	}

	var export *config.SidecarConfig
	if cfg != nil && cfg.Sidecar != nil {
		export = cloneSidecarConfig(cfg.Sidecar)
	} else {
		export = &config.SidecarConfig{}
	}
	export.BaseURL = export.EffectiveBaseURL()

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
		if hasGitTestData(jc) {
			export.InitContainer = &config.InitContainerConfig{IsGitJob: true}
		}
		if jc.mlflowTrackingURI != "" {
			if export.MLFlow == nil {
				export.MLFlow = &config.SidecarMLFlowConfig{}
			}
			export.MLFlow.TrackingURI = jc.mlflowTrackingURI
			export.MLFlow.TokenPath = mlflowAuthMountPath + "/" + mlflowTokenFile
			export.MLFlow.Workspace = jc.mlflowWorkspace
			if cfg != nil && cfg.MLFlow != nil {
				export.MLFlow.HTTPTimeout = cfg.MLFlow.HTTPTimeout
			}
			export.MLFlow.CACertPath = mlflowCACertPathForJob(jc, cfg)
		}
		if jc.modelTargetURL != "" {
			mc := &config.SidecarModelConfig{URL: jc.modelTargetURL}
			// AuthSecretMountPath is only set when a credentials secret is configured;
			// open models have no secret mount but still use the sidecar proxy.
			if jc.modelAuthSecretRef != "" {
				mc.AuthSecretMountPath = modelAuthMountPath
			}
			export.Model = mc
		}
	}

	if otelCfg := otelConfigForJobPod(cfg); otelCfg != nil {
		export.OTEL = otelCfg
	}

	return export, nil
}

// mlflowCACertPathForJob returns the PEM CA path job containers should use for MLflow TLS.
// Preference order:
//  1. Operator-merged MLflow CA bundle mounted on the job pod
//  2. Top-level mlflow.ca_cert_path / MLFLOW_CA_CERT_PATH from the API process
//  3. OpenShift service-serving CA (legacy / EvalHub-only trust)
func mlflowCACertPathForJob(jc *jobConfig, cfg *config.Config) string {
	if jc != nil && jc.mlflowCABundleConfigMap != "" {
		return mlflowCABundleMountPath + "/" + mlflowCABundleFile
	}
	if cfg != nil && cfg.MLFlow != nil && cfg.MLFlow.CACertPath != "" {
		return cfg.MLFlow.CACertPath
	}
	if jc != nil && jc.serviceCAConfigMap != "" {
		return serviceCAMountPath + "/" + serviceCABundleFile
	}
	return ""
}

func otelConfigForJobPod(cfg *config.Config) *config.OTELConfig {
	if cfg == nil || cfg.OTEL == nil || !cfg.OTEL.Enabled {
		return nil
	}
	out := *cfg.OTEL
	out.TLSConfig = nil
	if out.ServiceName == "" {
		out.ServiceName = otel.SidecarServiceName
	}
	return &out
}

func cloneSidecarConfig(sc *config.SidecarConfig) *config.SidecarConfig {
	if sc == nil {
		return nil
	}
	out := &config.SidecarConfig{LocalMode: sc.LocalMode, BaseURL: sc.BaseURL, Port: sc.Port}
	if sc.EvalHub != nil {
		eh := *sc.EvalHub
		out.EvalHub = &eh
	}
	if sc.MLFlow != nil {
		mf := *sc.MLFlow
		out.MLFlow = &mf
	}
	if sc.OCI != nil {
		oci := *sc.OCI
		out.OCI = &oci
	}
	if sc.Model != nil {
		m := *sc.Model
		out.Model = &m
	}
	if sc.InitContainer != nil {
		ic := *sc.InitContainer
		out.InitContainer = &ic
	}
	if sc.OTEL != nil {
		o := *sc.OTEL
		out.OTEL = &o
	}
	// SidecarContainer (image/resources) is for eval-hub job scheduling only, not the sidecar process.
	return out
}
