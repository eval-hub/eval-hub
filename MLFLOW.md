# MLFlow Integration

Concise guide for configuring MLFlow integration and understanding experiment tracking in the Eval Hub.

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `MLFLOW_TRACKING_URI` | MLFlow server URL | `http://localhost:5000` | Yes |

### Deployment Configuration

**Podman/Container:**
```bash
podman run -p 8080:8080 \
  -e MLFLOW_TRACKING_URI=http://mlflow:5000 \
  eval-hub:latest
```

**Kubernetes/OpenShift:**
```yaml
env:
  - name: MLFLOW_TRACKING_URI
    value: "http://mlflow-service:5000"
```

## Experiment Configuration

### ExperimentConfig Schema

```json
{
  "experiment": {
    "name": "string",
    "tags": [
      {"key": "string", "value": "string"}
    ]
  }
}
```


## Payload Examples

### Single Benchmark Evaluation

```json
{
  "model": {
    "url": "http://vllm:8000/v1",
    "name": "meta-llama/llama-3.1-8b"
  },
  "benchmarks": [
    {
      "id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "parameters": {"num_fewshot": 0}
    }
  ],
  "experiment": {
    "name": "arc-easy-evaluation",
    "tags": [
      {"key": "environment", "value": "testing"},
      {"key": "model_family", "value": "llama-3.1"}
    ]
  }
}
```

### Multi-Provider Evaluation

```json
{
  "model": {
    "url": "http://vllm:8000/v1",
    "name": "meta-llama/llama-3.1-8b"
  },
  "benchmarks": [
    {
      "id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "parameters": {"num_fewshot": 0}
    },
    {
      "id": "hellaswag",
      "provider_id": "lighteval",
      "parameters": {"num_fewshot": 0}
    }
  ],
  "experiment": {
    "name": "comprehensive-evaluation",
    "tags": [
      {"key": "evaluation_type", "value": "comprehensive"},
      {"key": "model_version", "value": "v1.0"}
    ]
  }
}
```

### Collection Evaluation

```json
{
  "model": {
    "url": "http://vllm:8000/v1",
    "name": "meta-llama/llama-3.1-8b"
  },
  "experiment": {
    "name": "healthcare-certification",
    "tags": [
      {"key": "environment", "value": "production"},
      {"key": "compliance", "value": "healthcare"},
      {"key": "certification_level", "value": "grade-a"}
    ]
  }
}
```

## MLFlow Experiment Structure

### Experiment Metadata
- **Name**: `{prefix}_{experiment.name}` or auto-generated
- **Tags**: Direct mapping from `experiment.tags`
- **Description**: Auto-generated based on benchmarks and model

### Run Organization
- One MLFlow run per evaluation request
- Run tags include model configuration and benchmark details
- Artifacts include detailed results and logs

### Result Storage
- **Metrics**: Benchmark scores and performance data
- **Parameters**: Model configuration and benchmark settings
- **Artifacts**: Detailed result files and execution logs
- **Tags**: Experiment tags plus auto-generated metadata

## Integration Examples

### CI/CD Pipeline
```bash
curl -X POST "http://eval-hub:8080/api/v1/evaluations/jobs" \
  -H "Content-Type: application/json" \
  -d '{
    "model": {"url": "http://vllm:8000/v1", "name": "my-model:v1.0"},
    "benchmarks": [{"id": "arc_easy", "provider_id": "lm_evaluation_harness"}],
    "experiment": {
      "name": "ci-evaluation-'$BUILD_ID'",
      "tags": [
        {"key": "build_id", "value": "'$BUILD_ID'"},
        {"key": "branch", "value": "'$GIT_BRANCH'"},
        {"key": "commit", "value": "'$GIT_COMMIT'"}
      ]
    }
  }'
```

### Production Monitoring
```json
{
  "experiment": {
    "name": "production-monitoring-2025-01",
    "tags": [
      {"key": "environment", "value": "production"},
      {"key": "monitoring", "value": "true"},
      {"key": "alert_threshold", "value": "0.85"},
      {"key": "team", "value": "ml-ops"}
    ]
  }
}
```

## Troubleshooting

### Common Issues

**Connection Errors:**
- Verify `MLFLOW_TRACKING_URI` is accessible from Eval Hub
- Check network connectivity and firewall rules
- Ensure MLFlow server is running and healthy

**Experiment Creation Failures:**
- Check MLFlow server disk space
- Verify experiment naming doesn't conflict with existing experiments
- Ensure tags contain only valid characters (alphanumeric, _, -, .)

**Missing Results:**
- Verify MLFlow run completed successfully
- Check evaluation request completed without errors
- Review MLFlow UI for run details and artifacts
