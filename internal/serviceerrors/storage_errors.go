package serviceerrors

import "fmt"

// StorageError represents an error in storage operations
type StorageError struct {
	Message string
	Code    int
}

func (e *StorageError) Error() string {
	return e.Message
}

func NewStorageErrorWithError(err error, format string, a ...any) *StorageError {
	msg := fmt.Sprintf(format, a...)
	e := fmt.Errorf("%s: %w", msg, err)
	return &StorageError{Message: e.Error(), Code: 500}
}

func NewStorageError(format string, a ...any) *StorageError {
	return &StorageError{Message: fmt.Sprintf(format, a...), Code: 500}
}

func NewStorageErrorWithCode(code int, format string, a ...any) *StorageError {
	return &StorageError{Message: fmt.Sprintf(format, a...), Code: code}
}
