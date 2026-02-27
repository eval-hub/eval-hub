package validation

import (
	"reflect"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	validator "github.com/go-playground/validator/v10"
)

func NewValidator() (*validator.Validate, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	register(validate)
	registerCustomValidators(validate)
	return validate, nil
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

// evaluationJobConfigBenchmarksMin ensures Benchmarks has at least one element when Collection is not present.
func evaluationJobConfigBenchmarksMin(sl validator.StructLevel) {
	cfg := sl.Current().Interface().(api.EvaluationJobConfig)
	if cfg.Collection != nil && cfg.Collection.ID != "" {
		return
	}
	if len(cfg.Benchmarks) < 1 {
		sl.ReportError(cfg.Benchmarks, "Benchmarks", "benchmarks", "min=1", "")
	}
}
