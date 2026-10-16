package api

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ResultsDataConfig describes either one format for all result artifacts or
// selected formats for individual source benchmarks. Its JSON form is an
// object or an array, respectively, under results_data_ref.data_config.
type ResultsDataConfig struct {
	Single       *ResultsDataConfigEntry  `json:"-" validate:"required_without=PerBenchmark,excluded_with=PerBenchmark"`
	PerBenchmark []ResultsDataConfigEntry `json:"-" validate:"required_without=Single,excluded_with=Single,omitempty,min=1,dive"`
}

// ResultsDataConfigEntry maps a result artifact into per-example numeric scores.
// Mapping keys are literal field names or dotted paths into nested JSON objects.
type ResultsDataConfigEntry struct {
	Format        string                        `json:"format,omitempty" validate:"omitempty,oneof=auto json jsonl csv parquet"`
	Path          string                        `json:"path,omitempty" validate:"omitempty,notblank"`
	Columns       map[string]string             `json:"columns,omitempty" validate:"omitempty,dive,keys,oneof=sample_id prediction label metric benchmark_id provider_id,endkeys,notblank"`
	Selection     map[string]string             `json:"selection,omitempty" validate:"omitnil,min=1,dive,keys,oneof=benchmark_id provider_id metric,endkeys,notblank"`
	ValueMappings map[string]map[string]float64 `json:"value_mappings,omitempty" validate:"omitempty,dive,keys,oneof=prediction label,endkeys,required,min=1,dive"`
}

// UnmarshalJSON distinguishes null mapping targets from numeric zero before
// converting them to the public numeric mapping type.
func (entry *ResultsDataConfigEntry) UnmarshalJSON(data []byte) error {
	type entryAlias ResultsDataConfigEntry
	var decoded struct {
		entryAlias
		ValueMappings map[string]map[string]*float64 `json:"value_mappings"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.ValueMappings != nil {
		decoded.entryAlias.ValueMappings = make(map[string]map[string]float64, len(decoded.ValueMappings))
		for role, targets := range decoded.ValueMappings {
			var values map[string]float64
			if targets != nil {
				values = make(map[string]float64, len(targets))
			}
			for category, target := range targets {
				if target == nil {
					return fmt.Errorf("results_data_ref.data_config.value_mappings[%q][%q] must be a number", role, category)
				}
				values[category] = *target
			}
			decoded.entryAlias.ValueMappings[role] = values
		}
	}
	*entry = ResultsDataConfigEntry(decoded.entryAlias)
	return nil
}

func (config *ResultsDataConfig) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("results_data_ref.data_config must be an object or a nonempty array")
	}
	var decoded ResultsDataConfig
	switch data[0] {
	case '{':
		decoded.Single = &ResultsDataConfigEntry{}
		if err := json.Unmarshal(data, decoded.Single); err != nil {
			return err
		}
	case '[':
		if err := json.Unmarshal(data, &decoded.PerBenchmark); err != nil {
			return err
		}
		if len(decoded.PerBenchmark) == 0 {
			return fmt.Errorf("results_data_ref.data_config array must not be empty")
		}
		for _, entry := range decoded.PerBenchmark {
			if len(entry.Selection) == 0 {
				return fmt.Errorf("results_data_ref.data_config array entries require a nonempty selection")
			}
		}
	default:
		return fmt.Errorf("results_data_ref.data_config must be an object or a nonempty array")
	}
	*config = decoded
	return nil
}

func (config ResultsDataConfig) MarshalJSON() ([]byte, error) {
	if config.Single != nil {
		return json.Marshal(config.Single)
	}
	return json.Marshal(config.PerBenchmark)
}
