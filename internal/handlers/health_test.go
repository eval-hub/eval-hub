package handlers_test

import (
	"encoding/json"

	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/handlers"
)

func TestHandleHealth(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil)

	t.Run("GET request returns healthy status", func(t *testing.T) {
		r := createMockRequest("GET", "/health")
		w := httptest.NewRecorder()
		ctx := createExecutionContext()
		h.HandleHealth(ctx, r, &MockResponseWrapper{w})

		if w.Code != 200 {
			t.Errorf("Expected status code %d, got %d", 200, w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response["status"] != "healthy" {
			t.Errorf("Expected status 'healthy', got %v", response["status"])
		}

		if _, ok := response["timestamp"]; !ok {
			t.Error("Response missing timestamp field")
		}

		// Verify timestamp is valid RFC3339 format
		if timestamp, ok := response["timestamp"].(string); ok {
			if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
				t.Errorf("Invalid timestamp format: %v", err)
			}
		}
	})

}
