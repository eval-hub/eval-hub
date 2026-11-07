package api

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// IsOCIExportEnabled reports whether a job has OCI exports configured.
func IsOCIExportEnabled(job *EvaluationJobResource) bool {
	return job != nil && job.Exports != nil && job.Exports.OCI != nil
}

var ociExportPrefixPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

// ValidateOCIExportPrefix checks the complete export prefix before any truncation.
// Empty prefixes are allowed. Length is limited only when constructing the final tag.
func ValidateOCIExportPrefix(prefix string) error {
	if prefix != "" && !ociExportPrefixPattern.MatchString(prefix) {
		return fmt.Errorf("OCI export prefix must match %s", ociExportPrefixPattern.String())
	}
	return nil
}

// OCISigningConfig selects the signing method and references its key material.
// It retains configuration only; it does not resolve keys or schedule signing.
type OCISigningConfig struct {
	Type      string  `json:"type" validate:"required,oneof=key-pair"`
	SecretRef *string `json:"secret_ref,omitempty" validate:"required_if=Type key-pair,omitempty,oci_signing_ref"`
}

var ociSigningSecretNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// ValidateOCISigningReference accepts a Kubernetes Secret name or a local directory URI.
// Execution-mode restrictions are applied by the service at job creation.
func ValidateOCISigningReference(ref string) error {
	if ociSigningSecretNamePattern.MatchString(ref) {
		return nil
	}
	_, err := ParseLocalSigningDirectory(ref)
	return err
}

// ParseLocalSigningDirectory validates a local file URI without accessing the filesystem.
// URL.Path is already decoded by url.Parse and must not be unescaped again.
func ParseLocalSigningDirectory(ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil || !strings.HasPrefix(ref, "file:///") || u.Scheme != "file" ||
		u.Host != "" || u.User != nil || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(ref, "#") || strings.ContainsRune(u.Path, '\x00') {
		return "", fmt.Errorf("signing reference must be a Kubernetes Secret name or an absolute file:/// directory URI without authority, query, or fragment")
	}
	return u.Path, nil
}

// OCIArtifactReference identifies an OCI artifact by digest.
type OCIArtifactReference struct {
	OCIDigest    string `json:"oci_digest" validate:"required,sha256_digest"`
	OCIReference string `json:"oci_reference" validate:"required,notblank"`
}

// OCIProcessingState describes the common post-evaluation OCI workflow,
// independently of the evaluation job state.
type OCIProcessingState string

const (
	OCIProcessingRunning   OCIProcessingState = "running"
	OCIProcessingFailed    OCIProcessingState = "failed"
	OCIProcessingCompleted OCIProcessingState = "completed"
)

// IsOCIProcessingTerminalState reports whether an OCI workflow state is a terminal outcome.
func IsOCIProcessingTerminalState(state OCIProcessingState) bool {
	return state == OCIProcessingFailed || state == OCIProcessingCompleted
}

// OCIProcessingStatus reports actual card, bundle, and signing workflow information.
// It has no default state or message.
// Workflow reporting uses running followed by completed or failed.
type OCIProcessingStatus struct {
	State   OCIProcessingState `json:"state" validate:"required,oneof=running failed completed"`
	Message *MessageInfo       `json:"message" validate:"required"`
}

// OCISigningResult reports the method used to sign the evaluation bundle.
type OCISigningResult struct {
	Type string `json:"type" validate:"required,oneof=key-pair"`
}

// EvaluationOCIResults remains unset until artifact and signing workflows supply values.
type EvaluationOCIResults struct {
	Status           *OCIProcessingStatus  `json:"status,omitempty"`
	EvaluationCard   *OCIArtifactReference `json:"evaluation_card,omitempty"`
	EvaluationBundle *OCIArtifactReference `json:"evaluation_bundle,omitempty"`
	Signing          *OCISigningResult     `json:"signing,omitempty"`
}

// EligibleForOCIProcessing reports whether a job has OCI exports configured and
// a completed or partially failed outcome.
func EligibleForOCIProcessing(job *EvaluationJobResource) bool {
	return IsResultsExportEligible(job) && IsOCIExportEnabled(job)
}

// UpdateOCIResults partially merges OCI workflow status and the card reference in memory.
// It performs no field validation or workflow enforcement. Storage updates have a
// stricter contract: UpdateEvaluationJobOCI requires a non-nil, validated status
// and validation of any supplied card, even though this helper permits card-only updates.
// A non-nil card intentionally replaces the existing reference (last-write-wins),
// which supports future evaluation-card regeneration; the current workflow has no
// regeneration path yet. Replacement updates the stored digest and reference,
// not the previously published immutable artifact.
// It initializes missing results and preserves all other
// fields. A nil status or card means no change to the existing value; if both are
// nil, results are returned unchanged.
func UpdateOCIResults(results *EvaluationJobResults, status *OCIProcessingStatus, card, bundle *OCIArtifactReference) *EvaluationJobResults {
	if status == nil && card == nil && bundle == nil {
		return results
	}
	if results == nil {
		results = &EvaluationJobResults{}
	}
	if results.OCI == nil {
		results.OCI = &EvaluationOCIResults{}
	}
	if status != nil {
		results.OCI.Status = status
	}
	if card != nil {
		results.OCI.EvaluationCard = card
	}
	if bundle != nil {
		results.OCI.EvaluationBundle = bundle
	}
	return results
}
