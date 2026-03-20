package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

const (
	// DefaultSidecarConfigPath is where the job pod mounts sidecar_config.json.
	DefaultSidecarConfigPath = "/meta/sidecar_config.json"
	// SidecarReadyFilePath is where the sidecar writes its ready file (emptyDir in job pods).
	SidecarReadyFilePath = "/data/sidecar-ready"
	// SidecarTerminationFilePath is used for Kubernetes termination messages.
	SidecarTerminationFilePath = "/data/termination-log"
)

// LoadSidecarRuntimeConfig loads eval-runtime-sidecar configuration from sidecar_config.json only.
func LoadSidecarRuntimeConfig(sidecarJSONPath, version, build, buildDate string) (*config.Config, error) {
	if strings.TrimSpace(sidecarJSONPath) == "" {
		sidecarJSONPath = DefaultSidecarConfigPath
	}
	data, err := os.ReadFile(sidecarJSONPath)
	if err != nil {
		return nil, fmt.Errorf("read sidecar config %q: %w", sidecarJSONPath, err)
	}

	var sc config.SidecarConfig
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parse sidecar config JSON: %w", err)
	}
	if sc.EvalHub == nil {
		sc.EvalHub = &config.EvalHubClientConfig{}
	}

	cfg := &config.Config{
		Service: &config.ServiceConfig{
			Version:   version,
			Build:     build,
			BuildDate: buildDate,
			ReadyFile: SidecarReadyFilePath,
		},
		Sidecar: &sc,
	}

	if sc.MLFlow != nil && strings.TrimSpace(sc.MLFlow.TrackingURI) != "" {
		cfg.MLFlow = &config.MLFlowConfig{
			TrackingURI:        strings.TrimSpace(sc.MLFlow.TrackingURI),
			HTTPTimeout:        sc.MLFlow.HTTPTimeout,
			CACertPath:         sc.MLFlow.CACertPath,
			InsecureSkipVerify: sc.MLFlow.InsecureSkipVerify,
			TokenPath:          sc.MLFlow.TokenPath,
			Workspace:          sc.MLFlow.Workspace,
		}
	}

	return cfg, nil
}
