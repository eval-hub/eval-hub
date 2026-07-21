package handlers

import (
	"net/url"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func FuzzDecodeParam(f *testing.F) {
	f.Add("Test%20Provider")
	f.Add("%20")
	f.Add("%2F")
	f.Add("%2F%20%2F")
	f.Add("plain")
	f.Add("%")
	f.Add("%zz")
	f.Add("")

	f.Fuzz(func(t *testing.T, v string) {
		got := DecodeParam(v)
		decoded, err := url.QueryUnescape(v)
		if err != nil {
			if got != v {
				t.Fatalf("DecodeParam(%q) = %q, want original on unescape error", v, got)
			}
			return
		}
		if got != decoded {
			t.Fatalf("DecodeParam(%q) = %q, want %q", v, got, decoded)
		}
	})
}

func FuzzIsAllowedPatch(f *testing.F) {
	allowed := []allowedPatch{
		{Path: "/status", Op: api.PatchOpReplace},
		{Path: "/metadata", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/metadata", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/spec/name", Op: api.PatchOpReplace},
	}

	f.Add(string(api.PatchOpReplace), "/status")
	f.Add(string(api.PatchOpAdd), "/metadata/labels")
	f.Add(string(api.PatchOpRemove), "/metadata/annotations/x")
	f.Add(string(api.PatchOpReplace), "/spec/name")
	f.Add(string(api.PatchOpReplace), "/forbidden")
	f.Add("bogus", "/status")
	f.Add(string(api.PatchOpAdd), "/metadata")
	f.Add(string(api.PatchOpAdd), "/metadatax")

	f.Fuzz(func(t *testing.T, op, path string) {
		_ = isAllowedPatch(allowed, api.PatchOp(op), path)
	})
}
