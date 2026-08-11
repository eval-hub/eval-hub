package features

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PaesslerAG/jsonpath"
)

// --- OCI Artifact Step Definitions ---

// ociBaseURL returns the base URL for OCI registry access (for test code to fetch artifacts)
// Returns empty string if OCI_REGISTRY is not set (scenarios will be skipped by requireOCIConfiguration)
func ociBaseURL() string {
	baseURL := os.Getenv("OCI_REGISTRY")
	return strings.TrimRight(baseURL, "/")
}

// getOCIHTTPClient returns a shared HTTP client for OCI operations
func (tc *scenarioConfig) getOCIHTTPClient() *http.Client {
	if tc.ociHTTPClient == nil {
		tc.ociHTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return tc.ociHTTPClient
}

// iFetchOCIManifestByRepoAndTag fetches OCI manifest and then the blob
func (tc *scenarioConfig) iFetchOCIManifestByRepoAndTag(repository, tag string) error {
	repositoryResolved, err := tc.getValue(repository)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to resolve repository value %q: %w", repository, err))
	}

	tagResolved, err := tc.getValue(tag)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to resolve tag value %q: %w", tag, err))
	}

	baseURL := ociBaseURL()
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", baseURL, repositoryResolved, tagResolved)

	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to create manifest request: %w", err))
	}

	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := tc.getOCIHTTPClient().Do(req)
	if err != nil {
		tc.ociManifestError = err
		return tc.logError(fmt.Errorf("failed to fetch OCI manifest: %w", err))
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			tc.logDebug("Failed to close OCI manifest response body: %v\n", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tc.ociManifestError = err
		return tc.logError(fmt.Errorf("failed to read OCI manifest response: %w", err))
	}

	tc.ociManifestStatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		tc.ociManifestError = fmt.Errorf("OCI manifest fetch returned status %d: %s", resp.StatusCode, string(body))
		tc.ociManifestBody = nil
		// Don't fail the step - let validation steps check if this is expected
		tc.logDebug("OCI manifest fetch returned non-200 status: %d\n", resp.StatusCode)
		return nil
	}

	tc.ociManifestBody = body
	tc.ociManifestError = nil
	tc.logDebug("OCI manifest fetched successfully (%d bytes)\n", len(body))

	// Parse manifest to extract blob digest and store for annotation verification
	var manifestData map[string]interface{}
	if err := json.Unmarshal(body, &manifestData); err != nil {
		tc.ociManifestError = err
		return tc.logError(fmt.Errorf("failed to parse OCI manifest JSON: %w", err))
	}
	tc.ociManifestData = manifestData // Store for later annotation assertions

	// Validate layers array exists and is non-empty
	layers, err := jsonpath.Get("$.layers", manifestData)
	if err != nil {
		tc.ociManifestError = err
		return tc.logError(fmt.Errorf("manifest has no layers array: %w", err))
	}
	layersArray, ok := layers.([]interface{})
	if !ok || len(layersArray) == 0 {
		tc.ociManifestError = fmt.Errorf("manifest layers array is empty or invalid")
		return tc.logError(tc.ociManifestError)
	}

	tc.logDebug("OCI manifest has %d layer(s)\n", len(layersArray))
	for i, layer := range layersArray {
		if layerMap, ok := layer.(map[string]interface{}); ok {
			digest := layerMap["digest"]
			mediaType := layerMap["mediaType"]
			tc.logDebug("  Layer %d: digest=%v, mediaType=%v\n", i, digest, mediaType)
		}
	}

	// Extract digest from $.layers[0].digest
	// NOTE: eval-hub OCI exports always place the results tar+gzip in the first layer.
	// The blob extraction logic (iFetchOCIBlobByDigest) then searches all files within
	// the tar for evaluation-card.json or falls back to the first .json file found.
	digest, err := jsonpath.Get("$.layers[0].digest", manifestData)
	if err != nil {
		tc.ociManifestError = err
		return tc.logError(fmt.Errorf("failed to extract digest from manifest: %w", err))
	}

	digestStr, ok := digest.(string)
	if !ok {
		tc.ociManifestError = fmt.Errorf("digest is not a string: %T", digest)
		return tc.logError(tc.ociManifestError)
	}

	tc.logDebug("Extracted blob digest: %s\n", digestStr)

	// Now fetch the blob
	return tc.iFetchOCIBlobByDigest(repositoryResolved, digestStr)
}

// iFetchOCIBlobByDigest fetches the OCI blob content by digest
func (tc *scenarioConfig) iFetchOCIBlobByDigest(repository, digest string) error {
	baseURL := ociBaseURL()
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", baseURL, repository, digest)

	req, err := http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to create blob request: %w", err))
	}

	resp, err := tc.getOCIHTTPClient().Do(req)
	if err != nil {
		tc.ociArtifactError = err
		return tc.logError(fmt.Errorf("failed to fetch OCI blob: %w", err))
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			tc.logDebug("Failed to close OCI blob response body: %v\n", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tc.ociArtifactError = err
		return tc.logError(fmt.Errorf("failed to read OCI blob response: %w", err))
	}

	if resp.StatusCode != http.StatusOK {
		tc.ociArtifactError = fmt.Errorf("OCI blob fetch returned status %d: %s", resp.StatusCode, string(body))
		return tc.logError(tc.ociArtifactError)
	}

	tc.logDebug("OCI blob fetched successfully (%d bytes)\n", len(body))

	// The blob might be gzipped and/or tarred - need to decompress and extract
	var finalContent []byte
	var decompressed []byte

	// Step 1: Check if gzipped (magic bytes 0x1f, 0x8b)
	if len(body) > 2 && body[0] == 0x1f && body[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			tc.ociArtifactError = err
			return tc.logError(fmt.Errorf("failed to create gzip reader: %w", err))
		}
		defer func() {
			if closeErr := reader.Close(); closeErr != nil {
				tc.logDebug("Failed to close gzip reader: %v\n", closeErr)
			}
		}()

		decompressed, err = io.ReadAll(reader)
		if err != nil {
			tc.ociArtifactError = err
			return tc.logError(fmt.Errorf("failed to decompress gzip blob: %w", err))
		}
	} else {
		decompressed = body
	}

	// Step 2: Extract from tar archive
	// NOTE: OCI blob may contain multiple .json files:
	//   - results_{jobID}.json (from adapter)
	//   - evaluation-card.json (from EH service - what we want for EvalCard tests)
	// We look for evaluation-card.json FIRST, fall back to any .json if not found
	tarReader := tar.NewReader(bytes.NewReader(decompressed))
	var allFiles []string
	var firstJsonContent []byte
	var firstJsonName string
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of tar archive
		}
		if err != nil {
			tc.ociArtifactError = err
			return tc.logError(fmt.Errorf("failed to read tar archive: %w", err))
		}

		if header.Typeflag == tar.TypeReg {
			allFiles = append(allFiles, header.Name)
			if strings.HasSuffix(header.Name, ".json") {
				content, err := io.ReadAll(tarReader)
				if err != nil {
					tc.ociArtifactError = err
					return tc.logError(fmt.Errorf("failed to read JSON file from tar: %w", err))
				}

				// Prefer evaluation-card.json if it exists
				if header.Name == "evaluation-card.json" {
					finalContent = content
					tc.logDebug("Found evaluation-card.json in OCI blob (%d bytes)\n", len(content))
					break // Found the EvalCard, stop searching
				}

				// Keep track of first .json as fallback
				if firstJsonContent == nil {
					firstJsonContent = content
					firstJsonName = header.Name
				}
			}
		}
	}

	// Use evaluation-card.json if found, otherwise fall back to first .json
	if finalContent == nil {
		finalContent = firstJsonContent
		if finalContent != nil {
			tc.logDebug("No evaluation-card.json found, using first JSON file: %s (%d bytes)\n", firstJsonName, len(finalContent))
		}
	}

	tc.logDebug("All files in OCI blob tar: %v\n", allFiles)

	if len(finalContent) == 0 {
		tc.ociArtifactError = fmt.Errorf("no JSON file found in OCI blob tar archive. Files found: %v", allFiles)
		return tc.logError(tc.ociArtifactError)
	}

	tc.ociArtifactBody = finalContent
	tc.ociArtifactError = nil

	return nil
}

// OCI manifest validation functions

func (tc *scenarioConfig) theOCIManifestShouldNotExist() error {
	// Check if manifest fetch returned 404
	if tc.ociManifestStatusCode == http.StatusNotFound {
		tc.logDebug("OCI manifest does not exist (as expected)\n")
		return nil
	}

	// If there was a network error (not HTTP), report it as infrastructure problem
	if tc.ociManifestError != nil {
		return tc.logError(fmt.Errorf("manifest fetch failed with unexpected error (not a 404): %w", tc.ociManifestError))
	}

	// Manifest exists but should not
	return tc.logError(fmt.Errorf("OCI manifest exists (status %d) but should not", tc.ociManifestStatusCode))
}

// OCI artifact validation functions

func (tc *scenarioConfig) theOCIArtifactShouldExist() error {
	if tc.ociArtifactError != nil {
		return tc.logError(fmt.Errorf("OCI artifact fetch failed: %w", tc.ociArtifactError))
	}
	if len(tc.ociArtifactBody) == 0 {
		return tc.logError(fmt.Errorf("OCI artifact body is empty"))
	}
	tc.logDebug("OCI artifact exists (%d bytes)\n", len(tc.ociArtifactBody))
	return nil
}

func (tc *scenarioConfig) theOCIArtifactShouldContain(expectedContent string) error {
	if tc.ociArtifactError != nil {
		return tc.logError(fmt.Errorf("OCI artifact fetch failed: %w", tc.ociArtifactError))
	}
	if !strings.Contains(string(tc.ociArtifactBody), expectedContent) {
		return tc.logError(fmt.Errorf("OCI artifact does not contain %q", expectedContent))
	}
	tc.logDebug("OCI artifact contains %q\n", expectedContent)
	return nil
}

func (tc *scenarioConfig) theOCIArtifactShouldContainValueAtPath(expectedValue, jsonPath string) error {
	if tc.ociArtifactError != nil {
		return tc.logError(fmt.Errorf("OCI artifact fetch failed: %w", tc.ociArtifactError))
	}

	// Resolve expected value (support {{value:...}} pattern)
	resolvedValue, err := tc.getValue(expectedValue)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to resolve expected value %q: %w", expectedValue, err))
	}

	var artifactData map[string]interface{}
	if err := json.Unmarshal(tc.ociArtifactBody, &artifactData); err != nil {
		return tc.logError(fmt.Errorf("failed to parse OCI artifact JSON: %w", err))
	}

	actualValue, err := jsonpath.Get(jsonPath, artifactData)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get value at path %q: %w", jsonPath, err))
	}

	actualStr := fmt.Sprintf("%v", actualValue)
	if actualStr != resolvedValue {
		return tc.logError(fmt.Errorf("OCI artifact value at path %q is %q, expected %q", jsonPath, actualStr, resolvedValue))
	}

	tc.logDebug("OCI artifact contains value %q at path %q\n", resolvedValue, jsonPath)
	return nil
}

func (tc *scenarioConfig) theOCIArtifactShouldBeValidJSON() error {
	if tc.ociArtifactError != nil {
		return tc.logError(fmt.Errorf("OCI artifact fetch failed: %w", tc.ociArtifactError))
	}

	var artifactData map[string]interface{}
	if err := json.Unmarshal(tc.ociArtifactBody, &artifactData); err != nil {
		return tc.logError(fmt.Errorf("OCI artifact is not valid JSON: %w", err))
	}

	tc.logDebug("OCI artifact is valid JSON\n")
	return nil
}

// theOCIManifestShouldContainAnnotation verifies a specific annotation exists in the OCI manifest
func (tc *scenarioConfig) theOCIManifestShouldContainAnnotation(key, expectedValue string) error {
	if tc.ociManifestError != nil {
		return tc.logError(fmt.Errorf("OCI manifest fetch failed: %w", tc.ociManifestError))
	}
	if tc.ociManifestData == nil {
		return tc.logError(fmt.Errorf("OCI manifest data not available"))
	}

	// Resolve expected value from saved values if needed
	expectedResolved, err := tc.getValue(expectedValue)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to resolve expected value %q: %w", expectedValue, err))
	}

	// Extract annotations from manifest
	annotations, err := jsonpath.Get("$.annotations", tc.ociManifestData)
	if err != nil {
		return tc.logError(fmt.Errorf("OCI manifest has no annotations field: %w", err))
	}

	annotationsMap, ok := annotations.(map[string]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("OCI manifest annotations is not a map: %T", annotations))
	}

	// Check if annotation key exists and matches expected value
	actualValue, exists := annotationsMap[key]
	if !exists {
		return tc.logError(fmt.Errorf("OCI manifest annotation %q not found. Available annotations: %v", key, annotationsMap))
	}

	actualStr, ok := actualValue.(string)
	if !ok {
		return tc.logError(fmt.Errorf("OCI manifest annotation %q is not a string: %T", key, actualValue))
	}

	if actualStr != expectedResolved {
		return tc.logError(fmt.Errorf("OCI manifest annotation %q = %q, expected %q", key, actualStr, expectedResolved))
	}

	tc.logDebug("OCI manifest annotation %q = %q (verified)\n", key, actualStr)
	return nil
}
