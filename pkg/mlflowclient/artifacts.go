package mlflowclient

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/opendatahub-io/mlflow-go/mlflow/artifacts"
)

const artifactsAPIBasePath = "/api/2.0/mlflow-artifacts/artifacts"

// UploadArtifact uploads artifact content to the MLflow proxied artifact store and returns
// the tracking-server URL used to download the artifact.
// artifactPath is the full artifact path (for example "1/abc123/artifacts/evaluation-card.json").
// The content is streamed by mlflow-go with no size cap.
func (c *Client) UploadArtifact(artifactPath string, content io.Reader, contentType string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("mlflow client is nil")
	}
	if content == nil {
		return "", fmt.Errorf("artifact content reader is nil")
	}
	artifactPath = strings.TrimPrefix(strings.TrimSpace(artifactPath), "/")
	if artifactPath == "" {
		return "", fmt.Errorf("artifact path is required")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	endpoint, err := buildArtifactUploadEndpoint(artifactPath)
	if err != nil {
		return "", err
	}
	artifactURL := c.baseURL + endpoint

	d, err := c.resolveDelegate()
	if err != nil {
		return "", err
	}
	if err := d.Artifacts().UploadArtifact(
		c.Context(),
		artifactPath,
		content,
		artifacts.WithUploadContentType(contentType),
	); err != nil {
		return "", mapError(err)
	}
	return artifactURL, nil
}

func buildArtifactUploadEndpoint(artifactPath string) (string, error) {
	segments := strings.Split(artifactPath, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("artifact path contains invalid segment %q", segment)
		}
		escaped = append(escaped, url.PathEscape(segment))
	}
	if len(escaped) == 0 {
		return "", fmt.Errorf("artifact path is required")
	}
	return artifactsAPIBasePath + "/" + strings.Join(escaped, "/"), nil
}

// DownloadArtifact fetches artifact content from the MLflow proxied artifact store and
// returns the response body as an io.ReadCloser for streaming. The caller is responsible
// for closing the returned reader when done.
// artifactPath is the full artifact path (for example "1/abc123/artifacts/evaluation-card.json").
func (c *Client) DownloadArtifact(artifactPath string) (io.ReadCloser, error) {
	if c == nil {
		return nil, fmt.Errorf("mlflow client is nil")
	}
	artifactPath = strings.TrimPrefix(strings.TrimSpace(artifactPath), "/")
	if artifactPath == "" {
		return nil, fmt.Errorf("artifact path is required")
	}
	// Validate the path with the same rules used for uploads (rejects "." / "..").
	if _, err := buildArtifactUploadEndpoint(artifactPath); err != nil {
		return nil, err
	}

	d, err := c.resolveDelegate()
	if err != nil {
		return nil, err
	}
	rc, err := d.Artifacts().DownloadArtifactByPath(c.Context(), artifactPath)
	if err != nil {
		return nil, mapError(err)
	}
	return rc, nil
}
