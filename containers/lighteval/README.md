# Lighteval KFP Component

Kubeflow Pipelines component for running Lighteval evaluations on language models.

## Overview

This component integrates Lighteval (v0.13.0+) with Kubeflow Pipelines, enabling automated evaluation of language models through OpenAI-compatible API endpoints.

**Key Features**:
- Evaluates models via OpenAI-compatible APIs using Lighteval CLI
- Supports multiple benchmarks (MMLU, HellaSwag, etc.)
- Configurable few-shot evaluation
- Outputs structured metrics and detailed results as KFP artifacts
- Based on Red Hat UBI9 Python 3.12

## Container Image

- **Name**: `quay.io/evalhub/lighteval-kfp:latest`
- **Size**: ~9.12 GB
- **Base**: `registry.redhat.io/ubi9/python-312:latest`
- **Architecture**: linux/amd64

## Building

### Build Container

```bash
cd containers/lighteval
podman build --arch amd64 -t quay.io/evalhub/lighteval-kfp:latest .
```

Or use the build script:

```bash
cd containers/lighteval
bash build.sh
```

### Build with Push to Registry

```bash
PUSH=true VERSION=v1.0.0 bash build.sh
```

## Usage

### Component Interface

**Inputs**:
- `--model_url` (required): Model endpoint URL (OpenAI-compatible API)
- `--model_name` (required): Model identifier
- `--benchmark` (required): Benchmark name
- `--tasks` (required): JSON array of tasks to evaluate
- `--num_fewshot` (optional, default: 0): Number of few-shot examples
- `--limit` (optional): Limit number of samples
- `--batch_size` (optional, default: 1): Batch size for evaluation
- `--output_metrics` (required): Path to write metrics artifact
- `--output_results` (required): Path to write results artifact

**Outputs**:
- Metrics artifact (JSON): Flattened metrics dictionary
- Results artifact (JSON): Detailed evaluation results

### Local Testing

Test the component help:

```bash
podman run --rm quay.io/evalhub/lighteval-kfp:latest --help
```

Run an evaluation (example with mock endpoint):

```bash
podman run --rm \
  quay.io/evalhub/lighteval-kfp:latest \
  --model_url https://api.openai.com/v1 \
  --model_name gpt-3.5-turbo \
  --benchmark mmlu \
  --tasks '["hellaswag", "mmlu:abstract_algebra"]' \
  --num_fewshot 5 \
  --limit 10 \
  --batch_size 1 \
  --output_metrics /tmp/metrics.json \
  --output_results /tmp/results.json
```

### KFP Pipeline Integration

Use the provided test pipeline:

```bash
# Compile the pipeline
python test_kfp_pipeline.py

# This generates lighteval_test_pipeline.yaml
```

Upload the generated YAML to your KFP instance and run with appropriate parameters.

## Implementation Details

### Lighteval CLI Integration

The component uses the Lighteval CLI (v0.13.0+) instead of the deprecated Python API:

```bash
lighteval endpoint litellm <model_args> <tasks> [options]
```

**Model Configuration**:
- Format: `model_name={name},base_url={url}`
- Example: `model_name=gpt-3.5-turbo,base_url=https://api.openai.com/v1`

**Task Format**:
- Format: `task|fewshot_count`
- Example: `hellaswag|5,mmlu:abstract_algebra|0`

### Supported Backends

Lighteval supports multiple endpoint backends:
- `litellm` - OpenAI-compatible APIs via LiteLLM (used by this component)
- `tgi` - Text Generation Inference
- `inference-endpoint` - HuggingFace Inference Endpoints
- `inference-providers` - HuggingFace Inference Providers

### Component Architecture

```
lighteval_component.py
├── parse_args()           # Parse command-line arguments
├── run_lighteval_evaluation()  # Execute Lighteval CLI
│   ├── Format model args
│   ├── Format task strings
│   ├── Run subprocess
│   └── Parse results
├── extract_metrics()      # Flatten metrics from results
└── write_kfp_artifacts()  # Write output artifacts
```

## Dependencies

**Python Packages**:
- `lighteval>=0.13.0`
- `kfp>=2.7.0`
- `openai>=1.0.0`
- `litellm>=1.0.0`
- `transformers>=4.35.0`
- `accelerate>=0.25.0`
- `torch>=2.1.0`

## Files

- `Dockerfile` - Container image definition
- `lighteval_component.py` - Component implementation
- `run_component.sh` - Entrypoint script (deprecated, uses direct Python)
- `build.sh` - Build automation script
- `test_kfp_pipeline.py` - KFP pipeline test script
- `lighteval_test_pipeline.yaml` - Compiled test pipeline

## Recent Updates

### CLI Migration (2025-12-02)

Migrated from Lighteval Python API to CLI:

**Before**:
```python
from lighteval.main_accelerate import main as lighteval_main
results = lighteval_main(model_config=..., tasks=...)
```

**After**:
```python
cmd = ["lighteval", "endpoint", "litellm", model_args, tasks_arg, ...]
result = subprocess.run(cmd, capture_output=True, text=True)
```

**Reasons**:
- Lighteval 0.13.0 removed the `main()` function
- CLI provides better backend support
- Simplified model configuration
- Better alignment with Lighteval's direction

## Troubleshooting

### Container Build Issues

**Problem**: Permission denied errors during build

**Solution**: Ensure files have correct permissions before copying:
```bash
chmod 644 lighteval_component.py run_component.sh
```

**Problem**: Missing dependencies

**Solution**: Rebuild without cache:
```bash
podman build --no-cache -t quay.io/evalhub/lighteval-kfp:latest .
```

### Runtime Issues

**Problem**: Lighteval CLI not found

**Solution**: Ensure lighteval is installed in the container:
```bash
podman run --rm quay.io/evalhub/lighteval-kfp:latest which lighteval
```

**Problem**: LiteLLM import error

**Solution**: Verify litellm is installed:
```bash
podman run --rm quay.io/evalhub/lighteval-kfp:latest python -c "import litellm"
```

**Problem**: Results file not found

**Solution**: Check Lighteval output directory structure. The component searches:
1. `output_dir/results/**/results_*.json`
2. `output_dir/**/results.json`

## Examples

### Minimal Example

```bash
podman run --rm quay.io/evalhub/lighteval-kfp:latest \
  --model_url http://localhost:8000/v1 \
  --model_name local-model \
  --benchmark test \
  --tasks '["hellaswag"]' \
  --output_metrics /tmp/metrics.json \
  --output_results /tmp/results.json
```

### Production Example

```bash
podman run --rm \
  -v $PWD/outputs:/outputs \
  quay.io/evalhub/lighteval-kfp:latest \
  --model_url https://api.openai.com/v1 \
  --model_name gpt-4 \
  --benchmark mmlu \
  --tasks '["mmlu:abstract_algebra", "mmlu:anatomy", "mmlu:astronomy"]' \
  --num_fewshot 5 \
  --limit 100 \
  --batch_size 1 \
  --output_metrics /outputs/metrics.json \
  --output_results /outputs/results.json
```

## Contributing

When adding new features:
1. Update `lighteval_component.py`
2. Update `Dockerfile` if dependencies change
3. Test with `test_kfp_pipeline.py`
4. Update this README
5. Rebuild the container

## References

- [Lighteval GitHub](https://github.com/huggingface/lighteval)
- [Kubeflow Pipelines](https://www.kubeflow.org/docs/components/pipelines/)
- [LiteLLM](https://docs.litellm.ai/)
- [Eval Hub](../../README.md)

## License

See main repository LICENSE file.
