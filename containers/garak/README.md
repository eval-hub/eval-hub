# Garak KFP Container

LLM security vulnerability scanner for Kubeflow Pipelines, powered by [Garak](https://github.com/NVIDIA/garak).

## Overview

This container provides a KFP component for running Garak security scans on LLM endpoints. It uses **KFP's native artifact system** for result storage - no explicit S3 configuration needed.

Features:
- **Probe-based scanning**: Run specific vulnerability probes (e.g., `dan`, `encoding`, `promptinject`)
- **Taxonomy-based scanning**: Use OWASP LLM Top 10 or AVID taxonomy filters
- **KFP Native Artifacts**: Results written directly to KFP output paths

## Building

```bash
# Build with default tag
./build.sh

# Build with custom tag
IMAGE_TAG=v1.0.0 ./build.sh

# Build and push
PUSH_IMAGE=true ./build.sh
```

## Usage

### Command-line Arguments

| Argument | Required | Default | Description |
|----------|----------|---------|-------------|
| `--model_url` | Yes | - | Model endpoint URL (OpenAI-compatible) |
| `--model_name` | Yes | - | Model identifier |
| `--api_key` | No | - | API key for model endpoint |
| `--benchmark_id` | Yes | - | Benchmark ID (e.g., `quick`, `owasp_llm_top10`) |
| `--probes` | No | `[]` | JSON array of Garak probes |
| `--taxonomy_filters` | No | `[]` | JSON array of taxonomy filters |
| `--eval_threshold` | No | `0.5` | Vulnerability threshold (0-1) |
| `--timeout_seconds` | No | `3600` | Maximum execution time |
| `--parallel_attempts` | No | `8` | Parallel probe attempts |
| `--generations` | No | `1` | Generations per probe |
| `--output_metrics` | Yes | - | Output path for metrics JSON (KFP artifact) |
| `--output_results` | Yes | - | Output path for results JSON (KFP artifact) |

### Example

```bash
docker run --rm \
    quay.io/trustyai/garak-kfp:latest \
    --model_url "https://your-model-endpoint/v1" \
    --model_name "gpt-4" \
    --benchmark_id "quick" \
    --probes '["realtoxicityprompts.RTPProfanity"]' \
    --output_metrics /tmp/metrics.json \
    --output_results /tmp/results.json
```

## Available Benchmarks

### Quick Testing
- **quick**: Fast 1-probe scan (~10 minutes)
- **standard**: 5-probe scan covering common vectors

### Comprehensive
- **owasp_llm_top10**: OWASP LLM Top 10 vulnerabilities
- **avid_security**: AVID Security taxonomy
- **avid_ethics**: AVID Ethics taxonomy
- **avid_performance**: AVID Performance taxonomy

### Individual Probes
- **toxicity**: Toxic content generation
- **bias_detection**: Bias evaluation
- **prompt_injection**: Prompt injection attacks
- **jailbreak**: Jailbreak attempts
- **pii_leakage**: PII leakage tests

## Output Format

### Metrics JSON (output_metrics)

```json
{
    "total_attempts": 100,
    "vulnerable_attempts": 15,
    "attack_success_rate": 15.0,
    "security_score": 85.0,
    "dan_DAN_attack_success_rate": 15.0
}
```

### Results JSON (output_results)

```json
{
    "generations": [
        {
            "probe": "dan.DAN",
            "detector": "mitigation.MitigationBypass",
            "passed": false,
            "score": 0.0,
            "vulnerable": true,
            "prompt": "...",
            "output": "..."
        }
    ],
    "scores": {
        "dan.DAN": {
            "attempts": 100,
            "passed": 85,
            "failed": 15,
            "attack_success_rate": 15.0,
            "detectors": {...}
        }
    },
    "summary": {
        "total_attempts": 100,
        "vulnerable_attempts": 15,
        "attack_success_rate": 15.0,
        "security_score": 85.0
    }
}
```

## Integration with eval-hub

This container is designed to be used with eval-hub's KFP executor. The `GarakAdapter` transforms eval-hub evaluation requests into the appropriate command-line arguments for this component.

```python
# In eval-hub
from eval_hub.adapters.frameworks.garak import GarakAdapter

adapter = GarakAdapter()
kfp_args = adapter.transform_to_kfp_args(context, backend_config)
```

## References

- [Garak Documentation](https://github.com/NVIDIA/garak)
- [OWASP LLM Top 10](https://genai.owasp.org/llm-top-10/)
- [AVID Taxonomy](https://avidml.org/)
