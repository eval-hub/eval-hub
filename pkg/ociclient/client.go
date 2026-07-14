package ociclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// DefaultChunkSize is the PATCH upload chunk size for streaming blob uploads.
	DefaultChunkSize = 5 * 1024 * 1024
)

// UploadBlobOptions configures streaming blob uploads.
type UploadBlobOptions struct {
	// KnownDigest skips upload when the blob already exists in the registry.
	KnownDigest string
	// ChunkSize overrides DefaultChunkSize for PATCH uploads. When zero, DefaultChunkSize is used.
	ChunkSize int
}

// Client pushes artifacts to an OCI distribution registry.
type Client struct {
	registry   string
	repository string
	httpClient *http.Client
	auth       *authenticator
}

// NewClient creates an OCI registry client for the given coordinates and credentials.
// It wires together the registry base URL, repository path, and authenticator needed for
// Distribution API calls during eval card export.
func NewClient(registryHost, repository string, creds Credentials, httpClient *http.Client) (*Client, error) {
	registry := NormalizeRegistryHost(registryHost)
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if registry == "" {
		return nil, fmt.Errorf("registry host is required")
	}
	if repository == "" {
		return nil, fmt.Errorf("repository is required")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("http client is required")
	}
	return &Client{
		registry:   registry,
		repository: repository,
		httpClient: httpClient,
		auth:       newAuthenticator(registry, repository, creds, httpClient),
	}, nil
}

// PushEvaluationCard uploads cardJSON as an OCI artifact for the given evaluation job. The manifest
// tag and blob descriptor annotations always include jobID so each job is addressable and identifiable
// in the registry.
func (c *Client) PushEvaluationCard(ctx context.Context, jobID string, cardJSON []byte, ociTag string, annotations map[string]string) error {
	if err := validateEvaluationJobID(jobID); err != nil {
		return err
	}
	tag := EvaluationCardManifestTag(jobID, ociTag)
	if tag == "" {
		return fmt.Errorf("manifest tag is required")
	}
	if len(cardJSON) == 0 {
		return fmt.Errorf("evaluation card content is empty")
	}

	configBlob, err := artifactConfigBlobForJob(jobID)
	if err != nil {
		return fmt.Errorf("build artifact config: %w", err)
	}
	configDigest, configSize, err := c.ensureBlob(ctx, configBlob)
	if err != nil {
		return fmt.Errorf("upload artifact config: %w", err)
	}
	layerDigest, layerSize, err := c.ensureBlob(ctx, cardJSON)
	if err != nil {
		return fmt.Errorf("upload evaluation card layer: %w", err)
	}

	manifestBytes, err := json.Marshal(manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeImageManifest,
		Config: descriptor{
			MediaType:   MediaTypeArtifactConfig,
			Size:        configSize,
			Digest:      configDigest,
			Annotations: configAnnotations(jobID),
		},
		Layers: []descriptor{{
			MediaType:   MediaTypeEvaluationCardLayer,
			Size:        layerSize,
			Digest:      layerDigest,
			Annotations: layerAnnotations(jobID),
		}},
		Annotations: mergeEvaluationCardAnnotations(jobID, tag, annotations),
	})
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := c.putManifest(ctx, tag, manifestBytes); err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}
	return nil
}

// UploadBlob streams content to the registry using chunked PATCH uploads. The digest is computed
// while reading, so callers do not need to buffer the entire payload in memory. When KnownDigest is
// set and the blob already exists, the upload is skipped.
func (c *Client) UploadBlob(ctx context.Context, r io.Reader, opts UploadBlobOptions) (digest string, size int64, err error) {
	if r == nil {
		return "", 0, fmt.Errorf("reader is required")
	}
	if opts.KnownDigest != "" {
		exists, existsErr := c.blobExists(ctx, opts.KnownDigest)
		if existsErr != nil {
			return "", 0, existsErr
		}
		if exists {
			return opts.KnownDigest, 0, nil
		}
	}
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return c.uploadBlobChunked(ctx, r, chunkSize)
}

// ensureBlob uploads content when absent and returns its digest and size for manifest assembly.
// Skipping existing blobs avoids redundant uploads when the same content was pushed before.
// Small payloads use a monolithic PUT; larger ones stream via PATCH chunks.
func (c *Client) ensureBlob(ctx context.Context, content []byte) (digest string, size int64, err error) {
	digest = blobDigest(content)
	size = int64(len(content))
	exists, err := c.blobExists(ctx, digest)
	if err != nil {
		return "", 0, err
	}
	if exists {
		return digest, size, nil
	}
	if len(content) <= DefaultChunkSize {
		if err := c.uploadBlobMonolithic(ctx, content, digest); err != nil {
			return "", 0, err
		}
		return digest, size, nil
	}
	uploadedDigest, uploadedSize, err := c.uploadBlobChunked(ctx, bytes.NewReader(content), DefaultChunkSize)
	if err != nil {
		return "", 0, err
	}
	if uploadedDigest != digest {
		return "", 0, fmt.Errorf("uploaded digest %q does not match expected %q", uploadedDigest, digest)
	}
	return digest, uploadedSize, nil
}

// blobDigest returns the OCI content digest (sha256) for a blob payload.
func blobDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// blobExists checks whether the registry already stores a blob with the given digest.
func (c *Client) blobExists(ctx context.Context, digest string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.registryURL("/v2/"+c.repository+"/blobs/"+digest), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound, http.StatusUnauthorized:
		return false, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("blob head failed with status %d: %s", resp.StatusCode, string(body))
	}
}

// startBlobUpload begins an OCI blob upload session and returns the upload URL from the Location header.
func (c *Client) startBlobUpload(ctx context.Context) (string, error) {
	startURL := c.registryURL("/v2/" + c.repository + "/blobs/uploads/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("start blob upload failed with status %d: %s", resp.StatusCode, string(body))
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("start blob upload missing Location header")
	}
	return c.resolveLocation(location)
}

// uploadBlobMonolithic performs a single-shot blob upload (POST then PUT with digest and body).
func (c *Client) uploadBlobMonolithic(ctx context.Context, content []byte, digest string) error {
	uploadURL, err := c.startBlobUpload(ctx)
	if err != nil {
		return err
	}
	sep := "?"
	if strings.Contains(uploadURL, "?") {
		sep = "&"
	}
	uploadURL += sep + "digest=" + url.QueryEscape(digest)

	putReq, err := c.newRequestWithBody(ctx, http.MethodPut, uploadURL, content)
	if err != nil {
		return err
	}
	putResp, err := c.do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("complete blob upload failed with status %d: %s", putResp.StatusCode, string(body))
	}
	return nil
}

// uploadBlobChunked streams content with PATCH chunks, then finalizes the upload with PUT digest.
func (c *Client) uploadBlobChunked(ctx context.Context, r io.Reader, chunkSize int) (digest string, size int64, err error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	uploadURL, err := c.startBlobUpload(ctx)
	if err != nil {
		return "", 0, err
	}

	hasher := sha256.New()
	buf := make([]byte, chunkSize)
	var offset int64

	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, hashErr := hasher.Write(chunk); hashErr != nil {
				return "", 0, hashErr
			}
			end := offset + int64(n) - 1
			if patchErr := c.patchBlobChunk(ctx, uploadURL, offset, end, chunk); patchErr != nil {
				return "", 0, patchErr
			}
			offset += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", 0, readErr
		}
	}

	digest = "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if err := c.completeBlobUpload(ctx, uploadURL, digest); err != nil {
		return "", 0, err
	}
	return digest, offset, nil
}

// patchBlobChunk uploads one chunk to an in-progress blob upload session.
func (c *Client) patchBlobChunk(ctx context.Context, uploadURL string, start, end int64, chunk []byte) error {
	req, err := c.newRequestWithBody(ctx, http.MethodPatch, uploadURL, chunk)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Range", strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("patch blob chunk failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// completeBlobUpload finalizes a chunked upload by sending PUT with the content digest.
func (c *Client) completeBlobUpload(ctx context.Context, uploadURL, digest string) error {
	sep := "?"
	if strings.Contains(uploadURL, "?") {
		sep = "&"
	}
	finalURL := uploadURL + sep + "digest=" + url.QueryEscape(digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, finalURL, nil)
	if err != nil {
		return err
	}
	req.ContentLength = 0
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("complete blob upload failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// putManifest publishes the artifact manifest under the given tag in the target repository.
func (c *Client) putManifest(ctx context.Context, tag string, manifestJSON []byte) error {
	req, err := c.newRequestWithBody(ctx, http.MethodPut, c.registryURL("/v2/"+c.repository+"/manifests/"+tag), manifestJSON)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", MediaTypeImageManifest)
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put manifest failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// registryURL joins the configured registry origin with a Distribution API path.
func (c *Client) registryURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return c.registry + path
}

// resolveLocation converts a blob upload Location header into an absolute URL. Registries may
// return relative paths that must be resolved against the registry origin.
func (c *Client) resolveLocation(location string) (string, error) {
	parsed, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("parse upload location: %w", err)
	}
	if parsed.IsAbs() {
		return location, nil
	}
	base, err := url.Parse(c.registry)
	if err != nil {
		return "", fmt.Errorf("parse registry url: %w", err)
	}
	return base.ResolveReference(parsed).String(), nil
}

// newRequestWithBody builds a request with a replayable body so do can retry after token refresh.
func (c *Client) newRequestWithBody(ctx context.Context, method, rawURL string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))
	return req, nil
}

// do sends a registry request with Bearer auth and transparently refreshes the token on 401.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	c.auth.authorize(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	_ = resp.Body.Close()
	if err := c.auth.refreshToken(); err != nil {
		return nil, fmt.Errorf("refresh registry token: %w", err)
	}
	retry, err := c.cloneRequest(req)
	if err != nil {
		return nil, err
	}
	c.auth.authorize(retry)
	return c.httpClient.Do(retry)
}

// cloneRequest duplicates a request and resets its body for a single auth retry.
func (c *Client) cloneRequest(req *http.Request) (*http.Request, error) {
	retry := req.Clone(req.Context())
	if req.GetBody == nil {
		return retry, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	retry.Body = body
	retry.ContentLength = req.ContentLength
	return retry, nil
}
