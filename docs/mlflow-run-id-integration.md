# MLflow Run ID Integration Guide

## Overview

This document explains how benchmark adapters should integrate with MLflow to ensure that `mlflow_run_id` is properly populated in evaluation job results.

## Background

When an evaluation job is created, the EvalHub server creates an MLflow experiment and stores the `mlflow_experiment_id` in the `EvaluationJobResource`. This experiment ID is now passed to benchmark adapters via the `job.json` file.

## Adapter Requirements

### 1. Read MLflow Experiment ID from Job Spec

The job spec (`job.json`) now includes the `mlflow_experiment_id` field:

```json
{
  "id": "job-uuid",
  "provider_id": "lm_evaluation_harness",
  "benchmark_id": "arc_easy",
  "benchmark_index": 0,
  "model": { ... },
  "experiment_name": "my-experiment",
  "mlflow_experiment_id": "1234567890",
  "callback_url": "https://evalhub/api/v1/..."
}
```

### 2. Create an MLflow Run

When the adapter starts running the benchmark, it **must** create an MLflow run within the provided experiment:

```python
import mlflow

# Read from job spec
mlflow_experiment_id = job_spec.get("mlflow_experiment_id")

if mlflow_experiment_id:
    # Create a run within the experiment
    with mlflow.start_run(experiment_id=mlflow_experiment_id) as run:
        mlflow_run_id = run.info.run_id

        # Run the benchmark
        results = run_benchmark()

        # Log metrics and artifacts to MLflow
        mlflow.log_metrics(results["metrics"])
        mlflow.log_artifacts(results["artifacts"])
```

### 3. Report MLflow Run ID in Status Updates

When the adapter sends status updates back to the EvalHub server (via the callback URL), it **must** include the `mlflow_run_id` in the `BenchmarkStatusEvent`:

```python
status_event = {
    "id": job_spec["benchmark_id"],
    "provider_id": job_spec["provider_id"],
    "benchmark_index": job_spec["benchmark_index"],
    "status": "completed",
    "mlflow_run_id": mlflow_run_id,  # <-- REQUIRED
    "metrics": { ... },
    "artifacts": { ... }
}

# Send to callback URL
requests.post(
    job_spec["callback_url"],
    json={"benchmark_status": status_event}
)
```

## Data Flow

1. **Job Creation**: EvalHub creates MLflow experiment → stores `mlflow_experiment_id`
2. **Job Spec Generation**: Runtime includes `mlflow_experiment_id` in `job.json`
3. **Adapter Execution**: Adapter reads experiment ID, creates run, gets `mlflow_run_id`
4. **Status Update**: Adapter reports `mlflow_run_id` back to EvalHub
5. **Result Storage**: EvalHub stores `mlflow_run_id` in `BenchmarkResult`
6. **API Response**: `GET /evaluations/jobs/{id}` returns `mlflow_run_id` in `data.results.benchmarks[*].mlflow_run_id`

## Benefits

- Enables dashboard to embed MLflow UI for specific runs
- Allows direct linking to MLflow run details
- Provides complete traceability from evaluation job to MLflow artifacts

## Migration for Existing Adapters

If you're updating an existing adapter:

1. Add code to read `mlflow_experiment_id` from job spec
2. Create MLflow run using the experiment ID
3. Include `mlflow_run_id` in all status update events (especially terminal states like `completed` or `failed`)

## Testing

To verify the fix:

1. Submit an evaluation job with MLflow configured
2. Wait for completion
3. Call `GET /evaluations/jobs/{id}`
4. Verify `data.results.benchmarks[*].mlflow_run_id` is populated

Example response:
```json
{
  "results": {
    "benchmarks": [
      {
        "id": "arc_easy",
        "provider_id": "lm_evaluation_harness",
        "benchmark_index": 0,
        "mlflow_run_id": "abcd1234efgh5678",  // <-- Should be present
        "metrics": { ... }
      }
    ]
  }
}
```

## Related Issues

- RHOAIENG-54869: mlflow_run_id missing from GET /evaluations/jobs/{id} benchmark results response
