package validation

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	validator "github.com/go-playground/validator/v10"
	"github.com/go-playground/validator/v10/non-standard/validators"
)

var (
	tagAliases = map[string]string{
		// this is the definition for tag name validation
		"tagname": "max=128,min=1,excludesall=0x2C0x7C",
		// this is the definition for id validation for a uuid - system resources are not uuid's
		"resource_id": "required,min=1,max=36",
	}

	// RFC 1123 DNS label: lowercase alphanumeric, internal hyphens, no leading/trailing hyphen.
	rfc1123DNSLabelRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
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
	// https://github.com/go-playground/validator/blob/v10.30.2/non-standard/validators/notblank.go
	instance.RegisterValidation("notblank", validators.NotBlank)
	instance.RegisterValidation("rfc1123_dns_label", validateRFC1123DNSLabel)
	// Benchmarks min=1 only when Collection is not set (required_without handles presence; this enforces length)
	instance.RegisterStructValidation(evaluationJobConfigBenchmarksMin, api.EvaluationJobConfig{})
}

func validateRFC1123DNSLabel(fl validator.FieldLevel) bool {
	return rfc1123DNSLabelRegex.MatchString(fl.Field().String())
}

// ValidateCollectionOverrides returns an error if any override references a
// provider_id or benchmark id that does not exist in the collection.
// It must be called after the collection is fetched from storage.
func ValidateCollectionOverrides(overrides []api.EvaluationBenchmarkConfig, collectionBenchmarks []api.CollectionBenchmarkConfig) error {
	if len(overrides) == 0 {
		return nil
	}
	type benchmarkKey struct{ providerID, id string }
	providerIDs := make(map[string]struct{}, len(collectionBenchmarks))
	pairs := make(map[benchmarkKey]struct{}, len(collectionBenchmarks))
	for _, b := range collectionBenchmarks {
		providerIDs[b.ProviderID] = struct{}{}
		pairs[benchmarkKey{b.ProviderID, b.ID}] = struct{}{}
	}
	for _, override := range overrides {
		if _, ok := providerIDs[override.ProviderID]; !ok {
			return serviceerrors.NewServiceError(
				messages.ResourceDoesNotExist,
				"Type", "provider",
				"ResourceID", override.ProviderID,
			)
		}
		if override.ID != "" {
			if _, ok := pairs[benchmarkKey{override.ProviderID, override.ID}]; !ok {
				return serviceerrors.NewServiceError(
					messages.ResourceDoesNotExist,
					"Type", "benchmark",
					"ResourceID", override.ID,
				)
			}
		}
	}
	return nil
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
