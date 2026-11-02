"""Prepare RULER's exact task inputs and a local tokenizer in the deployed image."""

import shutil
from pathlib import Path

from transformers import AutoTokenizer

from prepare_assets import prepare_assets

ROOT = Path("/test_data")
SCRIPTS = Path("/app/scripts")
CACHE = Path("/tmp/ruler-assets")
VERIFY = Path("/tmp/ruler-verify")
ROOT.mkdir(parents=True, exist_ok=True)

TASK_CONFIGS = [
    {"task": "common_words_extraction", "args": {"freq_cw": 30, "freq_ucw": 3, "num_cw": 10}},
    {"task": "freq_words_extraction", "args": {"alpha": 2.0}},
    {"task": "qa", "args": {"dataset": "squad"}},
    {"task": "qa", "args": {"dataset": "hotpotqa"}},
    {"task": "niah", "args": {"type_haystack": "essay"}},
    {"task": "variable_tracking", "args": {"type_haystack": "noise"}},
]

print("Preparing the RULER assets required by the six curated benchmarks", flush=True)
prepare_assets(TASK_CONFIGS, CACHE, SCRIPTS, timeout=1800)
for asset in CACHE.iterdir():
    destination = ROOT / asset.name
    if asset.is_dir():
        shutil.copytree(asset, destination, dirs_exist_ok=True)
    else:
        shutil.copy2(asset, destination)
prepare_assets(TASK_CONFIGS, VERIFY, SCRIPTS, staged_data_dir=ROOT)

tokenizer_dir = ROOT / "tokenizer"
if not tokenizer_dir.is_dir():
    print("Staging the public smoke-test tokenizer", flush=True)
    AutoTokenizer.from_pretrained("sshleifer/tiny-gpt2").save_pretrained(tokenizer_dir)
AutoTokenizer.from_pretrained(str(tokenizer_dir), local_files_only=True)

print("CURATED_ASSETS_READY", flush=True)
