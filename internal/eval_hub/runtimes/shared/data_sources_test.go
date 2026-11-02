package shared

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// TestPostProcessingDataSourcesPreservesRoles checks the shared init/adapter contract for all source kinds.
func TestPostProcessingDataSourcesPreservesRoles(t *testing.T) {
	results := map[string]*api.PostProcessingResultsDataRef{
		"s3":       {S3: &api.S3TestDataRef{Bucket: "results", SecretRef: "s3-auth"}},
		"git":      {Git: &api.GitTestDataRef{URL: "https://example.org/results", Ref: "pinned"}},
		"hf":       {HF: &api.HFTestDataRef{RepoID: "org/results", Revision: "pinned"}},
		"pvc":      {PVC: &api.PVCTestDataRef{ClaimName: "results", SubPath: "scores"}},
		"eval_job": {EvalJob: &api.EvaluationJobDataRef{ID: "completed"}},
		"mlflow":   {MLFlow: &api.MLflowDataRef{RunID: "run"}},
		"oci":      {OCI: &api.OCIDataRef{Coordinates: api.OCICoordinates{OCIHost: "registry.example", OCIRepository: "results"}}},
	}
	calibration := []api.CalibrationDataRef{
		{S3: &api.S3TestDataRef{Bucket: "labels", SecretRef: "labels-auth"}},
		{Git: &api.GitTestDataRef{URL: "https://example.org/labels", Ref: "labels-revision"}},
		{HF: &api.HFTestDataRef{RepoID: "org/labels", Revision: "labels-revision"}},
		{PVC: &api.PVCTestDataRef{ClaimName: "labels", SubPath: "calibration"}},
	}
	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			parameters := map[string]any{"operations": api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
				ResultsDataRef: result, ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{CalibrationDataRef: calibration},
			}}}
			refs, err := PostProcessingDataSources(parameters)
			if err != nil {
				t.Fatal(err)
			}
			want := []NamedDataSource{
				{Name: "calibration-0", Source: api.TestDataRef{S3: calibration[0].S3}},
				{Name: "calibration-1", Source: api.TestDataRef{Git: calibration[1].Git}},
				{Name: "calibration-2", Source: api.TestDataRef{HF: calibration[2].HF}},
				{Name: "calibration-3", Source: api.TestDataRef{PVC: calibration[3].PVC}},
			}
			if name == "s3" || name == "git" || name == "hf" || name == "pvc" {
				want = append([]NamedDataSource{{Name: "results", Source: api.TestDataRef{S3: result.S3, Git: result.Git, HF: result.HF, PVC: result.PVC}}}, want...)
			}
			if !reflect.DeepEqual(refs, want) {
				t.Fatalf("source roles or configuration changed: got %+v, want %+v", refs, want)
			}
		})
	}
}

// TestPostProcessingDataSourcesRejectsInvalidRoles ensures ambiguous inputs identify the offending role.
func TestPostProcessingDataSourcesRejectsInvalidRoles(t *testing.T) {
	for _, test := range []struct {
		name        string
		results     *api.PostProcessingResultsDataRef
		calibration []api.CalibrationDataRef
		role        string
	}{
		{"ambiguous results", &api.PostProcessingResultsDataRef{S3: &api.S3TestDataRef{}, PVC: &api.PVCTestDataRef{}}, nil, "results"},
		{"missing calibration source", nil, []api.CalibrationDataRef{{}}, "calibration-0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := PostProcessingDataSources(map[string]any{"operations": api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
				ResultsDataRef: test.results, ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{CalibrationDataRef: test.calibration},
			}}})
			if err == nil || !strings.Contains(err.Error(), test.role+": exactly one") {
				t.Fatalf("expected role-specific validation error, got %v", err)
			}
		})
	}
}

// TestPostProcessingDataSourcesRejectMalformedOperations rejects malformed operation payloads before input staging.
func TestPostProcessingDataSourcesRejectMalformedOperations(t *testing.T) {
	for name, parameters := range map[string]map[string]any{
		"missing":               nil,
		"null":                  {"operations": nil},
		"wrong type":            {"operations": "bad"},
		"unserializable":        {"operations": make(chan int)},
		"ambiguous calibration": {"operations": api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{CalibrationDataRef: []api.CalibrationDataRef{{S3: &api.S3TestDataRef{}, HF: &api.HFTestDataRef{}}}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PostProcessingDataSources(parameters); err == nil {
				t.Fatal("accepted invalid operations")
			}
		})
	}
}
