package sql

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestOCIArtifactReferencesEquivalent(t *testing.T) {
	artifact := &api.OCIArtifactReference{
		OCIDigest:    "sha256:abc",
		OCIReference: "registry.example/team/card@sha256:abc",
	}
	for _, tc := range []struct {
		name string
		a, b *api.OCIArtifactReference
		want bool
	}{
		{name: "both nil", want: true},
		{name: "nil first reference", b: artifact},
		{name: "nil second reference", a: artifact},
		{
			name: "identical fields in separate references",
			a:    artifact,
			b:    &api.OCIArtifactReference{OCIDigest: artifact.OCIDigest, OCIReference: artifact.OCIReference},
			want: true,
		},
		{
			name: "different digest",
			a:    artifact,
			b:    &api.OCIArtifactReference{OCIDigest: "sha256:def", OCIReference: artifact.OCIReference},
		},
		{
			name: "different reference",
			a:    artifact,
			b:    &api.OCIArtifactReference{OCIDigest: artifact.OCIDigest, OCIReference: "registry.example/other/card@sha256:abc"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ociArtifactReferencesEquivalent(tc.a, tc.b); got != tc.want {
				t.Fatalf("ociArtifactReferencesEquivalent(%+v, %+v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
