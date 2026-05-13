# Running LightEval locally with EvalHub

This guide shows how to use eval-hub-server to run LightEval evaluations locally.

## Prerequisites

The following tools are expected to be available on your machine:

- [uv](https://docs.astral.sh/uv/) — Python package manager
- [podman](https://podman.io/) (or Docker) — for running the OCI registry
- [ollama](https://ollama.com/) (or any OpenAI-compatible LLM server) — for serving a local model

The setup runs eval-hub-server, MLflow, an OCI registry, and an LLM model all on `localhost`.

## 1. Download the LightEval adapter

Download the adapter driver and its requirements from eval-hub-contrib:

```bash
curl -o main.py https://raw.githubusercontent.com/eval-hub/eval-hub-contrib/main/adapters/lighteval/main.py
curl -o requirements.txt https://raw.githubusercontent.com/eval-hub/eval-hub-contrib/main/adapters/lighteval/requirements.txt
```

## 2. Install dependencies

Install the project packages and the LightEval adapter requirements:

```bash
uv sync --extra demo
uv pip install -r requirements.txt
```

This installs eval-hub-server, MLflow, packages required for the notebook `evalhub-client.ipynb`, and the LightEval adapter runtime dependencies.

## 3. Start MLflow

> **Note:** The first `mlflow server` start may take a few extra seconds while it initializes the database.

Activate the venv and start the MLflow server there:

```bash
source .venv/bin/activate

mlflow server \
  --backend-store-uri sqlite:///mlflow.db \
  --host localhost \
  --port 5000
```

Verify it's running from another terminal:

```bash
curl http://localhost:5000/health
```

The MLflow UI dashboard is accessible at `http://localhost:5000`.

## 4. Start the OCI registry

In another terminal, pull the registry image and start it on `localhost:5001`:

```bash
podman pull docker.io/library/registry:2

podman run -d -p 5001:5000 \
    --name eval-hub-oci-registry \
    -e REGISTRY_STORAGE_DELETE_ENABLED=true \
    docker.io/library/registry:2
```

## 5. Start the LLM server

> **Note:** Ollama is used here as an example. Any OpenAI-compatible LLM server (llama.cpp, LM Studio, etc.) will work.

Pull a model:

```bash
ollama pull llama3.2:3b-instruct-q4_K_M
```

Verify it's running:

```bash
curl -s http://localhost:11434/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3.2:3b-instruct-q4_K_M",
    "messages": [{"role": "user", "content": "Why is the sky blue?"}],
    "max_tokens": 100
  }' | jq "."
```

## 6. Configure eval-hub-server

Download the template `config.yaml` from the eval-hub repository:

```bash
mkdir -p config
curl -o config/config.yaml https://raw.githubusercontent.com/eval-hub/eval-hub/main/config/config.yaml
```

Create the provider configuration `config/providers/lighteval.yaml`:

```bash
mkdir -p config/providers

cat > config/providers/lighteval.yaml << 'EOF'
id: lighteval
name: LightEval
description: LightEval for evaluation framework
runtime:
  local:
    command: "python main.py"
    env:
      - name: OCI_INSECURE
        value: "true"

benchmarks:
  - id: gsm8k
    name: Grade-school math word problems
    description: |-
      Multi-step arithmetic word problems requiring 2-8 reasoning steps (8-shot, 1,319 examples).
    category: math
    metrics:
      - exact_match
      - acc
    num_few_shot: 8
    dataset_size: 1319
    tags:
      - math
      - reasoning
      - lighteval
    primary_score:
      metric: acc
      lower_is_better: false
    pass_criteria:
      threshold: 0.25
EOF
```

## 7. Start eval-hub-server

In a terminal with the venv activated, start the server with the required environment variables:

```bash
source .venv/bin/activate

MLFLOW_TRACKING_URI=http://localhost:5000 \
  eval-hub-server --local --configdir ./config
```

Verify it's running in another terminal:

```bash
curl http://localhost:8080/api/v1/health
```

## 8. Run an evaluation

Use the `evalhub-client.ipynb` notebook to run the evaluation lifecycle.
