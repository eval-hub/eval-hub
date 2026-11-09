package api

// IsOCIExportEnabled reports whether a job has OCI exports configured.
func IsOCIExportEnabled(job *EvaluationJobResource) bool {
	return job != nil && job.Exports != nil && job.Exports.OCI != nil
}

// OCISigningConfig selects the signing method and optionally references a key Secret.
// It retains configuration only; it does not resolve keys or schedule signing.
type OCISigningConfig struct {
	Type      string  `json:"type" validate:"required,oneof=key-pair"`
	SecretRef *string `json:"secret_ref,omitempty" validate:"omitempty,rfc1123_dns_label"`
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
func UpdateOCIResults(results *EvaluationJobResults, status *OCIProcessingStatus, card *OCIArtifactReference) *EvaluationJobResults {
	if status == nil && card == nil {
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
	return results
}
