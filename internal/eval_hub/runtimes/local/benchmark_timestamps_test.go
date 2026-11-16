package local

import (
	"testing"
	"time"
)

func TestBenchmarkCreationRecordsSuccessfulProcessLaunch(t *testing.T) {
	for _, command := range []string{"true", ""} {
		t.Run(command, func(t *testing.T) {
			evaluation := sampleEvaluation("provider-1")
			cleanupDir(t, evaluation.Resource.ID)
			rt := &LocalRuntime{logger: discardLogger(), ctx: testContext(t), tracker: newTracker()}
			calls := 0
			before := time.Now()
			storage := &fakeStorage{providerConfigs: sampleLocalProviders("provider-1", command),
				creationFn: func(id string, index int, created time.Time) error {
					calls++
					if id != evaluation.Resource.ID || index != 0 || created.Before(before) || created.After(time.Now()) {
						t.Errorf("unexpected process creation metadata: %s / %d / %v", id, index, created)
					}
					return nil
				}}
			err := rt.runBenchmark(evaluation.Resource.ID, evaluation.Benchmarks[0], 0, evaluation, nil, storage)
			if command == "" {
				if err == nil || calls != 0 {
					t.Fatalf("failed launch recorded creation: calls=%d err=%v", calls, err)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("successful launch did not record creation: calls=%d err=%v", calls, err)
			}
		})
	}
}
