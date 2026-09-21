package sql

import "sync"

var (
	evaluationJobUpdateTestHookMu      sync.RWMutex
	evaluationJobUpdateAfterLockedRead func(jobID, benchmarkID string)
	collectionPatchTestHookMu          sync.RWMutex
	collectionPatchAfterLockedRead     func(collectionID string)
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

func setCollectionPatchAfterLockedReadHook(fn func(collectionID string)) {
	collectionPatchTestHookMu.Lock()
	defer collectionPatchTestHookMu.Unlock()
	collectionPatchAfterLockedRead = fn
}

func invokeCollectionPatchAfterLockedReadHook(collectionID string) {
	collectionPatchTestHookMu.RLock()
	fn := collectionPatchAfterLockedRead
	collectionPatchTestHookMu.RUnlock()
	if fn != nil {
		fn(collectionID)
	}
}
