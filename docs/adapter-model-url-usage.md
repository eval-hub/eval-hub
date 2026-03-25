# Adapter Model URL Usage Guide

## Overview

This document explains how benchmark adapters **must** use the `model.url` field from the job specification to connect to models, rather than downloading models from HuggingFace or other registries.

## Problem

Adapters were incorrectly attempting to download models from HuggingFace even when a custom `model.url` was provided in the evaluation job request. This caused jobs to fail when:
- Models were served locally (e.g., via vLLM, TGIS, or other inference servers)
- Models were gated on HuggingFace but accessible via local endpoints
- Network policies prevented external downloads

## Solution

Adapters **must** respect the `model.url` field and use it to connect to the model inference endpoint.

## Job Spec Structure

The job spec (`job.json`) includes the model configuration:

```json
{
  "id": "job-uuid",
  "provider_id": "lm_evaluation_harness",
  "benchmark_id": "arc_easy",
  "model": {
    "url": "http://llama-3-3-70b-instruct-predictor.llm-models.svc.cluster.local:8000/v1",
    "name": "meta-llama/Llama-3.3-70B-Instruct",
    "parameters": {
      "temperature": 0.7
    }
  },
  ...
}
```

### Model Fields

| Field | Required | Description |
|-------|----------|-------------|
| `url` | Yes | The HTTP(S) endpoint for the model inference service (OpenAI-compatible API) |
| `name` | Yes | The model identifier (used for logging, not for loading) |
| `auth` | No | Authentication configuration (if required by the model endpoint) |
| `parameters` | No | Model-specific parameters (temperature, top_p, etc.) |

## Adapter Implementation Requirements

### 1. DO NOT Download Models

Adapters **must not** attempt to download models from HuggingFace, ModelHub, or any other model registry based on `model.name`.

**❌ WRONG:**
```python
from transformers import AutoModelForCausalLM

# This will try to download from HuggingFace!
model = AutoModelForCausalLM.from_pretrained(job_spec["model"]["name"])
```

### 2. Use the Model URL

Adapters **must** connect to the provided `model.url` endpoint using an OpenAI-compatible client.

**✅ CORRECT:**
```python
from openai import OpenAI

# Use the provided URL
client = OpenAI(
    base_url=job_spec["model"]["url"],
    api_key=os.getenv("MODEL_API_KEY", "dummy")  # or from job_spec["model"]["auth"]
)

# Now use the client for inference
response = client.completions.create(
    model=job_spec["model"]["name"],  # Some endpoints require this
    prompt="What is AI?",
    max_tokens=100
)
```

### 3. LM Evaluation Harness Example

For LM Evaluation Harness adapters:

```python
from lm_eval import simple_evaluate
from lm_eval.models import get_model

# Read from job spec
model_url = job_spec["model"]["url"]
model_name = job_spec["model"]["name"]

# Configure model to use the custom endpoint
model = get_model("openai-chat-completions").create_from_arg_string(
    f"model={model_name},base_url={model_url}"
)

# Run evaluation
results = simple_evaluate(
    model=model,
    tasks=["arc_easy"],
    ...
)
```

### 4. OpenAI-Compatible API Compliance

The `model.url` endpoint is expected to be OpenAI-compatible, supporting endpoints like:
- `/v1/completions`
- `/v1/chat/completions`
- `/v1/embeddings` (if applicable)

Most modern inference servers (vLLM, TGI, llama.cpp, etc.) support this interface.

## Environment Variables

Adapters can access model configuration via environment variables set by the runtime:

- `MODEL_URL`: The model inference endpoint URL
- `MODEL_NAME`: The model identifier
- `MODEL_API_KEY`: API key for authentication (if provided via `auth.secret_ref`)

**Note**: These are set by the EvalHub runtime based on the job spec.

## Testing

To verify that your adapter correctly uses `model.url`:

1. **Deploy a local model**:
   ```bash
   # Example: Start vLLM server
   vllm serve meta-llama/Llama-3.2-1B-Instruct \
     --host 0.0.0.0 \
     --port 8000
   ```

2. **Submit an evaluation job**:
   ```json
   {
     "model": {
       "url": "http://localhost:8000/v1",
       "name": "meta-llama/Llama-3.2-1B-Instruct"
     },
     "benchmarks": [{"id": "arc_easy", "provider_id": "lm_evaluation_harness"}]
   }
   ```

3. **Verify the adapter connects to localhost**, not HuggingFace:
   - Check adapter logs for HTTP requests to `localhost:8000`
   - Ensure no network requests to `huggingface.co`

## Common Mistakes

### Mistake 1: Ignoring model.url
```python
# ❌ WRONG: Ignoring the URL and downloading from HF
model_name = job_spec["model"]["name"]
model = load_model_from_huggingface(model_name)
```

### Mistake 2: Using model.name as a file path
```python
# ❌ WRONG: Treating name as a local path
model_name = job_spec["model"]["name"]
model = load_model(f"/models/{model_name}")
```

### Mistake 3: Not handling authentication
```python
# ⚠️ INCOMPLETE: Missing authentication
client = OpenAI(base_url=job_spec["model"]["url"])
# Should include api_key if auth is required
```

**✅ CORRECT:**
```python
api_key = "dummy"  # Default for local servers
if "auth" in job_spec["model"]:
    api_key = load_api_key_from_secret(job_spec["model"]["auth"]["secret_ref"])

client = OpenAI(
    base_url=job_spec["model"]["url"],
    api_key=api_key
)
```

## Migration Guide

If you have an existing adapter that downloads models:

1. **Remove model download logic**:
   - Delete calls to `AutoModelForCausalLM.from_pretrained()`
   - Remove model caching/storage code

2. **Add OpenAI client**:
   - Install `openai` package
   - Create client with `base_url` from job spec

3. **Update inference calls**:
   - Replace direct model calls with OpenAI API calls
   - Map evaluation framework's interface to OpenAI's API

4. **Test with local inference server**:
   - Verify adapter works with vLLM, TGI, or similar servers

## Related Issues

- RHOAIENG-54862: model.url field is ignored, causing evaluation job to fail

## References

- [OpenAI API Reference](https://platform.openai.com/docs/api-reference)
- [vLLM OpenAI Server](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html)
- [LM Evaluation Harness Model Guide](https://github.com/EleutherAI/lm-evaluation-harness/blob/main/docs/model_guide.md)
