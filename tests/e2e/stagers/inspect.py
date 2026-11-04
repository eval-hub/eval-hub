"""Prepare Inspect Hugging Face and CyberSecEval assets in the deployed image."""

import hashlib
import json
import os
from pathlib import Path
from urllib.request import urlopen

from datasets import load_dataset

ROOT = Path("/test_data")
HF_TOKEN = os.environ.get("HF_TOKEN", "").strip()
CYBERSECEVAL_URL = (
    "https://raw.githubusercontent.com/meta-llama/PurpleLlama/"
    "70e7a376f2a310fb89f7ad207685baa69f78107f/"
    "CybersecurityBenchmarks/datasets/prompt_injection/prompt_injection.json"
)
ROOT.mkdir(parents=True, exist_ok=True)

DATASETS = [
    (
        "cais/hle",
        None,
        "5a81a4c7271a2a2a312b9a690f0c2fde837e4c29",
        "test",
    ),
    (
        # Match the repository ID used by the pinned Inspect task; the Hub
        # cache is keyed by repository ID, so the alias cannot satisfy offline loads.
        "lmms-lab-encoder/DocVQA",
        "DocVQA",
        "539088ef8a8ada01ac8e2e6d4e372586748a265e",
        "validation",
    ),
]
for repo, config, revision, required_split in DATASETS:
    if repo == "cais/hle" and not HF_TOKEN:
        print("Skipping gated cais/hle staging: set HF_TOKEN to include the HLE e2e case", flush=True)
        continue
    print(f"Caching {repo} ({config or 'default'}) at revision {revision}", flush=True)
    # Use the standard HF_HOME/HF_HUB_CACHE/HF_DATASETS_CACHE paths configured
    # on the staging pod; the evaluation adapter uses the same paths offline.
    kwargs = {"revision": revision}
    if repo == "cais/hle":
        kwargs["token"] = HF_TOKEN
    if config:
        kwargs["name"] = config
    dataset = load_dataset(repo, **kwargs)
    if required_split not in dataset or len(dataset[required_split]) == 0:
        raise RuntimeError(f"{repo} has no non-empty {required_split!r} split")

# Match eval-hub-contrib/adapters/inspect/_preflight.py's cache-file helper:
# both the pinned URL and the first 32 SHA-256 hex characters are part of the path.
url_hash = hashlib.sha256(CYBERSECEVAL_URL.encode("utf-8")).hexdigest()[:32]
cyber_path = ROOT / "cyberseceval_2" / f"prompt_injection_{url_hash}.json"
cyber_path.parent.mkdir(parents=True, exist_ok=True)
if not cyber_path.is_file() or cyber_path.stat().st_size == 0:
    print(f"Staging the pinned CyberSecEval dataset at {cyber_path}", flush=True)
    with urlopen(CYBERSECEVAL_URL, timeout=120) as response:
        payload = response.read()
    json.loads(payload)
    cyber_path.write_bytes(payload)

print("CURATED_ASSETS_READY", flush=True)
