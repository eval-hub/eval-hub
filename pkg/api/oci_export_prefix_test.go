package api

import (
	"strings"
	"testing"
)

func TestValidateOCIExportPrefix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prefix  string
		wantErr bool
	}{
		{name: "empty"},
		{name: "letters", prefix: "Evaluation"},
		{name: "leading digit", prefix: "1result"},
		{name: "leading underscore", prefix: "_result"},
		{name: "allowed punctuation", prefix: "eval.v1_results-"},
		{name: "reserved card prefix", prefix: "evaluation-card", wantErr: true},
		{name: "custom card prefix", prefix: "evaluation-card-custom"},
		{name: "case distinct card prefix", prefix: "Evaluation-card"},
		{name: "tag length boundary", prefix: strings.Repeat("a", 128)},
		{name: "long prefix", prefix: strings.Repeat("a", 256)},
		{name: "leading period", prefix: ".result", wantErr: true},
		{name: "leading hyphen", prefix: "-result", wantErr: true},
		{name: "slash", prefix: "team/results", wantErr: true},
		{name: "colon", prefix: "eval:latest", wantErr: true},
		{name: "space", prefix: "eval results", wantErr: true},
		{name: "trailing newline", prefix: "eval\n", wantErr: true},
		{name: "non ASCII", prefix: "eval-é", wantErr: true},
		{name: "invalid suffix beyond tag limit", prefix: strings.Repeat("a", 256) + "/", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOCIExportPrefix(tc.prefix)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateOCIExportPrefix(%q) = %v, want error: %v", tc.prefix, err, tc.wantErr)
			}
		})
	}
}
