package abstractions_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
)

func TestQueryFilter_ExtractQueryParams(t *testing.T) {
	f := &abstractions.QueryFilter{
		Limit:  10,
		Offset: 5,
		Params: map[string]any{
			"a": "keep",
			"b": "",
			"c": "also",
		},
	}
	out := f.ExtractQueryParams()
	if out.Limit != 10 || out.Offset != 5 {
		t.Fatalf("ExtractQueryParams() limit/offset = %d,%d, want 10,5", out.Limit, out.Offset)
	}
	if _, ok := out.Params["b"]; ok {
		t.Error("empty string param should be deleted")
	}
	if out.Params["a"] != "keep" || out.Params["c"] != "also" {
		t.Errorf("params = %v", out.Params)
	}
}

func TestQueryFilter_HasParams(t *testing.T) {
	f := &abstractions.QueryFilter{
		Params: map[string]any{
			"x": "1",
			"y": "",
		},
	}
	if f.HasParams("x", "y") {
		t.Error("HasParams should be false when y is empty after extract")
	}
	f2 := &abstractions.QueryFilter{Params: map[string]any{"a": "v", "b": "w"}}
	if !f2.HasParams("a", "b") {
		t.Error("HasParams should be true when both present")
	}
	if f2.HasParams("a", "missing") {
		t.Error("HasParams should be false when a param is absent")
	}
}

func TestQueryFilter_String(t *testing.T) {
	f := &abstractions.QueryFilter{Limit: 3, Offset: 1, Params: map[string]any{"k": "v"}}
	s := f.String()
	want := `{"limit":3,"offset":1,"params":map[k:v]}`
	if s != want {
		t.Errorf("String() = %q, want %q", s, want)
	}
}
