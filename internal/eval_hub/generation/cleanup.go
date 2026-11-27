package generation

import "time"

const (
	// RuntimeResourceCleanupTimeout bounds a DELETE request's runtime cleanup work.
	RuntimeResourceCleanupTimeout = time.Minute

	// CleanupProtectionLease keeps a row out of retention cleanup long enough for
	// the bounded runtime cleanup request to finish, including a short DB retry.
	CleanupProtectionLease = 2 * time.Minute
)
