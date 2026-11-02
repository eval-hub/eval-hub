"""Prepare LM Evaluation Harness test-data bundles in the deployed image."""

from pathlib import Path

from datasets import load_dataset, load_from_disk
from huggingface_hub import snapshot_download
from transformers import AutoTokenizer

ROOT = Path("/test_data")
CACHE = Path("/tmp/hf-cache")

# The first path is the slug built from the task YAML's dataset_path/name. The
# source repo may differ when a task uses a Hub mirror of that dataset.
DATASETS = [
    ("oskarvanderwal/winogender", "oskarvanderwal/winogender", "all", None),
    ("jannalu/crows_pairs_multilingual", "jannalu/crows_pairs_multilingual", "english", None),
    ("oskarvanderwal/bbq", "oskarvanderwal/bbq", "All", None),
    ("epfl-dlab/JSONSchemaBench", "epfl-dlab/JSONSchemaBench", "Github_easy", None),
    ("skg/toxigen-data", "skg/toxigen-data", "annotated", None),
    (
        "EleutherAI/hendrycks_ethics",
        "EleutherAI/hendrycks_ethics",
        "commonsense",
        "a710dd862056df1a92346e7afee93a34d57a5f42",
    ),
    ("hails/bigbench", "hails/bigbench", "hhh_alignment_zero_shot", None),
    ("truthful_qa", "truthfulqa/truthful_qa", "multiple_choice", None),
]

ROOT.mkdir(parents=True, exist_ok=True)
CACHE.mkdir(parents=True, exist_ok=True)

for task_path, source_repo, config, revision in DATASETS:
    slug = task_path.replace("/", "--") + "--" + config
    destination = ROOT / slug
    if (destination / "dataset_dict.json").is_file():
        print(f"Using existing dataset bundle {destination}", flush=True)
        continue
    print(f"Staging {source_repo}/{config} at {destination}", flush=True)
    kwargs = {"name": config, "cache_dir": str(CACHE)}
    if revision:
        kwargs["revision"] = revision
    dataset = load_dataset(source_repo, **kwargs)
    if not len(dataset):
        raise RuntimeError(f"{source_repo}/{config} has no splits")
    dataset.save_to_disk(str(destination))
    local = load_from_disk(str(destination))
    if not len(local):
        raise RuntimeError(f"saved bundle {destination} has no splits")

tokenizer_dir = ROOT / "tokenizer"
if not (tokenizer_dir / "tokenizer.json").is_file():
    print("Staging the public smoke-test tokenizer", flush=True)
    snapshot_download(repo_id="sshleifer/tiny-gpt2", local_dir=str(tokenizer_dir))
AutoTokenizer.from_pretrained(str(tokenizer_dir), local_files_only=True)

print("CURATED_ASSETS_READY", flush=True)
