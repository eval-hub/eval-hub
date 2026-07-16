package sql

import "sync"

var (
	evaluationJobUpdateTestHookMu      sync.RWMutex
	evaluationJobUpdateAfterLockedRead func(jobID, benchmarkID string)
)

func setEvaluationJobUpdateAfterLockedReadHook(fn func(jobID, benchmarkID string)) {
	evaluationJobUpdateTestHookMu.Lock()
	defer evaluationJobUpdateTestHookMu.Unlock()
	evaluationJobUpdateAfterLockedRead = fn
}

func invokeEvaluationJobUpdateAfterLockedReadHook(jobID, benchmarkID string) {
	evaluationJobUpdateTestHookMu.RLock()
	fn := evaluationJobUpdateAfterLockedRead
	evaluationJobUpdateTestHookMu.RUnlock()
	if fn != nil {
		fn(jobID, benchmarkID)
	}
}
