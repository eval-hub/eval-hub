package features

import (
	"fmt"
	"strings"
	"testing"
	"time"

	pkgapi "github.com/eval-hub/eval-hub/pkg/api"
)

func TestWaitForOCIExportStatus(t *testing.T) {
	tests := []struct {
		name      string
		states    []pkgapi.OCIProcessingState
		fetchErr  error
		wantError string
		wantCalls int
	}{
		{
			name:      "retries running until completed",
			states:    []pkgapi.OCIProcessingState{pkgapi.OCIProcessingRunning, pkgapi.OCIProcessingRunning, pkgapi.OCIProcessingCompleted},
			wantCalls: 3,
		},
		{
			name:      "fails immediately on another terminal state",
			states:    []pkgapi.OCIProcessingState{pkgapi.OCIProcessingFailed},
			wantError: `OCI export reached terminal state "failed" (expected "completed")`,
			wantCalls: 1,
		},
		{
			name:      "timeout retains last fetch error",
			fetchErr:  fmt.Errorf("job fetch unavailable"),
			wantError: `last status: "", last error: job fetch unavailable`,
		},
		{
			name:      "timeout retains last status",
			states:    []pkgapi.OCIProcessingState{pkgapi.OCIProcessingRunning},
			wantError: `last status: "running", last error: <nil>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			fetchJob := func() (*pkgapi.EvaluationJobResource, error) {
				calls++
				if tt.fetchErr != nil {
					return nil, tt.fetchErr
				}
				index := min(calls-1, len(tt.states)-1)
				return &pkgapi.EvaluationJobResource{
					Results: &pkgapi.EvaluationJobResults{
						OCI: &pkgapi.EvaluationOCIResults{
							Status: &pkgapi.OCIProcessingStatus{State: tt.states[index]},
						},
					},
				}, nil
			}
			err := waitForOCIExportStatus(fetchJob, pkgapi.OCIProcessingCompleted, time.Second, time.Millisecond)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
			if tt.wantCalls > 0 && calls != tt.wantCalls {
				t.Errorf("fetch calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantCalls == 0 && (calls < 2 || !strings.Contains(err.Error(), "timed out after")) {
				t.Errorf("expected retries followed by timeout; calls = %d, error = %v", calls, err)
			}
		})
	}
}
