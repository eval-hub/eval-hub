# MLflow Artifact Logging Guide

## Overview

This document explains how benchmark adapters must log artifacts (reports, plots, model outputs, etc.) to MLflow so they appear in the MLflow UI and are accessible via the EvalHub API.

## Problem

When evaluation pipelines execute, they may produce valuable artifacts like:
- HTML/JSON reports (e.g., Garak security reports)
- Plots and visualizations
- Model outputs and predictions
- Benchmark-specific analysis files

If adapters don't explicitly log these artifacts to MLflow, they remain only in the pipeline pod's filesystem and are **lost when the pod terminates** or are only available as pipeline artifacts (not in MLflow).

## Required: Log Artifacts to MLflow

Adapters **must** call MLflow's artifact logging functions to upload files to MLflow's artifact store.

### Basic Artifact Logging

```python
import mlflow

# Start an MLflow run (or use existing run context)
with mlflow.start_run(experiment_id=mlflow_experiment_id) as run:
    mlflow_run_id = run.info.run_id

    # Run the benchmark
    results = run_benchmark()

    # Log metrics
    mlflow.log_metrics({
        "accuracy": results["accuracy"],
        "latency_ms": results["latency"]
    })

    # Log artifacts (files/directories)
    mlflow.log_artifact("/tmp/report.html", "reports")
    mlflow.log_artifact("/tmp/results.json", "results")

    # Log entire directory
    if os.path.exists("/tmp/garak_output"):
        mlflow.log_artifacts("/tmp/garak_output", "garak")
```

### Garak Adapter Example

For Garak security evaluation:

```python
import mlflow
import subprocess
import os

# Read job spec
mlflow_experiment_id = job_spec.get("mlflow_experiment_id")

# Start MLflow run
with mlflow.start_run(experiment_id=mlflow_experiment_id) as run:
    mlflow_run_id = run.info.run_id

    # Run Garak
    output_dir = "/tmp/garak_output"
    subprocess.run([
        "garak",
        "--model_type", "openai",
        "--model_name", job_spec["model"]["name"],
        "--endpoint", job_spec["model"]["url"],
        "--report_dir", output_dir
    ])

    # ✅ CRITICAL: Log Garak artifacts to MLflow
    if os.path.exists(output_dir):
        mlflow.log_artifacts(output_dir, artifact_path="garak_reports")
        print(f"✅ Logged Garak artifacts to MLflow run {mlflow_run_id}")
    else:
        print(f"⚠️  Garak output directory not found: {output_dir}")

    # Find and log the HTML report specifically
    html_report = os.path.join(output_dir, "report.html")
    if os.path.exists(html_report):
        mlflow.log_artifact(html_report, artifact_path="reports")

    # Log metrics if available
    results_json = os.path.join(output_dir, "results.json")
    if os.path.exists(results_json):
        with open(results_json) as f:
            results = json.load(f)
            if "metrics" in results:
                mlflow.log_metrics(results["metrics"])
```

## Artifact Organization

### Recommended Directory Structure

Organize artifacts logically in MLflow:

```
artifacts/
├── reports/
│   ├── report.html         # Main HTML report
│   └── summary.json        # Summary JSON
├── results/
│   ├── raw_outputs.jsonl   # Raw model outputs
│   └── processed.csv       # Processed results
├── plots/
│   ├── accuracy_plot.png
│   └── latency_histogram.png
└── garak/
    ├── garak_report.html
    ├── vulnerabilities.json
    └── test_details.jsonl
```

### Using artifact_path

The `artifact_path` parameter organizes files:

```python
# Creates: artifacts/reports/report.html
mlflow.log_artifact("/tmp/report.html", artifact_path="reports")

# Creates: artifacts/garak/garak_report.html
mlflow.log_artifact("/tmp/garak_report.html", artifact_path="garak")

# Logs entire directory: artifacts/garak/*
mlflow.log_artifacts("/tmp/garak_output", artifact_path="garak")
```

## Artifact Types

### 1. Text Reports

```python
# HTML reports
mlflow.log_artifact("report.html", "reports")

# JSON results
mlflow.log_artifact("results.json", "results")

# Markdown documentation
mlflow.log_artifact("README.md", "docs")
```

### 2. Plots and Visualizations

```python
import matplotlib.pyplot as plt

# Create plot
plt.figure()
plt.plot(accuracies)
plt.savefig("/tmp/accuracy_plot.png")

# Log to MLflow
mlflow.log_artifact("/tmp/accuracy_plot.png", "plots")

# Or use mlflow.log_figure()
mlflow.log_figure(plt.gcf(), "accuracy_plot.png")
```

### 3. Model Outputs

```python
# Log model predictions
with open("/tmp/predictions.jsonl", "w") as f:
    for pred in predictions:
        f.write(json.dumps(pred) + "\n")

mlflow.log_artifact("/tmp/predictions.jsonl", "outputs")
```

### 4. Benchmark-Specific Files

```python
# Log custom benchmark files
mlflow.log_artifacts("/tmp/benchmark_specific", "benchmark_data")
```

## MLflow Environment Variables

Ensure these environment variables are set (EvalHub runtime provides them):

```python
import os

# Verify MLflow configuration
mlflow_tracking_uri = os.getenv("MLFLOW_TRACKING_URI")
if not mlflow_tracking_uri:
    print("⚠️  MLFLOW_TRACKING_URI not set - artifacts won't be logged!")

print(f"MLflow Tracking URI: {mlflow_tracking_uri}")
print(f"MLflow Token Path: {os.getenv('MLFLOW_TRACKING_TOKEN_PATH')}")
print(f"MLflow Workspace: {os.getenv('MLFLOW_WORKSPACE')}")
```

## Verification

### Check Artifacts in MLflow UI

1. Open MLflow UI (link provided in evaluation job results)
2. Navigate to your experiment
3. Click on the run (identified by `mlflow_run_id`)
4. Go to "Artifacts" tab
5. Verify all expected files are present

### Check via API

```bash
# Get evaluation job results
curl -H "Authorization: Bearer $TOKEN" \
  https://evalhub/api/v1/evaluations/jobs/$JOB_ID | jq '.results'
```

The response should include artifacts:

```json
{
  "results": {
    "benchmarks": [
      {
        "id": "garak",
        "mlflow_run_id": "abc123...",
        "artifacts": {
          "report_html": "artifacts/reports/report.html",
          "results_json": "artifacts/results/results.json"
        }
      }
    ],
    "mlflow_experiment_url": "https://mlflow/experiments/123"
  }
}
```

## Common Issues

### Issue 1: Artifacts Not Appearing in MLflow

**Symptom**: Pipeline produces files, but they don't appear in MLflow UI.

**Cause**: Adapter didn't call `mlflow.log_artifact()` or `mlflow.log_artifacts()`.

**Solution**:
```python
# ❌ WRONG: Just creating files
with open("/tmp/report.html", "w") as f:
    f.write(report_content)
# File exists in pod but not in MLflow!

# ✅ CORRECT: Log to MLflow
with open("/tmp/report.html", "w") as f:
    f.write(report_content)
mlflow.log_artifact("/tmp/report.html", "reports")
```

### Issue 2: MLflow Run Not Started

**Symptom**: `mlflow.log_artifact()` fails with "No active run".

**Cause**: Forgot to start an MLflow run.

**Solution**:
```python
# ❌ WRONG: Logging without starting run
mlflow.log_artifact("/tmp/report.html")  # Error: No active run!

# ✅ CORRECT: Start run first
with mlflow.start_run(experiment_id=experiment_id) as run:
    mlflow.log_artifact("/tmp/report.html", "reports")
```

### Issue 3: Wrong Experiment ID

**Symptom**: Artifacts logged to wrong experiment or create new experiment.

**Cause**: Not using the `mlflow_experiment_id` from job spec.

**Solution**:
```python
# ❌ WRONG: Using experiment name instead of ID
mlflow.set_experiment("my-experiment")

# ✅ CORRECT: Use experiment ID from job spec
experiment_id = job_spec.get("mlflow_experiment_id")
with mlflow.start_run(experiment_id=experiment_id) as run:
    # ... log artifacts
```

### Issue 4: Files Don't Exist

**Symptom**: `mlflow.log_artifact()` fails with "File not found".

**Cause**: File path is incorrect or file wasn't created.

**Solution**:
```python
import os

artifact_path = "/tmp/report.html"

# Check if file exists before logging
if os.path.exists(artifact_path):
    mlflow.log_artifact(artifact_path, "reports")
    print(f"✅ Logged {artifact_path}")
else:
    print(f"⚠️  Artifact not found: {artifact_path}")
    print(f"Available files in /tmp: {os.listdir('/tmp')}")
```

## Best Practices

### 1. Always Log Artifacts

**Every adapter should log artifacts**, even if it's just a simple results.json:

```python
import json

results = {
    "benchmark_id": job_spec["benchmark_id"],
    "metrics": metrics,
    "timestamp": datetime.now().isoformat()
}

with open("/tmp/results.json", "w") as f:
    json.dump(results, f, indent=2)

mlflow.log_artifact("/tmp/results.json", "results")
```

### 2. Log Early and Often

Don't wait until the end - log artifacts as they're created:

```python
with mlflow.start_run(experiment_id=experiment_id) as run:
    # Log interim results
    mlflow.log_artifact("/tmp/phase1_results.json", "interim")

    # Continue processing
    phase2_results = process_phase2()
    mlflow.log_artifact("/tmp/phase2_results.json", "interim")

    # Log final results
    mlflow.log_artifact("/tmp/final_report.html", "reports")
```

### 3. Include Metadata

Log metadata alongside artifacts:

```python
# Log configuration
mlflow.log_params({
    "benchmark_id": job_spec["benchmark_id"],
    "model_name": job_spec["model"]["name"],
    "num_examples": job_spec.get("num_examples", "all")
})

# Log metrics
mlflow.log_metrics(results["metrics"])

# Log artifacts
mlflow.log_artifact("/tmp/report.html", "reports")
```

### 4. Handle Errors Gracefully

```python
try:
    mlflow.log_artifact("/tmp/report.html", "reports")
    print("✅ Successfully logged report to MLflow")
except Exception as e:
    print(f"⚠️  Failed to log artifact to MLflow: {e}")
    # Don't fail the entire job if MLflow logging fails
```

## Testing

### Local Testing

```python
import mlflow
import tempfile

# Use local MLflow tracking
mlflow.set_tracking_uri("file:///tmp/mlruns")

with mlflow.start_run():
    # Create test artifact
    with open("/tmp/test_report.html", "w") as f:
        f.write("<h1>Test Report</h1>")

    # Log it
    mlflow.log_artifact("/tmp/test_report.html", "reports")

    print(f"✅ Test artifact logged to {mlflow.get_artifact_uri()}")
```

### Integration Testing

Run a full evaluation job and verify artifacts:

```bash
# Submit job
JOB_ID=$(curl -X POST ... | jq -r '.resource.id')

# Wait for completion
# ...

# Get results
MLFLOW_RUN_ID=$(curl https://evalhub/api/v1/evaluations/jobs/$JOB_ID | \
  jq -r '.results.benchmarks[0].mlflow_run_id')

# Verify artifacts exist in MLflow
mlflow artifacts list --run-id $MLFLOW_RUN_ID
```

## Related Issues

- RHOAIENG-54539: Artifacts are not written to EvalHub/MLFlow
- RHOAIENG-54869: mlflow_run_id missing from GET /evaluations/jobs/{id} (see mlflow-run-id-integration.md)

## References

- [MLflow Artifact Logging](https://mlflow.org/docs/latest/tracking.html#logging-functions)
- [MLflow Python API](https://mlflow.org/docs/latest/python_api/mlflow.html)
- [MLflow Run Context](https://mlflow.org/docs/latest/tracking.html#automatic-logging)
