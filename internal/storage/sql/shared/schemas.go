package shared

import (
	"time"
)

type EvaluationJobQuery struct {
	ID           string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Tenant       string
	Status       string
	ExperimentID string
	EntityJSON   string
}

type CollectionQuery struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Tenant     string
	EntityJSON string
}

type ProviderQuery struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Tenant     string
	EntityJSON string
}
