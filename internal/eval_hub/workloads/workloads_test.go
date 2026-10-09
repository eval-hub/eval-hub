package workloads

import (
	"context"
	"testing"
)

func TestTypeFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want Type
	}{
		{name: "nil context"},
		{name: "context without workload type", ctx: context.Background()},
		{
			name: "context value with unexpected type",
			ctx:  context.WithValue(context.Background(), workloadTypeContextKey{}, string(PostProcessing)),
		},
		{
			name: "workload type",
			ctx:  WithType(context.Background(), PostProcessing),
			want: PostProcessing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := TypeFromContext(test.ctx); got != test.want {
				t.Fatalf("TypeFromContext() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWithTypePreservesExistingContextValues(t *testing.T) {
	type valueContextKey struct{}

	base := context.WithValue(context.Background(), valueContextKey{}, "preserved")
	got := WithType(base, Evaluation)
	if value := got.Value(valueContextKey{}); value != "preserved" {
		t.Fatalf("WithType() lost existing context value: got %v", value)
	}
	if workloadType := TypeFromContext(got); workloadType != Evaluation {
		t.Fatalf("TypeFromContext() = %q, want %q", workloadType, Evaluation)
	}
}
