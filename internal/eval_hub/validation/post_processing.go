package validation

import (
	"regexp"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

var ociDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validateResultsDataConfigEntry(sl validator.StructLevel) {
	entry := sl.Current().Interface().(api.ResultsDataConfigEntry)
	if entry.RecordsPath == nil {
		return
	}
	// The post-processor owns JSONPath parsing and record-array validation.
	// Keep the API check consistent with the schema without a second dialect.
	if !strings.HasPrefix(*entry.RecordsPath, "$") {
		sl.ReportError(*entry.RecordsPath, "records_path", "RecordsPath", "json_path", "")
	}
	if entry.Format != "" && entry.Format != "auto" && entry.Format != "json" {
		sl.ReportError(*entry.RecordsPath, "records_path", "RecordsPath", "requires_json", "")
	}
}

func validateOCIDigest(fl validator.FieldLevel) bool {
	return ociDigestPattern.MatchString(fl.Field().String())
}

func validateStandalonePostProcessingRequest(sl validator.StructLevel) {
	request, ok := sl.Current().Interface().(api.StandalonePostProcessingRequest)
	if !ok {
		return
	}
	validatePostProcessingOperationOrder(sl, request.Operations.OperationNames(), request.OperationOrder)
}

func validateJobPostProcessingRequest(sl validator.StructLevel) {
	request, ok := sl.Current().Interface().(api.ConfidenceIntervalPostProcessingRequest)
	if !ok {
		return
	}
	validatePostProcessingOperationOrder(sl, request.Operations.OperationNames(), request.OperationOrder)
}

func validatePostProcessingOperationOrder(sl validator.StructLevel, operationNames, requestedOrder []string) {
	if requestedOrder == nil {
		return
	}
	if len(requestedOrder) != len(operationNames) {
		reportInvalidPostProcessingOperationOrder(sl, requestedOrder)
		return
	}
	available := make(map[string]struct{}, len(operationNames))
	for _, name := range operationNames {
		available[name] = struct{}{}
	}
	for _, name := range requestedOrder {
		if _, ok := available[name]; !ok {
			reportInvalidPostProcessingOperationOrder(sl, requestedOrder)
			return
		}
		delete(available, name)
	}
}

func reportInvalidPostProcessingOperationOrder(sl validator.StructLevel, order []string) {
	sl.ReportError(order, "operation_order", "OperationOrder", "operation_order_matches_operations", "")
}
