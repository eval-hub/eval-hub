package oci

import "github.com/eval-hub/eval-hub/pkg/api"

// HasPublishedReference reports whether a published artifact has a nonempty digest and reference.
// It does not validate digest or reference syntax.
func HasPublishedReference(ref *api.OCIArtifactReference) bool {
	return ref != nil && ref.OCIDigest != "" && ref.OCIReference != ""
}
