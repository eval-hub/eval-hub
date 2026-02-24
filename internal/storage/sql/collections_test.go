package sql

import (
	"encoding/json"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestApplyPatches(t *testing.T) {
	t.Run("nil patches returns document unchanged", func(t *testing.T) {
		doc := `{"name":"x"}`
		got, err := applyPatches(doc, nil)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		if string(got) != doc {
			t.Errorf("expected document unchanged, got %q", got)
		}
	})

	t.Run("empty patches returns document unchanged", func(t *testing.T) {
		doc := `{"name":"only"}`
		patches := &api.Patch{}
		got, err := applyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		if string(got) != doc {
			t.Errorf("expected document unchanged, got %q", got)
		}
	})

	t.Run("single replace patch applies and returns patched JSON", func(t *testing.T) {
		doc := `{"name":"original","description":"desc","benchmarks":[]}`
		patches := &api.Patch{
			{Op: api.PatchOpReplace, Path: "/name", Value: "patched-name"},
		}
		got, err := applyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		if name, _ := m["name"].(string); name != "patched-name" {
			t.Errorf("expected name %q, got %q", "patched-name", name)
		}
		if desc, _ := m["description"].(string); desc != "desc" {
			t.Errorf("expected description unchanged %q, got %q", "desc", desc)
		}
	})

	t.Run("multiple patches apply and return patched JSON", func(t *testing.T) {
		doc := `{"name":"a","description":"b"}`
		patches := &api.Patch{
			{Op: api.PatchOpReplace, Path: "/name", Value: "x"},
			{Op: api.PatchOpReplace, Path: "/description", Value: "y"},
		}
		got, err := applyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		if name, _ := m["name"].(string); name != "x" {
			t.Errorf("expected name %q, got %q", "x", name)
		}
		if desc, _ := m["description"].(string); desc != "y" {
			t.Errorf("expected description %q, got %q", "y", desc)
		}
	})

	t.Run("replace nested path applies correctly", func(t *testing.T) {
		doc := `{"benchmarks":[{"id":"a","provider_id":"p1"}]}`
		patches := &api.Patch{
			{Op: api.PatchOpReplace, Path: "/benchmarks/0/id", Value: "new-id"},
		}
		got, err := applyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		benchmarks, _ := m["benchmarks"].([]any)
		if len(benchmarks) != 1 {
			t.Fatalf("expected 1 benchmark, got %d", len(benchmarks))
		}
		first, _ := benchmarks[0].(map[string]any)
		if id, _ := first["id"].(string); id != "new-id" {
			t.Errorf("expected id %q, got %q", "new-id", id)
		}
	})
}
