package api

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

// MergeOCIResults updates workflow status and, when supplied, the card reference.
// It initializes missing results and preserves all other fields. A nil card means
// no change to the existing reference; this function does not reset OCI results.
func MergeOCIResults(results *EvaluationJobResults, status *OCIProcessingStatus, card *OCIArtifactReference) *EvaluationJobResults {
	if results == nil {
		results = &EvaluationJobResults{}
	}
	if results.OCI == nil {
		results.OCI = &EvaluationOCIResults{}
	}
	results.OCI.Status = status
	if card != nil {
		results.OCI.EvaluationCard = card
	}
	return results
}
