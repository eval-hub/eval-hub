package oci

// OperationError associates an OCI failure with its operation and cause.
type OperationError struct {
	Operation string
	Reason    string
	Err       error
}

func (e *OperationError) Error() string {
	if e.Err != nil {
		return e.Operation + ": " + e.Err.Error()
	}
	return e.Operation + ": " + e.Reason
}

func (e *OperationError) Unwrap() error { return e.Err }
