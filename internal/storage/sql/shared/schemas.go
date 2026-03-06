package shared

import (
	"github.com/eval-hub/eval-hub/pkg/api"
)

// this has all the fields to cover all the entities in the database
type EntityQuery struct {
	Resource           api.Resource
	MLFlowExperimentID string
	Status             string
	EntityJSON         string
}
