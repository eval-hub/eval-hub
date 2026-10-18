package ociclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
)

var descriptorDigestPattern = regexp.MustCompile(`^[a-z0-9]+([+._-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$`)

// MaxManifestResponseBytes bounds each child manifest response to 4 MiB.
// The OCI Distribution Specification v1.1.1 states:
// "Client and registry implementations SHOULD expect to be able to support manifest pushes of at least 4 megabytes."
// https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-manifests
// This read limit is an EvalHub policy informed by that push-size guidance;
// the specification does not define 4 MiB as a maximum manifest size.
const MaxManifestResponseBytes = 4 * 1024 * 1024

// ValidateSHA256Digest checks a canonical digest before it is used in a registry path.
func ValidateSHA256Digest(digest string) error {
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return fmt.Errorf("invalid SHA-256 digest %q", digest)
	}
	if _, err := hex.DecodeString(digest[7:]); err != nil || digest != strings.ToLower(digest) {
		return fmt.Errorf("invalid SHA-256 digest %q", digest)
	}
	return nil
}

// GetManifest retrieves an OCI image or Docker schema-2 manifest by digest from
// the configured repository. It verifies the original bytes and returns their descriptor.
// It does not retrieve blobs, follow descriptor URLs, or resolve tags.
func (c *Client) GetManifest(ctx context.Context, digest string) (Descriptor, error) {
	if err := ValidateSHA256Digest(digest); err != nil {
		return Descriptor{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.registryURL("/v2/"+c.repository+"/manifests/"+digest), nil)
	if err != nil {
		return Descriptor{}, err
	}
	req.Header.Set("Accept", MediaTypeImageManifest+", "+MediaTypeDockerManifest)
	resp, err := c.do(req)
	if err != nil {
		return Descriptor{}, fmt.Errorf("get manifest %s: %w", digest, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Descriptor{}, registryHTTPError("get manifest", resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxManifestResponseBytes+1))
	if err != nil {
		return Descriptor{}, fmt.Errorf("read manifest %s: %w", digest, err)
	}
	if len(body) > MaxManifestResponseBytes {
		return Descriptor{}, fmt.Errorf("manifest %s exceeds %d bytes", digest, MaxManifestResponseBytes)
	}
	if blobDigest(body) != digest {
		return Descriptor{}, fmt.Errorf("manifest content digest does not match %s", digest)
	}
	if err := checkResponseDigest(resp, digest); err != nil {
		return Descriptor{}, err
	}
	if resp.ContentLength >= 0 && resp.ContentLength != int64(len(body)) {
		return Descriptor{}, fmt.Errorf("manifest Content-Length does not match response bytes")
	}
	mediaType, err := validateChildManifest(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return Descriptor{}, fmt.Errorf("validate manifest %s: %w", digest, err)
	}
	return Descriptor{MediaType: mediaType, Size: int64(len(body)), Digest: digest}, nil
}

// validateChildManifest checks the manifest envelope and descriptors, returning
// its supported media type only when the body and HTTP Content-Type agree.
func validateChildManifest(body []byte, contentType string) (string, error) {
	var child struct {
		SchemaVersion int               `json:"schemaVersion"`
		MediaType     string            `json:"mediaType"`
		Config        json.RawMessage   `json:"config"`
		Layers        []json.RawMessage `json:"layers"`
		Subject       json.RawMessage   `json:"subject"`
	}
	if err := json.Unmarshal(body, &child); err != nil {
		return "", err
	}
	if child.SchemaVersion != 2 || child.Layers == nil {
		return "", fmt.Errorf("schemaVersion 2 and layers array are required")
	}
	headerType := ""
	if contentType != "" {
		var err error
		headerType, _, err = mime.ParseMediaType(contentType)
		if err != nil {
			return "", fmt.Errorf("invalid Content-Type: %w", err)
		}
	}
	mediaType := child.MediaType
	if mediaType == "" {
		mediaType = headerType
	}
	if mediaType != MediaTypeImageManifest && mediaType != MediaTypeDockerManifest {
		return "", fmt.Errorf("unsupported child manifest media type %q", mediaType)
	}
	if headerType != "" && headerType != mediaType {
		return "", fmt.Errorf("Content-Type %q conflicts with manifest mediaType %q", headerType, mediaType)
	}
	if err := validateChildDescriptor(child.Config); err != nil {
		return "", fmt.Errorf("config descriptor: %w", err)
	}
	for i, layer := range child.Layers {
		if err := validateChildDescriptor(layer); err != nil {
			return "", fmt.Errorf("layer %d descriptor: %w", i, err)
		}
	}
	if len(child.Subject) > 0 {
		if err := validateChildDescriptor(child.Subject); err != nil {
			return "", fmt.Errorf("subject descriptor: %w", err)
		}
	}
	return mediaType, nil
}

// validateChildDescriptor checks required descriptor fields without fetching the
// referenced content. Unrecognized digest algorithms need only match OCI syntax.
func validateChildDescriptor(raw json.RawMessage) error {
	var d struct {
		MediaType string `json:"mediaType"`
		Size      *int64 `json:"size"`
		Digest    string `json:"digest"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	if d.Size == nil || *d.Size < 0 {
		return fmt.Errorf("nonnegative size is required")
	}
	if mediaType, params, err := mime.ParseMediaType(d.MediaType); err != nil || !strings.Contains(mediaType, "/") || len(params) != 0 {
		return fmt.Errorf("invalid mediaType %q", d.MediaType)
	}
	// Child blob descriptors may use other OCI digest algorithms. Retrieval of the
	// enclosing manifest still requires SHA-256; no child blob is fetched here.
	parts := strings.SplitN(d.Digest, ":", 2)
	if !descriptorDigestPattern.MatchString(d.Digest) {
		return fmt.Errorf("invalid descriptor digest %q", d.Digest)
	}
	switch parts[0] {
	case "sha256", "sha512":
		length := 64
		if parts[0] == "sha512" {
			length = 128
		}
		if _, err := hex.DecodeString(parts[1]); err != nil || len(parts[1]) != length || parts[1] != strings.ToLower(parts[1]) {
			return fmt.Errorf("invalid descriptor digest %q", d.Digest)
		}
	}
	return nil
}

// checkResponseDigest rejects a conflicting registry digest when the optional
// Docker-Content-Digest response header is present.
// See the OCI Distribution Specification v1.1.1 manifest response contracts:
// https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pulling-manifests
// https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-manifests
func checkResponseDigest(resp *http.Response, expected string) error {
	if actual := resp.Header.Get("Docker-Content-Digest"); actual != "" && actual != expected {
		return fmt.Errorf("registry digest %q conflicts with expected %q", actual, expected)
	}
	return nil
}

// PushIndex publishes exact, deterministic JSON bytes and returns the final tag
// and locally computed digest. The client is shared by local and cluster callers.
func (c *Client) PushIndex(ctx context.Context, tag string, index Index) (*api.OCIArtifactReference, error) {
	if tag == "" || len(tag) > 128 {
		return nil, fmt.Errorf("invalid index tag")
	}
	if err := api.ValidateOCIExportPrefix(tag); err != nil {
		return nil, err
	}
	if index.SchemaVersion != 2 || index.MediaType != MediaTypeImageIndex || len(index.Manifests) == 0 {
		return nil, fmt.Errorf("invalid OCI index")
	}
	body, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshal index: %w", err)
	}
	if err := c.putManifestWithMediaType(ctx, tag, body, MediaTypeImageIndex, true); err != nil {
		return nil, fmt.Errorf("push index: %w", err)
	}
	digest := blobDigest(body)
	return &api.OCIArtifactReference{OCIDigest: digest, OCIReference: strings.TrimPrefix(strings.TrimPrefix(c.registry, "https://"), "http://") + "/" + c.repository + ":" + tag + "@" + digest}, nil
}
