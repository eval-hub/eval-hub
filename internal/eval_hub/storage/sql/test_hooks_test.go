package sql

import "testing"

func TestCollectionPatchReadHooks(t *testing.T) {
	t.Cleanup(func() {
		setCollectionPatchBeforeLockedReadHook(nil)
		setCollectionPatchAfterLockedReadHook(nil)
	})

	var beforeID, afterID string
	setCollectionPatchBeforeLockedReadHook(func(id string) {
		beforeID = id
	})
	setCollectionPatchAfterLockedReadHook(func(id string) {
		afterID = id
	})

	invokeCollectionPatchBeforeLockedReadHook("before")
	invokeCollectionPatchAfterLockedReadHook("after")
	if beforeID != "before" {
		t.Errorf("before hook ID = %q, want %q", beforeID, "before")
	}
	if afterID != "after" {
		t.Errorf("after hook ID = %q, want %q", afterID, "after")
	}

	setCollectionPatchBeforeLockedReadHook(nil)
	setCollectionPatchAfterLockedReadHook(nil)
	invokeCollectionPatchBeforeLockedReadHook("ignored")
	invokeCollectionPatchAfterLockedReadHook("ignored")
}
