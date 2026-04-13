package api_test

import (
	"encoding/json"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCollectionsValidation(t *testing.T) {
	srcs := []string{
		`
		{
        	"name": "test-collection-1",
        	"category": "test",
        	"description": "Collection of benchmarks for FVT",
        	"pass_criteria": {
            	"threshold": 0
        	},
        	"benchmarks": [
            	{
                	"id": "arc_easy",
                	"provider_id": "lm_evaluation_harness",
                	"primary_score": {
						"metric": "acc_norm",
						"lower_is_better": false
					},
					"pass_criteria": {
                    	"threshold": 0.5
                	},
                	"parameters": {
                    	"limit": 10,
                    	"num_fewshot": 0,
                    	"tokenizer": "google/flan-t5-small"
                	}
            	}
        	]
    	}
		`,
	}

	for _, src := range srcs {
		var config api.CollectionConfig
		err := json.Unmarshal([]byte(src), &config)
		if err != nil {
			t.Fatalf("failed to unmarshal collection config: %v", err)
		}
		validator := validation.NewValidator()
		err = validator.Struct(config)
		if err != nil {
			t.Fatalf("failed to validate collection config: %v", err)
		}
	}
}
