package validation

import (
	"reflect"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	validator "github.com/go-playground/validator/v10"
)

var (
	tagAliases = map[string]string{
		// this is the definition for tag name validation
		"tagname": "max=128,min=1,excludesall=0x2C0x7C",
		// this is the definition for id validation for a uuid - system resources are not uuid's
		"resource_id": "required,min=1,max=36",
	}
)

func NewValidator() *validator.Validate {
	validate := validator.New(validator.WithRequiredStructEnabled())
	for alias, definition := range tagAliases {
		validate.RegisterAlias(alias, definition)
	}
	register(validate)
	registerCustomValidators(validate)
	return validate
}

func register(instance *validator.Validate) {
	// register function to get tag name from json tags
	instance.RegisterTagNameFunc(
		func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		},
	)
}

func registerCustomValidators(instance *validator.Validate) {
	// Benchmarks min=1 only when Collection is not set (required_without handles presence; this enforces length)
	instance.RegisterStructValidation(evaluationJobConfigBenchmarksMin, api.EvaluationJobConfig{})
}

// evaluationJobConfigBenchmarksMin ensures Benchmarks has at least one element when Collection is not present
// and no benchmarks are provided when Collection is set.
func evaluationJobConfigBenchmarksMin(sl validator.StructLevel) {
	if cfg, ok := sl.Current().Interface().(api.EvaluationJobConfig); ok {
		if cfg.Collection != nil && cfg.Collection.ID != "" {
			if len(cfg.Benchmarks) > 0 {
				sl.ReportError(cfg.Benchmarks, "benchmarks", "benchmarks", "benchmarks or collection", "collection")
			}
			return
		}
		if len(cfg.Benchmarks) < 1 {
			sl.ReportError(cfg.Benchmarks, "benchmarks", "benchmarks", "minimum one benchmark", "1")
		}
	}
}
