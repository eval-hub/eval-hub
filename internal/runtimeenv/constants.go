// Package runtimeenv holds constants shared between the eval-runtime-init binary
// and the eval-runtime-sidecar. These values form the runtime ABI between the two
// containers in an evaluation job pod.
package runtimeenv

const (
	// TestDataDir is the volume mount path where test data is staged.
	TestDataDir = "/test_data"

	// InitMetadataDir is the mount path of the emptyDir shared exclusively between
	// the init container and the sidecar. The adapter never sees this volume.
	// Future init container outputs (beyond .git-metadata) should be written here.
	InitMetadataDir = "/run/init-metadata"

	// GitMetadataFile is the absolute path of the file that the init container
	// writes after a successful git clone (mode 0600; init and sidecar share the
	// pod UID). It contains the resolved commit SHA (one line, no trailing
	// whitespace). The sidecar retries reading it on each BenchmarkStatusEvent
	// proxy call until loaded, then injects it as JobMeta.ResolvedSHA on every event.
	GitMetadataFile = InitMetadataDir + "/.git-metadata"
)
