package handlers

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestAllBenchmarksHavePreRecordedData_EmptySlice(t *testing.T) {
	if allBenchmarksHavePreRecordedData(nil) {
		t.Fatal("expected false for nil slice")
	}
	if allBenchmarksHavePreRecordedData([]api.EvaluationBenchmarkConfig{}) {
		t.Fatal("expected false for empty slice")
	}
}

func TestAllBenchmarksHavePreRecordedData_AllPreRecorded(t *testing.T) {
	benchmarks := []api.EvaluationBenchmarkConfig{
		{TestDataRef: &api.TestDataRef{Type: "pre_recorded_data"}},
		{TestDataRef: &api.TestDataRef{Type: "pre_recorded_data"}},
	}
	if !allBenchmarksHavePreRecordedData(benchmarks) {
		t.Fatal("expected true when all benchmarks have pre_recorded_data")
	}
}

func TestAllBenchmarksHavePreRecordedData_NonePreRecorded(t *testing.T) {
	benchmarks := []api.EvaluationBenchmarkConfig{
		{TestDataRef: &api.TestDataRef{Type: "data_set"}},
		{TestDataRef: &api.TestDataRef{Type: "data_set"}},
	}
	if allBenchmarksHavePreRecordedData(benchmarks) {
		t.Fatal("expected false when no benchmarks have pre_recorded_data")
	}
}

func TestAllBenchmarksHavePreRecordedData_Mixed(t *testing.T) {
	benchmarks := []api.EvaluationBenchmarkConfig{
		{TestDataRef: &api.TestDataRef{Type: "pre_recorded_data"}},
		{TestDataRef: &api.TestDataRef{Type: "data_set"}},
	}
	if allBenchmarksHavePreRecordedData(benchmarks) {
		t.Fatal("expected false when only some benchmarks have pre_recorded_data")
	}
}

func TestAllBenchmarksHavePreRecordedData_NilTestDataRef(t *testing.T) {
	benchmarks := []api.EvaluationBenchmarkConfig{
		{TestDataRef: nil},
	}
	if allBenchmarksHavePreRecordedData(benchmarks) {
		t.Fatal("expected false when TestDataRef is nil")
	}
}

func TestAllBenchmarksHavePreRecordedData_EmptyType(t *testing.T) {
	benchmarks := []api.EvaluationBenchmarkConfig{
		{TestDataRef: &api.TestDataRef{Type: ""}},
	}
	if allBenchmarksHavePreRecordedData(benchmarks) {
		t.Fatal("expected false when Type is empty string")
	}
}

func TestAllBenchmarksHavePreRecordedData_SinglePreRecorded(t *testing.T) {
	benchmarks := []api.EvaluationBenchmarkConfig{
		{TestDataRef: &api.TestDataRef{Type: "pre_recorded_data"}},
	}
	if !allBenchmarksHavePreRecordedData(benchmarks) {
		t.Fatal("expected true for single pre_recorded_data benchmark")
	}
}
