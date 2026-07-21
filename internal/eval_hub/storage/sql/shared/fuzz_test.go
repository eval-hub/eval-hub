package shared

import (
	"strings"
	"testing"
)

func FuzzGetValues(f *testing.F) {
	f.Add("tags", "a,b,c")
	f.Add("tags", "a|b|c")
	f.Add("name", "single")
	f.Add("tags", "a,b|c")
	f.Add("tags", "")
	f.Add("tags", ",,")
	f.Add("tags", "||")

	f.Fuzz(func(t *testing.T, key, values string) {
		parts, op := GetValues(key, values)
		if op != "AND" && op != "OR" {
			t.Fatalf("unexpected operator %q", op)
		}
		if len(parts) == 0 {
			t.Fatal("GetValues returned no parts")
		}
		switch {
		case strings.Contains(values, ","):
			if op != "AND" {
				t.Fatalf("comma-delimited values should use AND, got %q", op)
			}
			if len(parts) != len(strings.Split(values, ",")) {
				t.Fatalf("AND parts len = %d, want %d", len(parts), len(strings.Split(values, ",")))
			}
		case strings.Contains(values, "|"):
			if op != "OR" {
				t.Fatalf("pipe-delimited values should use OR, got %q", op)
			}
			if len(parts) != len(strings.Split(values, "|")) {
				t.Fatalf("OR parts len = %d, want %d", len(parts), len(strings.Split(values, "|")))
			}
		default:
			if op != "AND" {
				t.Fatalf("single value should use AND, got %q", op)
			}
			if len(parts) != 1 {
				t.Fatalf("single value parts len = %d, want 1", len(parts))
			}
		}
	})
}

func FuzzValidateFilter(f *testing.F) {
	allowed := []string{"name", "tags", "owner", "status"}

	f.Add("name")
	f.Add("tags")
	f.Add("unknown")
	f.Add("")
	f.Add("name,tags")

	f.Fuzz(func(t *testing.T, raw string) {
		var filter []string
		if raw != "" {
			filter = strings.Split(raw, ",")
		}
		err := ValidateFilter(filter, allowed)
		for _, key := range filter {
			allowedKey := false
			for _, col := range allowed {
				if key == col {
					allowedKey = true
					break
				}
			}
			if !allowedKey {
				if err == nil {
					t.Fatalf("expected error for disallowed key %q", key)
				}
				return
			}
		}
		if err != nil {
			t.Fatalf("unexpected error for allowed filter %v: %v", filter, err)
		}
	})
}
