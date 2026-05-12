package sql

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestMessageCodeForOverallState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state api.OverallState
		want  string
	}{
		{api.OverallStateFailed, constants.MESSAGE_CODE_EVALUATION_JOB_FAILED},
		{api.OverallStatePartiallyFailed, constants.MESSAGE_CODE_EVALUATION_JOB_PARTIALLY_FAILED},
		{api.OverallStateCompleted, constants.MESSAGE_CODE_EVALUATION_JOB_COMPLETED},
		{api.OverallStateCancelled, constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED},
		{api.OverallStatePending, constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED},
		{api.OverallStateRunning, constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.state), func(t *testing.T) {
			t.Parallel()
			if got := messageCodeForOverallState(tc.state); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
