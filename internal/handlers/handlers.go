package handlers

import (
	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/go-playground/validator/v10"
)

type Handlers struct {
	storage  abstractions.Storage
	validate *validator.Validate
}

func New(storage abstractions.Storage, validate *validator.Validate) *Handlers {
	return &Handlers{
		storage:  storage,
		validate: validate,
	}
}
