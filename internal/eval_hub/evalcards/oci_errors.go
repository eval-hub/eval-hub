package evalcards

// OCIExportError associates an export failure with its operation and cause.
type OCIExportError struct {
	Operation string
	Reason    string
	Err       error
}

func (e *OCIExportError) Error() string {
	if e.Err != nil {
		return e.Operation + ": " + e.Err.Error()
	}
	return e.Operation + ": " + e.Reason
}

func (e *OCIExportError) Unwrap() error { return e.Err }
