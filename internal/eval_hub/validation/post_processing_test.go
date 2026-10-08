package validation

import (
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	validator "github.com/go-playground/validator/v10"
)

func TestPostProcessingOperationsRequireAtLeastOneOperation(t *testing.T) {
	validate := newTestValidator(t)
	tests := []struct {
		name       string
		operations any
		wantErr    bool
	}{
		{name: "empty standalone operations", operations: api.StandalonePostProcessingOperations{}, wantErr: true},
		{name: "standalone confidence interval", operations: api.StandalonePostProcessingOperations{ConfidenceInterval: validStandaloneConfidenceInterval()}},
		{name: "empty job operations", operations: api.JobPostProcessingOperations{}, wantErr: true},
		{name: "job confidence interval", operations: api.JobPostProcessingOperations{ConfidenceInterval: validJobConfidenceInterval()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate.Struct(test.operations)
			if !test.wantErr && err != nil {
				t.Fatalf("validation error = %v", err)
			}
			if test.wantErr {
				assertValidationTag(t, err, "required")
			}
		})
	}
}

func TestPostProcessingOperationOrderValidation(t *testing.T) {
	validate := newTestValidator(t)
	validStandalone := api.StandalonePostProcessingRequest{
		Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: validStandaloneConfidenceInterval()},
	}
	validJob := api.ConfidenceIntervalPostProcessingRequest{
		Operations: api.JobPostProcessingOperations{ConfidenceInterval: validJobConfidenceInterval()},
	}
	tests := []struct {
		name       string
		standalone *api.StandalonePostProcessingRequest
		job        *api.ConfidenceIntervalPostProcessingRequest
		wantErr    bool
	}{
		{name: "standalone order omitted", standalone: &validStandalone},
		{name: "standalone matching order", standalone: withStandaloneOrder(validStandalone, []string{"confidence_interval"})},
		{name: "standalone empty order", standalone: withStandaloneOrder(validStandalone, []string{}), wantErr: true},
		{name: "standalone unknown operation", standalone: withStandaloneOrder(validStandalone, []string{"other"}), wantErr: true},
		{name: "standalone duplicate operation", standalone: withStandaloneOrder(validStandalone, []string{"confidence_interval", "confidence_interval"}), wantErr: true},
		{name: "job order omitted", job: &validJob},
		{name: "job matching order", job: withJobOrder(validJob, []string{"confidence_interval"})},
		{name: "job empty order", job: withJobOrder(validJob, []string{}), wantErr: true},
		{name: "job unknown operation", job: withJobOrder(validJob, []string{"other"}), wantErr: true},
		{name: "job duplicate operation", job: withJobOrder(validJob, []string{"confidence_interval", "confidence_interval"}), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.standalone != nil {
				err = validate.Struct(test.standalone)
			} else {
				err = validate.Struct(test.job)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validation error = %v", err)
			}
			if test.wantErr {
				assertValidationTag(t, err, "operation_order_matches_operations")
			}
		})
	}
}

func TestPostProcessingResultsDataRefValidation(t *testing.T) {
	validate := newTestValidator(t)
	tests := []struct {
		name string
		ref  api.PostProcessingResultsDataRef
		want string
	}{
		{name: "no source", want: "required_without_all"},
		{name: "eval job", ref: api.PostProcessingResultsDataRef{EvalJob: &api.EvaluationJobDataRef{ID: "job-id"}}},
		{name: "s3", ref: api.PostProcessingResultsDataRef{S3: validS3Ref()}},
		{name: "pvc", ref: api.PostProcessingResultsDataRef{PVC: validPVCRef()}},
		{name: "git", ref: api.PostProcessingResultsDataRef{Git: validGitRef()}},
		{name: "hugging face", ref: api.PostProcessingResultsDataRef{HF: validHFRef()}},
		{name: "mlflow", ref: api.PostProcessingResultsDataRef{MLFlow: &api.MLflowDataRef{RunID: "run-id", ArtifactPath: "results.jsonl"}}},
		{name: "oci", ref: api.PostProcessingResultsDataRef{OCI: validOCIRef()}},
		{
			name: "multiple sources",
			ref:  api.PostProcessingResultsDataRef{S3: validS3Ref(), PVC: validPVCRef()},
			want: "excluded_with",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate.Struct(test.ref)
			if test.want == "" && err != nil {
				t.Fatalf("validation error = %v", err)
			}
			if test.want != "" {
				assertValidationTag(t, err, test.want)
			}
		})
	}
}

func TestCalibrationDataRefValidation(t *testing.T) {
	validate := newTestValidator(t)
	tests := []struct {
		name string
		ref  api.CalibrationDataRef
		want string
	}{
		{name: "no source", ref: calibrationRefWithSource(nil), want: "required_without_all"},
		{name: "s3", ref: calibrationRefWithSource(func(ref *api.CalibrationDataRef) { ref.S3 = validS3Ref() })},
		{name: "pvc", ref: calibrationRefWithSource(func(ref *api.CalibrationDataRef) { ref.PVC = validPVCRef() })},
		{name: "git", ref: calibrationRefWithSource(func(ref *api.CalibrationDataRef) { ref.Git = validGitRef() })},
		{name: "hugging face", ref: calibrationRefWithSource(func(ref *api.CalibrationDataRef) { ref.HF = validHFRef() })},
		{
			name: "multiple sources",
			ref: calibrationRefWithSource(func(ref *api.CalibrationDataRef) {
				ref.PVC = validPVCRef()
				ref.HF = validHFRef()
			}),
			want: "excluded_with",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate.Struct(test.ref)
			if test.want == "" && err != nil {
				t.Fatalf("validation error = %v", err)
			}
			if test.want != "" {
				assertValidationTag(t, err, test.want)
			}
		})
	}
}

func TestStandaloneConfidenceIntervalValidation(t *testing.T) {
	validate := newTestValidator(t)
	external := validStandaloneConfidenceInterval()
	external.ResultsDataRef = &api.PostProcessingResultsDataRef{PVC: validPVCRef()}
	tests := []struct {
		name string
		cfg  api.StandaloneConfidenceIntervalConfig
		want string
	}{
		{name: "eval job source", cfg: *validStandaloneConfidenceInterval()},
		{name: "external source requires primary score", cfg: *external, want: "required_without"},
		{
			name: "primary score not allowed with eval job",
			cfg: func() api.StandaloneConfidenceIntervalConfig {
				cfg := *validStandaloneConfidenceInterval()
				cfg.PrimaryScore = &api.PrimaryScore{Metric: "accuracy"}
				return cfg
			}(),
			want: "excluded_with",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate.Struct(test.cfg)
			if test.want == "" && err != nil {
				t.Fatalf("validation error = %v", err)
			}
			if test.want != "" {
				assertValidationTag(t, err, test.want)
			}
		})
	}
}

func TestOCIDataRefDigestValidation(t *testing.T) {
	validate := newTestValidator(t)
	valid := validOCIRef()
	if err := validate.Struct(*valid); err != nil {
		t.Fatalf("empty digest rejected: %v", err)
	}
	valid.Digest = "sha256:" + strings.Repeat("a", 64)
	if err := validate.Struct(*valid); err != nil {
		t.Fatalf("valid digest rejected: %v", err)
	}
	invalid := *valid
	invalid.Digest = "sha256:ABC"
	assertValidationTag(t, validate.Struct(invalid), "sha256_digest")
}

func validStandaloneConfidenceInterval() *api.StandaloneConfidenceIntervalConfig {
	return &api.StandaloneConfidenceIntervalConfig{
		ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
			CalibrationDataRef: []api.CalibrationDataRef{calibrationRefWithSource(func(ref *api.CalibrationDataRef) { ref.PVC = validPVCRef() })},
			SignificanceLevel:  0.05,
		},
		ResultsDataRef: &api.PostProcessingResultsDataRef{EvalJob: &api.EvaluationJobDataRef{ID: "job-id"}},
	}
}

func validJobConfidenceInterval() *api.ConfidenceIntervalConfig {
	return &api.ConfidenceIntervalConfig{
		ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
			CalibrationDataRef: []api.CalibrationDataRef{calibrationRefWithSource(func(ref *api.CalibrationDataRef) { ref.PVC = validPVCRef() })},
			SignificanceLevel:  0.05,
		},
	}
}

func calibrationRefWithSource(setSource func(*api.CalibrationDataRef)) api.CalibrationDataRef {
	ref := api.CalibrationDataRef{DataConfig: api.CalibrationDataConfig{
		Format:  "jsonl",
		Columns: api.CalibrationDataColumns{Label: "label", Prediction: "prediction"},
	}}
	if setSource != nil {
		setSource(&ref)
	}
	return ref
}

func validS3Ref() *api.S3TestDataRef {
	return &api.S3TestDataRef{Bucket: "bucket", Key: "key", SecretRef: "secret"}
}

func validPVCRef() *api.PVCTestDataRef {
	return &api.PVCTestDataRef{ClaimName: "calibration-data"}
}

func validGitRef() *api.GitTestDataRef {
	return &api.GitTestDataRef{URL: "https://github.com/eval-hub/test-data.git", Ref: "main"}
}

func validHFRef() *api.HFTestDataRef {
	return &api.HFTestDataRef{RepoID: "org/dataset"}
}

func validOCIRef() *api.OCIDataRef {
	return &api.OCIDataRef{
		Coordinates:  api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "org/dataset"},
		ArtifactPath: "results.jsonl",
	}
}

func withStandaloneOrder(request api.StandalonePostProcessingRequest, order []string) *api.StandalonePostProcessingRequest {
	request.OperationOrder = order
	return &request
}

func withJobOrder(request api.ConfidenceIntervalPostProcessingRequest, order []string) *api.ConfidenceIntervalPostProcessingRequest {
	request.OperationOrder = order
	return &request
}

func assertValidationTag(t *testing.T, err error, tag string) {
	t.Helper()
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		t.Fatalf("error = %v, want validator.ValidationErrors containing tag %q", err, tag)
	}
	for _, validationError := range validationErrors {
		if validationError.Tag() == tag {
			return
		}
	}
	t.Fatalf("validation error %v does not contain tag %q", err, tag)
}
