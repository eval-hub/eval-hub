# Job-scoped post-processing API test report

- **Date:** 2026-10-15
- **Branch:** `codex/job-scoped-post-processing-apis`
- **Server:** local EvalHub, `http://127.0.0.1:8080`, local runtime, in-memory SQLite
- **Result:** All requested API status checks passed.

The evaluation job used the local deterministic test adapter. Its request included dummy OCI coordinates (`http://localhost:5001`, repository `evalhub/dummy`, tag `test`). Per instruction, no OCI registry was started or queried; artifact publication was not verified.

| Check | Result |
| --- | --- |
| Create OCI-configured evaluation job | `202 Accepted` |
| Evaluation job reaches completed state | `200 OK`, `status.state = completed` |
| Create job-scoped post-processing job | `202 Accepted`, `status.state = pending` |
| Get job-scoped post-processing job | `200 OK` |
| Delete job-scoped post-processing job | `200 OK` |
| Get deleted job-scoped post-processing job | `404 Not Found` |
| Standalone post-processing with `eval_job` source | `400 Bad Request`, rejected |

Common request headers were `X-Tenant: test-tenant` and `X-User: test-user`; POST requests also sent `Content-Type: application/json`.

<details>
<summary>1. Create an evaluation job with dummy OCI export values</summary>

**Request:** `POST /api/v1/evaluations/jobs`

```json
{
  "name": "job-scoped-oci-dummy-source",
  "model": {
    "name": "test",
    "url": "http://localhost:9999"
  },
  "benchmarks": [
    {
      "id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "parameters": {
        "num_examples": 5
      }
    }
  ],
  "exports": {
    "oci": {
      "coordinates": {
        "oci_host": "http://localhost:5001",
        "oci_repository": "evalhub/dummy",
        "oci_tag": "test"
      }
    }
  }
}
```

**Response:** `202 Accepted`

```json
{
  "resource": {
    "id": "3834b652-4f55-4f1f-9347-a743422c93a3",
    "tenant": "test-tenant",
    "created_at": "2026-10-15T11:39:04.314157+05:30",
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
  "name": "job-scoped-oci-dummy-source",
  "model": {
    "url": "http://localhost:9999",
    "name": "test"
  },
  "benchmarks": [
    {
      "id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "parameters": {
        "num_examples": 5
      }
    }
  ],
  "exports": {
    "oci": {
      "coordinates": {
        "oci_host": "http://localhost:5001",
        "oci_repository": "evalhub/dummy",
        "oci_tag": "test"
      }
    }
  }
}
```

</details>

<details>
<summary>2. Poll the evaluation job and confirm successful completion</summary>

**Request:** `GET /api/v1/evaluations/jobs/3834b652-4f55-4f1f-9347-a743422c93a3`

**Response:** `200 OK`; `status.state` was `completed`.

```json
{
  "resource": {
    "id": "3834b652-4f55-4f1f-9347-a743422c93a3",
    "tenant": "test-tenant",
    "created_at": "2026-10-15T06:09:04Z",
    "updated_at": "2026-10-15T06:09:06Z",
    "owner": "test-user"
  },
  "status": {
    "state": "completed",
    "message": {
      "message": "Evaluation job is completed",
      "message_code": "evaluation_job_updated",
      "message_origin": "server"
    },
    "benchmarks": [
      {
        "provider_id": "lm_evaluation_harness",
        "id": "arc_easy",
        "benchmark_index": 0,
        "status": "completed",
        "completed_at": "2026-10-15T06:09:05.650227+00:00"
      }
    ]
  },
  "results": {
    "benchmarks": [
      {
        "id": "arc_easy",
        "provider_id": "lm_evaluation_harness",
        "benchmark_index": 0,
        "metrics": {
          "accuracy": 0.7
        },
        "metrics_schema": [
          {
            "name": "accuracy",
            "type": "numeric"
          }
        ],
        "artifacts": {
          "evalhub.env_card": {
            "aggregate_results": {},
            "autograder_bias": {},
            "capture_completeness": 0.12,
            "confidence_intervals": {},
            "cpu_model": "arm",
            "custom": {},
            "k8s_pod_labels": {},
            "k8s_resource_limits": {},
            "key_packages": {},
            "os_info": "macOS-26.7.1-arm64-arm-64bit",
            "per_task_results": {},
            "python_version": "3.11.14",
            "scorer_ids": []
          }
        }
      }
    ]
  },
  "name": "job-scoped-oci-dummy-source",
  "model": {
    "url": "http://localhost:9999",
    "name": "test"
  },
  "benchmarks": [
    {
      "id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "parameters": {
        "num_examples": 5
      }
    }
  ],
  "exports": {
    "oci": {
      "coordinates": {
        "oci_host": "http://localhost:5001",
        "oci_repository": "evalhub/dummy",
        "oci_tag": "test"
      }
    }
  }
}
```

</details>

<details>
<summary>3. Create job-scoped post-processing and verify pending status</summary>

**Request:** `POST /api/v1/evaluations/jobs/3834b652-4f55-4f1f-9347-a743422c93a3/post-processing`

```json
{
  "name": "job-scoped-ci",
  "operations": {
    "confidence_interval": {
      "calibration_data_ref": [
        {
          "pvc": {
            "claim_name": "calibration-data"
          },
          "data_config": {
            "format": "jsonl",
            "columns": {
              "label": "label",
              "prediction": "prediction"
            }
          }
        }
      ],
      "significance_level": 0.05
    }
  }
}
```

**Response:** `202 Accepted`; `status.state` was `pending`.

```json
{
  "resource": {
    "id": "1a003660-79b8-42ba-8378-133d0faf69b1",
    "tenant": "test-tenant",
    "created_at": "2026-10-15T11:39:14.351327+05:30",
    "owner": "test-user"
  },
  "name": "job-scoped-ci",
  "operations": {
    "confidence_interval": {
      "calibration_data_ref": [
        {
          "pvc": {
            "claim_name": "calibration-data"
          },
          "data_config": {
            "format": "jsonl",
            "columns": {
              "label": "label",
              "prediction": "prediction"
            }
          }
        }
      ],
      "significance_level": 0.05
    }
  },
  "status": {
    "state": "pending"
  }
}
```

</details>

<details>
<summary>4. Get the job-scoped post-processing resource</summary>

**Request:** `GET /api/v1/evaluations/jobs/3834b652-4f55-4f1f-9347-a743422c93a3/post-processing/1a003660-79b8-42ba-8378-133d0faf69b1`

**Response:** `200 OK`

```json
{
  "resource": {
    "id": "1a003660-79b8-42ba-8378-133d0faf69b1",
    "tenant": "test-tenant",
    "created_at": "2026-10-15T06:09:14Z",
    "updated_at": "2026-10-15T06:09:14Z",
    "owner": "test-user"
  },
  "name": "job-scoped-ci",
  "operations": {
    "confidence_interval": {
      "calibration_data_ref": [
        {
          "pvc": {
            "claim_name": "calibration-data"
          },
          "data_config": {
            "format": "jsonl",
            "columns": {
              "label": "label",
              "prediction": "prediction"
            }
          }
        }
      ],
      "significance_level": 0.05
    }
  },
  "status": {
    "state": "pending"
  }
}
```

</details>

<details>
<summary>5. Delete the job-scoped post-processing resource</summary>

**Request:** `DELETE /api/v1/evaluations/jobs/3834b652-4f55-4f1f-9347-a743422c93a3/post-processing/1a003660-79b8-42ba-8378-133d0faf69b1`

**Response:** `200 OK`, empty body.

</details>

<details>
<summary>6. Confirm GET after DELETE returns 404</summary>

**Request:** `GET /api/v1/evaluations/jobs/3834b652-4f55-4f1f-9347-a743422c93a3/post-processing/1a003660-79b8-42ba-8378-133d0faf69b1`

**Response:** `404 Not Found`

```json
{
  "message_code": "resource_not_found",
  "message": "The post-processing computation resource '1a003660-79b8-42ba-8378-133d0faf69b1' was not found.",
  "trace": "8759a9da-8abd-4e50-94f5-c403866998f7"
}
```

</details>

<details>
<summary>7. Confirm standalone post-processing rejects an eval_job results_data_ref</summary>

**Request:** `POST /api/v1/evaluations/post-processing`

```json
{
  "name": "standalone-eval-job-ref-rejected",
  "operations": {
    "confidence_interval": {
      "results_data_ref": {
        "eval_job": {
          "id": "3834b652-4f55-4f1f-9347-a743422c93a3"
        }
      },
      "calibration_data_ref": [
        {
          "pvc": {
            "claim_name": "calibration-data"
          },
          "data_config": {
            "format": "jsonl",
            "columns": {
              "label": "label",
              "prediction": "prediction"
            }
          }
        }
      ],
      "significance_level": 0.05
    }
  }
}
```

**Response:** `400 Bad Request`

```json
{
  "message_code": "request_validation_failed",
  "message": "The request validation failed: 'results_data_ref.eval_job is only available on job-scoped post-processing'. Please check the request and try again.",
  "trace": "60846654-0473-4f59-89e9-123c55671b51"
}
```

</details>
