package handlers_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/handlers"
)

func TestDecodeParam(t *testing.T) {
	t.Run("DecodeParam should decode the parameter", func(t *testing.T) {
		tests := [][]string{
			{"Test%20Provider", "Test Provider"},
			{"%20", " "},
			{"%2F", "/"},
			{"%2F%20", "/ "},
			{"%2F%20%2F", "/ /"},
			{"%2F%20%2F%20", "/ / "},
			{"%2F%20%2F%20%2F", "/ / /"},
			{"%2F%20%2F%20%2F%20", "/ / / "},
		}
		for _, test := range tests {
			got := handlers.DecodeParam(test[0])
			if got != test[1] {
				t.Errorf("DecodeParam(%q) = %q, want %q", test, got, test[1])
			}
		}
	})
}
