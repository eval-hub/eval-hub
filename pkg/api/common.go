package api

import "time"

// ------------------------------------------------------------------------------------------------
// General naming conventions:
// ------------------------------------------------------------------------------------------------
// - ...Config - represents an object specified by the user when creating or updating a resource.
// - ...Resource - represents an object stored in the database. This is the REST resource.
// - ...ResourceList - represents a list of REST resources
// - ...Ref - represents a reference to an object
// - ...Error - represents an error response
// ------------------------------------------------------------------------------------------------

// PatchOp represents the patch operation enum
type PatchOp string

// The tenant that provides scoping for objects stored in the database but not limited to the database.
type Tenant string

const (
	PatchOpReplace PatchOp = "replace"
	PatchOpAdd     PatchOp = "add"
	PatchOpRemove  PatchOp = "remove"
)

type Ref struct {
	ID string `json:"id"`
}

type HRef struct {
	Href string `json:"href"`
}

// Error represents an error response
type Error struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
	Trace   string `json:"trace"`
}

// PatchOperation represents a single patch operation
type PatchOperation struct {
	Op    PatchOp `json:"op"`
	Path  string  `json:"path"`
	Value any     `json:"value,omitempty"`
}

// Patch represents a list of patch operations
type Patch []PatchOperation

// Resource represents base resource fields
type Resource struct {
	ID        string    `json:"id"`
	Tenant    Tenant    `json:"tenant"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Page represents generic pagination schema
type Page struct {
	First      *HRef `json:"first"`
	Next       *HRef `json:"next,omitempty"`
	Limit      int   `json:"limit"`
	TotalCount int   `json:"total_count"`
}
