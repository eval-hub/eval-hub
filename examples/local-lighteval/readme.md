# Running LightEval locally with EvalHub

Follow the [Local Mode Tutorial](https://eval-hub.github.io/guides/local-mode-tutorial/) for step-by-step setup instructions covering eval-hub-server, MLflow, an OCI registry, and a local LLM.

## What's in this directory

| File | Purpose |
|---|---|
| `pyproject.toml` | Python dependencies — run `uv sync --extra demo` to install everything |
| `evalhub-client.ipynb` | Jupyter notebook that demonstrates the full evaluation lifecycle using the eval-hub-sdk Python client (submitting jobs, polling status, retrieving results) |
