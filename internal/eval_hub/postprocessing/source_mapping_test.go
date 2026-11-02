package postprocessing

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestToEvaluationJobCopiesExternalOCIConfiguration(t *testing.T) {
	source := &api.OCIDataRef{
		Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "org/repo", OCITag: "source", Annotations: map[string]string{"purpose": "source"}},
		Digest:      "sha256:" + strings.Repeat("a", 64), ArtifactPath: "results/data.jsonl",
		K8s: &api.OCIConnectionConfig{Connection: "registry-secret"},
	}
	request := &api.StandalonePostProcessingRequest{Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{ResultsDataRef: &api.PostProcessingResultsDataRef{OCI: source}}}}
	job := ToEvaluationJob(request)
	if job.Exports == nil || job.Exports.OCI == nil {
		t.Fatal("OCI source must configure runtime exports")
	}
	export := job.Exports.OCI
	if !reflect.DeepEqual(export.Coordinates, source.Coordinates) || !reflect.DeepEqual(export.K8s, source.K8s) {
		t.Fatalf("exports lost source registry configuration: %+v", export)
	}
	operations, err := OperationsFromJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(operations.ConfidenceInterval.ResultsDataRef.OCI, source) {
		t.Fatal("source digest or artifact path changed")
	}
	export.Coordinates.Annotations["purpose"] = "changed"
	export.K8s.Connection = "changed"
	if source.Coordinates.Annotations["purpose"] != "source" || source.K8s.Connection != "registry-secret" {
		t.Fatal("runtime configuration aliases request")
	}
}
