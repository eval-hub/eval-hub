package mlflowclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const artifactsAPIBasePath = "/api/2.0/mlflow-artifacts/artifacts"

// UploadArtifact uploads artifact content to the MLflow proxied artifact store and returns
// the tracking-server URL used to download the artifact.
// artifactPath is the full artifact path (for example "1/abc123/artifacts/evaluation-card.json").
// Content length is set automatically when it can be determined from the reader; otherwise
// the request uses chunked transfer encoding.
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

	if c.ctx == nil {
		return "", fmt.Errorf("context is nil for MLFlow request")
	}

	req, err := http.NewRequestWithContext(c.ctx, http.MethodPut, artifactURL, content)
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	contentLength := readerContentLength(content)
	if contentLength >= 0 {
		req.ContentLength = contentLength
	}
	c.applyAuthHeader(req)
	if c.workspacesEnabled && c.workspace != "" {
		req.Header.Set("X-MLFLOW-WORKSPACE", c.workspace)
	}

	if contentLength >= 0 {
		c.logger.Info("MLFlow artifact upload started", "endpoint", endpoint, "bytes", contentLength)
	} else {
		c.logger.Info("MLFlow artifact upload started", "endpoint", endpoint)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload artifact: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		mlflowError := MLFlowError{}
		if err := json.Unmarshal(respBody, &mlflowError); err == nil && mlflowError.ErrorCode != "" {
			return "", &APIError{
				StatusCode:   resp.StatusCode,
				ResponseBody: string(respBody),
				MLFlowError:  &mlflowError,
			}
		}
		return "", &APIError{
			StatusCode:   resp.StatusCode,
			ResponseBody: string(respBody),
		}
	}

	c.logger.Info("MLFlow artifact upload successful", "endpoint", endpoint, "status", resp.StatusCode)
	return artifactURL, nil
}

func readerContentLength(r io.Reader) int64 {
	switch br := r.(type) {
	case *bytes.Reader:
		return int64(br.Len())
	case *bytes.Buffer:
		return int64(br.Len())
	case *strings.Reader:
		return int64(br.Len())
	}

	seeker, ok := r.(io.Seeker)
	if !ok {
		return -1
	}
	current, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return -1
	}
	end, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		return -1
	}
	if _, err := seeker.Seek(current, io.SeekStart); err != nil {
		return -1
	}
	return end - current
}

func buildArtifactUploadEndpoint(artifactPath string) (string, error) {
	segments := strings.Split(artifactPath, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(segment))
	}
	if len(escaped) == 0 {
		return "", fmt.Errorf("artifact path is required")
	}
	return artifactsAPIBasePath + "/" + strings.Join(escaped, "/"), nil
}
