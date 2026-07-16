package ociclient

const (
	// MediaTypeEvaluationCardLayer is the OCI layer media type for eval-hub evaluation cards.
	MediaTypeEvaluationCardLayer = "application/vnd.eval-hub.evaluation-card.v1+json"
	// MediaTypeArtifactConfig is the OCI artifact config media type.
	MediaTypeArtifactConfig = "application/vnd.oci.artifact.config.v1+json"
	// MediaTypeImageManifest is the OCI image manifest media type used for artifact publishing.
	MediaTypeImageManifest = "application/vnd.oci.image.manifest.v1+json"
)

type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Size        int64             `json:"size"`
	Digest      string            `json:"digest"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// manifest is the OCI image manifest envelope used to publish evaluation cards as artifacts.
type manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Config        descriptor        `json:"config"`
	Layers        []descriptor      `json:"layers"`
	Subject       *descriptor       `json:"subject,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}
