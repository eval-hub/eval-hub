package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestResultsDataConfigUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSingle *ResultsDataConfigEntry
		wantList   []ResultsDataConfigEntry
		wantErr    string
	}{
		{
			name:  "object selects one config for all artifacts",
			input: ` {"format":"jsonl","columns":{"prediction":"scores.value"}} `,
			wantSingle: &ResultsDataConfigEntry{
				Format:  "jsonl",
				Columns: map[string]string{"prediction": "scores.value"},
			},
		},
		{
			name:  "array selects configs per benchmark",
			input: `[{"selection":{"provider_id":"provider-a"}},{"selection":{"metric":"accuracy"}}]`,
			wantList: []ResultsDataConfigEntry{
				{Selection: map[string]string{"provider_id": "provider-a"}},
				{Selection: map[string]string{"metric": "accuracy"}},
			},
		},
		{name: "empty input", input: "", wantErr: "object or a nonempty array"},
		{name: "whitespace input", input: " \n\t ", wantErr: "object or a nonempty array"},
		{name: "empty array", input: `[]`, wantErr: "array must not be empty"},
		{name: "array entry has no selection", input: `[{}]`, wantErr: "entries require a nonempty selection"},
		{name: "array entry has empty selection", input: `[{"selection":{}}]`, wantErr: "entries require a nonempty selection"},
		{name: "scalar input", input: `"json"`, wantErr: "object or a nonempty array"},
		{name: "null input", input: `null`, wantErr: "object or a nonempty array"},
		{name: "invalid object JSON", input: `{"format":`, wantErr: "unexpected end of JSON input"},
		{name: "invalid array JSON", input: `[{]`, wantErr: "invalid character"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got ResultsDataConfig
			err := got.UnmarshalJSON([]byte(test.input))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("UnmarshalJSON() error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalJSON() error = %v", err)
			}
			if !reflect.DeepEqual(got.Single, test.wantSingle) || !reflect.DeepEqual(got.PerBenchmark, test.wantList) {
				t.Fatalf("UnmarshalJSON() = %#v, want Single=%#v PerBenchmark=%#v", got, test.wantSingle, test.wantList)
			}
		})
	}
}

func TestResultsDataConfigMarshalJSON(t *testing.T) {
	tests := []struct {
		name   string
		config ResultsDataConfig
		want   string
	}{
		{
			name: "single config marshals as object",
			config: ResultsDataConfig{Single: &ResultsDataConfigEntry{
				Format: "json", Columns: map[string]string{"prediction": "score"},
			}},
			want: `{"format":"json","columns":{"prediction":"score"}}`,
		},
		{
			name: "per benchmark config marshals as array",
			config: ResultsDataConfig{PerBenchmark: []ResultsDataConfigEntry{
				{Selection: map[string]string{"provider_id": "provider-a"}},
			}},
			want: `[{"selection":{"provider_id":"provider-a"}}]`,
		},
		{name: "unset config marshals nil list", config: ResultsDataConfig{}, want: `null`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.config)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("MarshalJSON() = %s, want %s", got, test.want)
			}
		})
	}
}
