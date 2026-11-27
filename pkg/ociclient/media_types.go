package ociclient

const (
	// MediaTypeEvaluationCardLayer is the OCI layer media type for eval-hub evaluation cards.
	MediaTypeEvaluationCardLayer = "application/vnd.eval-hub.evaluation-card.v1+json"
	// MediaTypeArtifactConfig is the OCI artifact config media type.
	MediaTypeArtifactConfig = "application/vnd.oci.artifact.config.v1+json"
	// MediaTypeImageManifest is the OCI image manifest media type used for artifact publishing.
	MediaTypeImageManifest = "application/vnd.oci.image.manifest.v1+json"
	// MediaTypeImageIndex is the OCI index media type used for evaluation bundles.
	MediaTypeImageIndex = "application/vnd.oci.image.index.v1+json"
	// MediaTypeDockerManifest is the Docker schema-2 single-image manifest format.
	MediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
)
