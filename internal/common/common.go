package common

import "github.com/google/uuid"

func GUID() string {
	return uuid.New().String()
}
