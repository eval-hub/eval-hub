package api

import (
	"strings"
	"testing"
)

func TestParseLocalSigningDirectory(t *testing.T) {
	for _, tt := range []struct{ ref, path string }{
		{"file:///absolute/keys", "/absolute/keys"},
		{"file:///keys%20with%20spaces", "/keys with spaces"},
		{"file:///keys%252Fencoded", "/keys%2Fencoded"},
		{"file:///keys%23%3F", "/keys#?"},
		{"file:///keys/../other", "/keys/../other"},
		{"file:///", "/"},
	} {
		t.Run(tt.ref, func(t *testing.T) {
			got, err := ParseLocalSigningDirectory(tt.ref)
			if err != nil || got != tt.path {
				t.Fatalf("path = %q, error = %v; want %q", got, err, tt.path)
			}
		})
	}
	for _, ref := range []string{
		"", "keys", "/absolute/keys", "file:keys", "file:/keys", "file://keys", "file://localhost/keys",
		"file://user@/keys", "file:///keys?query", "file:///keys?", "file:///keys#fragment", "file:///keys#",
		"file:///keys%zz", "file:///keys%00", "https:///keys", "FILE:///keys", " file:///keys", "file:///keys\n",
	} {
		t.Run("reject/"+ref, func(t *testing.T) {
			if path, err := ParseLocalSigningDirectory(ref); err == nil || path != "" {
				t.Fatalf("accepted invalid URI: path=%q, error=%v", path, err)
			}
		})
	}
}

func TestValidateOCISigningReference(t *testing.T) {
	for _, ref := range []string{"key", "cosign-key-1", strings.Repeat("a", 63), "file:///keys"} {
		if err := ValidateOCISigningReference(ref); err != nil {
			t.Fatalf("rejected valid reference %q: %v", ref, err)
		}
	}
	for _, ref := range []string{"", "  ", "Key", "key_name", "tenant/key", "-key", "key-", strings.Repeat("a", 64)} {
		if err := ValidateOCISigningReference(ref); err == nil {
			t.Fatalf("accepted invalid reference %q", ref)
		}
	}
}
