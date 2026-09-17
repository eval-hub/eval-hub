# MLFlow Integration

Concise guide for configuring MLFlow integration and understanding experiment tracking in the Eval Hub.

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `MLFLOW_TRACKING_URI` | MLFlow server URL | `http://localhost:5000` | Yes |
| `MLFLOW_CA_CERT_PATH` | PEM CA bundle used to verify the MLflow server certificate when the tracking URI is `https://` | (system CA roots) | No |

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

### Certificates

When `MLFLOW_TRACKING_URI` uses `https://`, EvalHub always verifies the MLflow server certificate. There is no skip-verify option. Plain `http://` URIs do not use TLS.

On OpenShift, the TrustyAI Service Operator builds a merged CA bundle ConfigMap named `{evalhub-instance}-mlflow-ca-bundle` (key `ca-bundle.crt`). That bundle is mounted into the EvalHub API at `/etc/evalhub/mlflow-ca/ca-bundle.crt` and exposed as `MLFLOW_CA_CERT_PATH`. The same ConfigMap is copied into tenant namespaces so evaluation job sidecars and adapters use the same trust store.

The operator concatenates these sources on every reconcile (CA rotations in the sources are picked up automatically):

| Source | ConfigMap (EvalHub instance namespace) | Trusts |
|--------|----------------------------------------|--------|
| ODH trusted CA bundle | `odh-trusted-ca-bundle` (`ca-bundle.crt`, `odh-ca-bundle.crt`) | Public/system CAs plus any CAs from DSCI `trustedCABundle` (and the cluster-wide proxy trusted CA) |
| OpenShift service-serving CA | `openshift-service-ca.crt` (`service-ca.crt`) | In-cluster `*.svc.cluster.local` Service certificates |
| Optional user CA | ConfigMap named by `spec.mlflow.tls.caBundle` | A custom CA that signs the MLflow Route or Ingress certificate |

For local or Podman deployments, set `MLFLOW_CA_CERT_PATH` to a PEM file yourself. If it is unset, the process uses the system CA roots.

#### Internal Service hostname

`MLFLOW_TRACKING_URI` is the in-cluster Service DNS name, for example `https://mlflow.mlflow.svc.cluster.local:443`.

- **Certificate used:** the OpenShift service-serving certificate on the MLflow Service. The certificate SAN matches the Service hostname (`*.svc.cluster.local`).
- **Where the CA comes from:** `openshift-service-ca.crt` (`service-ca.crt`), merged into `{evalhub-instance}-mlflow-ca-bundle`.
- **Cluster admin:** no extra CA configuration. Do not set `spec.mlflow.tls.caBundle`. Confirm `MLFLOW_TRACKING_URI` uses the Service hostname that matches the serving certificate.

#### Public hostname with the default ingress certificate

`MLFLOW_TRACKING_URI` is the public Route or Ingress hostname, and MLflow presents the cluster's default ingress certificate.

- **Certificate used:** the cluster default Ingress/Route certificate (the IngressController default or wildcard certificate).
- **Where the CA comes from:** `odh-trusted-ca-bundle` in the EvalHub namespace. That ConfigMap holds public/system CAs plus any CAs configured on DSCI `trustedCABundle`.
- **Cluster admin:** no EvalHub-specific CA reference is required (`spec.mlflow.tls.caBundle` can be omitted). If the default ingress certificate is not publicly trusted (typical for an OpenShift-generated wildcard), add its issuing CA to DSCI `spec.trustedCABundle.customCABundle` or the cluster-wide proxy trusted CA so it appears in `odh-trusted-ca-bundle`. The operator then includes it in the merged bundle automatically.

#### Public hostname with a custom certificate

`MLFLOW_TRACKING_URI` is the public hostname, and the MLflow Route or Ingress presents a custom TLS certificate (not the cluster default).

- **Certificate used:** the custom certificate configured on the MLflow Route or Ingress.
- **Where the CA comes from:** a ConfigMap the cluster admin creates in the EvalHub instance namespace. The operator merges that PEM into `{evalhub-instance}-mlflow-ca-bundle` together with the service-serving CA and the ODH trusted CA bundle.
- **Cluster admin:**
  1. Create a ConfigMap in the EvalHub instance namespace whose data contains the PEM-encoded CA (and any intermediates) that signed the custom certificate. The default key is `ca-bundle.crt`.
  2. Point the EvalHub CR at that ConfigMap:

```yaml
spec:
  mlflow:
    tls:
      caBundle:
        name: mlflow-custom-ca
        key: ca-bundle.crt   # optional; default is ca-bundle.crt
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
