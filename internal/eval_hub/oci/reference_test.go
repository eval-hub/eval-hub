package oci

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestHasPublishedReference(t *testing.T) {
	t.Parallel()

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reference := "registry.example/repo:tag@" + digest
	for _, tc := range []struct {
		name string
		ref  *api.OCIArtifactReference
		want bool
	}{
		{name: "nil"},
		{name: "empty", ref: &api.OCIArtifactReference{}},
		{name: "missing digest", ref: &api.OCIArtifactReference{OCIReference: reference}},
		{name: "missing reference", ref: &api.OCIArtifactReference{OCIDigest: digest}},
		{name: "complete", ref: &api.OCIArtifactReference{OCIDigest: digest, OCIReference: reference}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasPublishedReference(tc.ref); got != tc.want {
				t.Fatalf("HasPublishedReference(%+v) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}
