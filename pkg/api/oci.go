package api

// OCISigningConfig selects the signing method and optionally references a key Secret.
// It retains configuration only; it does not resolve keys or schedule signing.
type OCISigningConfig struct {
	Type      string  `json:"type" validate:"required,oneof=key-pair"`
	SecretRef *string `json:"secret_ref,omitempty" validate:"omitempty,notblank"`
}

// OCIArtifactReference identifies an OCI artifact by digest.
type OCIArtifactReference struct {
	OCIDigest    string `json:"oci_digest" validate:"required,notblank"`
	OCIReference string `json:"oci_reference" validate:"required,notblank"`
}

// OCIProcessingState describes the common post-evaluation OCI workflow,
// independently of the evaluation job state.
type OCIProcessingState string

const (
	OCIProcessingPending                    OCIProcessingState = "pending"
	OCIProcessingGeneratingEvaluationCard   OCIProcessingState = "generating_evaluation_card"
	OCIProcessingGeneratingEvaluationBundle OCIProcessingState = "generating_evaluation_bundle"
	OCIProcessingSigning                    OCIProcessingState = "signing"
	OCIProcessingFailed                     OCIProcessingState = "failed"
	OCIProcessingCompleted                  OCIProcessingState = "completed"
)

// OCIProcessingStatus reports actual card, bundle, and signing workflow information.
// It has no default state or message.
type OCIProcessingStatus struct {
	State   OCIProcessingState `json:"state" validate:"required,oneof=pending generating_evaluation_card generating_evaluation_bundle signing failed completed"`
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
