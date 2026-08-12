package shared

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

const (
	SidecarJobInfoFileName  = "sidecar-job-info.json"
	DefaultModelHTTPTimeout = 60 * time.Second
)

// SidecarJobInfo is the per-job config written to sidecar-job-info.json in local sidecar mode.
// The sidecar lazy-loads this file on the first request for a given job-id.
type SidecarJobInfo struct {
	Model *config.SidecarModelConfig `json:"model,omitempty"`
}

// WriteSidecarJobInfo writes sidecar-job-info.json into the job directory.
// It creates the job directory if it does not exist.
func WriteSidecarJobInfo(jobsBaseDir, jobID string, info *SidecarJobInfo) (string, error) {
	jobDir := filepath.Join(jobsBaseDir, jobID)
	if err := os.MkdirAll(jobDir, 0o750); err != nil {
		return "", fmt.Errorf("create job directory: %w", err)
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal sidecar job info: %w", err)
	}

	infoPath := filepath.Join(jobDir, SidecarJobInfoFileName)
	if err := os.WriteFile(infoPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write sidecar job info: %w", err)
	}

	return infoPath, nil
}

// RewriteModelURLForLocalSidecar returns a URL that routes through the local sidecar's
// per-job-id model proxy: sidecar-scheme://sidecar-host/model/<job-id>/<original-path>.
// The sidecar strips the /model/<job-id> prefix before forwarding to the real model upstream.
func RewriteModelURLForLocalSidecar(sidecarBaseURL, jobID, modelURL string) (string, error) {
	sidecar, err := url.Parse(strings.TrimSuffix(sidecarBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid sidecar base URL %q: %w", sidecarBaseURL, err)
	}
	if sidecar.Host == "" {
		return "", fmt.Errorf("invalid sidecar base URL %q: missing host", sidecarBaseURL)
	}
	model, err := url.Parse(strings.TrimSuffix(modelURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid model URL %q: %w", modelURL, err)
	}
	out := &url.URL{
		Scheme:   sidecar.Scheme,
		Host:     sidecar.Host,
		Path:     "/model/" + jobID + model.Path,
		RawQuery: model.RawQuery,
		Fragment: model.Fragment,
	}
	return out.String(), nil
}
