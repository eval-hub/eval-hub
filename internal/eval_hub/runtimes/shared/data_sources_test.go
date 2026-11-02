package shared

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

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
