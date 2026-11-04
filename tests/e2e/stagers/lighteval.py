"""Prepare LightEval's selected datasets and eager registry dependencies."""

import importlib.util
import os
import shutil
from pathlib import Path
from urllib.request import urlopen

import nltk
from datasets import load_dataset
from transformers import AutoTokenizer

ROOT = Path("/test_data")
DATA_CACHE = ROOT / "datasets"
NLTK_DATA = ROOT / "nltk_data"
HF_TOKEN = os.environ.get("HF_TOKEN", "").strip()
TINY_BENCHMARKS_URL = (
    "https://raw.githubusercontent.com/felipemaiapolo/tinyBenchmarks/"
    "e9a8b1031b0340571beb6c9ca3a27891be09a8fd/tinyBenchmarks/tinyBenchmarks.pkl"
)
DATA_CACHE.mkdir(parents=True, exist_ok=True)

# LightEval imports IFEval while discovering tasks. IFEval loads these NLTK
# resources at import time, so stage them before importing the task registry.
# This staging pod has egress; evaluation-job pods do not.
NLTK_DATA.mkdir(parents=True, exist_ok=True)
os.environ["NLTK_DATA"] = str(NLTK_DATA)
nltk.data.path.insert(0, str(NLTK_DATA))
for resource in ("punkt", "punkt_tab"):
    print(f"Staging NLTK {resource} for LightEval", flush=True)
    nltk.download(resource, download_dir=str(NLTK_DATA), raise_on_error=True)
    nltk.data.find(f"tokenizers/{resource}", paths=[str(NLTK_DATA)])
nltk.sent_tokenize("First sentence. Second sentence.")

tiny_path = ROOT / "lighteval" / "tinyBenchmarks.pkl"
tiny_path.parent.mkdir(parents=True, exist_ok=True)
if not tiny_path.is_file() or tiny_path.stat().st_size == 0:
    print("Staging LightEval's eagerly imported tinyBenchmarks.pkl", flush=True)
    with urlopen(TINY_BENCHMARKS_URL, timeout=120) as response:
        tiny_path.write_bytes(response.read())

# LightEval imports this registry asset from its installed task directory,
# while EvalHub exposes staged files under /test_data. Copy it to LightEval's
# expected path before importing task definitions, which eagerly load it.
spec = importlib.util.find_spec("lighteval")
if spec is None or spec.origin is None:
    raise RuntimeError("installed LightEval package has no importable module path")
registry_dir = Path(spec.origin).parent / "tasks" / "tasks"
if not registry_dir.is_dir():
    raise RuntimeError(f"LightEval task registry directory does not exist: {registry_dir}")
shutil.copyfile(tiny_path, registry_dir / "tinyBenchmarks.pkl")
import main as lighteval_adapter  # noqa: E402

configs = lighteval_adapter._task_configs()
TASKS = ("lcb:codegeneration_v6", "gpqa:diamond", "math_500")
for task_name in TASKS:
    if task_name == "gpqa:diamond" and not HF_TOKEN:
        print("Skipping gated LightEval GPQA staging: set HF_TOKEN to include the GPQA e2e case", flush=True)
        continue
    config = configs.get(task_name)
    if config is None:
        raise RuntimeError(f"installed LightEval registry has no task {task_name!r}")
    repo = config.hf_repo
    subset = config.hf_subset
    revision = getattr(config, "hf_revision", None)
    if not repo:
        raise RuntimeError(f"LightEval task {task_name!r} has no Hugging Face repository")
    print(f"Caching LightEval task {task_name}: {repo}/{subset or 'default'}", flush=True)
    kwargs = {"cache_dir": str(DATA_CACHE)}
    if task_name == "gpqa:diamond":
        kwargs["token"] = HF_TOKEN
    if subset:
        kwargs["name"] = subset
    if revision:
        kwargs["revision"] = revision
    dataset = load_dataset(repo, **kwargs)
    if not len(dataset):
        raise RuntimeError(f"LightEval dataset {repo}/{subset or 'default'} has no splits")

tokenizer_dir = ROOT / "tokenizer"
if not (tokenizer_dir / "tokenizer.json").is_file():
    print("Staging the public smoke-test tokenizer", flush=True)
    AutoTokenizer.from_pretrained("sshleifer/tiny-gpt2").save_pretrained(tokenizer_dir)
AutoTokenizer.from_pretrained(str(tokenizer_dir), local_files_only=True)

print("CURATED_ASSETS_READY", flush=True)
