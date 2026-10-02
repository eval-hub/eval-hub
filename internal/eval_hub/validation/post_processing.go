package validation

import (
	"regexp"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

var ociDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validatePostProcessingOperations(sl validator.StructLevel) {
	operations, ok := sl.Current().Interface().(interface{ HasOperation() bool })
	if ok {
		reportIfNoPostProcessingOperation(sl, operations.HasOperation())
	}
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

func reportIfNoPostProcessingOperation(sl validator.StructLevel, hasOperation bool) {
	if !hasOperation {
		sl.ReportError(nil, "operations", "Operations", "at_least_one_operation", "")
	}
}

func validatePostProcessingResultsDataRef(sl validator.StructLevel) {
	ref := sl.Current().Interface().(api.PostProcessingResultsDataRef)
	sources := 0
	for _, present := range []bool{ref.EvalJob != nil, ref.S3 != nil, ref.PVC != nil, ref.Git != nil, ref.HF != nil, ref.MLFlow != nil, ref.OCI != nil} {
		if present {
			sources++
		}
	}
	if sources != 1 {
		sl.ReportError(ref, "results_data_ref", "ResultsDataRef", "exactly_one_source", "")
	}
	if ref.Type != "" && ref.S3 == nil && ref.PVC == nil && ref.Git == nil && ref.HF == nil {
		sl.ReportError(ref.Type, "type", "Type", "test_data_source_required", "")
	}
	if ref.ResolvedSHA != "" {
		sl.ReportError(ref.ResolvedSHA, "resolved_sha", "ResolvedSHA", "read_only", "")
	}
}

func validateStandaloneConfidenceIntervalConfig(sl validator.StructLevel) {
	cfg := sl.Current().Interface().(api.StandaloneConfidenceIntervalConfig)
	for _, ref := range cfg.CalibrationDataRef {
		if ref.ResolvedSHA != "" {
			sl.ReportError(ref.ResolvedSHA, "calibration_data_ref.resolved_sha", "CalibrationDataRef", "read_only", "")
		}
	}
	if cfg.ResultsDataRef != nil && cfg.ResultsDataRef.EvalJob == nil && cfg.PrimaryScore == nil {
		sl.ReportError(cfg.PrimaryScore, "primary_score", "PrimaryScore", "required_for_external_results", "")
	}
	if cfg.ResultsDataRef != nil && cfg.ResultsDataRef.EvalJob != nil && cfg.PrimaryScore != nil {
		sl.ReportError(cfg.PrimaryScore, "primary_score", "PrimaryScore", "not_allowed_for_eval_job_results", "")
	}
}

func validateCalibrationDataRef(sl validator.StructLevel) {
	ref := sl.Current().Interface().(api.CalibrationDataRef)
	sources := 0
	for _, present := range []bool{ref.S3 != nil, ref.PVC != nil, ref.Git != nil, ref.HF != nil} {
		if present {
			sources++
		}
	}
	if sources != 1 {
		sl.ReportError(ref, "calibration_data_ref", "CalibrationDataRef", "exactly_one_source", "")
	}
	if ref.ResolvedSHA != "" {
		sl.ReportError(ref.ResolvedSHA, "resolved_sha", "ResolvedSHA", "read_only", "")
	}
}

func validateOCIDataRef(sl validator.StructLevel) {
	ref := sl.Current().Interface().(api.OCIDataRef)
	if ref.Digest != "" && !ociDigestPattern.MatchString(ref.Digest) {
		sl.ReportError(ref.Digest, "digest", "Digest", "sha256_digest", "")
	}
}
