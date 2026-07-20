package features

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluationJobResourceSchemaFiles(t *testing.T) {
	connectedSample := `{
		"resource": {
			"id": "00000000-0000-0000-0000-000000000001",
			"tenant": "test-tenant",
			"created_at": "2026-01-01T00:00:00Z",
			"owner": "test-user"
		},
		"status": {
			"state": "pending",
			"message": {
				"message": "Evaluation job created",
				"message_code": "evaluation_job_created"
			}
		},
		"results": {},
		"name": "test-evaluation-job",
		"tags": ["environment"],
		"model": {
			"name": "test",
			"url": "http://test.com",
			"auth": {"secret_ref": "model-auth"}
		},
		"benchmarks": [{
			"id": "arc_easy",
			"provider_id": "lm_evaluation_harness",
			"parameters": {
				"num_examples": 10,
				"num_fewshot": 3,
				"tokenizer": "google/flan-t5-small"
			}
		}]
	}`

	disconnectedSample := `{
		"resource": {
			"id": "00000000-0000-0000-0000-000000000001",
			"tenant": "test-tenant",
			"created_at": "2026-01-01T00:00:00Z",
			"owner": "test-user"
		},
		"status": {
			"state": "pending",
			"message": {
				"message": "Evaluation job created",
				"message_code": "evaluation_job_created"
			}
		},
		"results": {},
		"name": "test-evaluation-job",
		"tags": ["environment"],
		"model": {
			"name": "test",
			"url": "http://test.com",
			"auth": {"secret_ref": "model-auth"}
		},
		"benchmarks": [{
			"id": "arc_easy",
			"provider_id": "lm_evaluation_harness",
			"parameters": {
				"num_examples": 10,
				"num_fewshot": 3,
				"tokenizer": "/test_data/tokenizer"
			},
			"test_data_ref": {
				"s3": {
					"bucket": "mlpipeline",
					"key": "offline",
					"secret_ref": "minio-test"
				}
			}
		}]
	}`

	requestOnlySample := `{
		"name": "test-evaluation-job",
		"model": {"name": "test", "url": "http://test.com"},
		"benchmarks": [{"id": "arc_easy", "provider_id": "lm_evaluation_harness"}]
	}`

	tc := &scenarioConfig{}
	cases := []struct {
		name     string
		schema   string
		payload  string
		wantFail bool
	}{
		{
			name:    "connected response",
			schema:  "schemas/evaluation_job_resource_connected.schema.json",
			payload: connectedSample,
		},
		{
			name:    "disconnected response",
			schema:  "schemas/evaluation_job_resource_disconnected.schema.json",
			payload: disconnectedSample,
		},
		{
			name:   "connected response with message_origin",
			schema: "schemas/evaluation_job_resource_connected.schema.json",
			payload: `{
				"resource": {
					"id": "00000000-0000-0000-0000-000000000001",
					"tenant": "test-tenant",
					"created_at": "2026-01-01T00:00:00Z",
					"owner": "test-user"
				},
				"status": {
					"state": "pending",
					"message": {
						"message": "Evaluation job created",
						"message_code": "evaluation_job_created",
						"message_origin": "server"
					}
				},
				"results": {},
				"name": "test-evaluation-job",
				"model": {
					"name": "test",
					"url": "http://test.com"
				},
				"benchmarks": [{
					"id": "arc_easy",
					"provider_id": "lm_evaluation_harness",
					"parameters": {
						"tokenizer": "google/flan-t5-small"
					}
				}]
			}`,
		},
		{
			name:     "request body rejected by response schema",
			schema:   "schemas/evaluation_job_resource_connected.schema.json",
			payload:  requestOnlySample,
			wantFail: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tc.findFile(tt.schema); err != nil {
				t.Fatalf("find schema: %v", err)
			}
			err := tc.compareJSONSchemaFile(tt.schema, tt.payload)
			if tt.wantFail && err == nil {
				t.Fatal("expected schema validation to fail")
			}
			if !tt.wantFail && err != nil {
				t.Fatalf("schema validation failed: %v", err)
			}
		})
	}
}

func TestEvaluationJobResourceSchemaFilesExist(t *testing.T) {
	root := testDataRoot()
	for _, name := range []string{
		"evaluation_job_resource_connected.schema.json",
		"evaluation_job_resource_disconnected.schema.json",
	} {
		path := filepath.Join(root, "schemas", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("schema file missing: %s", path)
		}
	}
}
