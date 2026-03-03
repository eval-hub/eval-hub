package shared

import (
	"github.com/eval-hub/eval-hub/pkg/api"
)

type EvaluationJobQuery struct {
	Resource   api.EvaluationResource
	Status     string
	EntityJSON string
}

type CollectionQuery struct {
	Resource   api.Resource
	EntityJSON string
}

type ProviderQuery struct {
	Resource   api.Resource
	EntityJSON string
}
