#!/usr/bin/env python3
"""Export model-specific smoke data; run with offline/requirements.txt installed.

Preparation reads pinned CSV/JSONL archives and Parquet, without dataset loaders.
The output is a directory to upload intact to an HF dataset repo or mount by PVC.
"""

import argparse
import csv
import hashlib
import io
import importlib.metadata
import itertools
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import zipfile


MANIFEST = Path(__file__).with_name("datasets.json")


def bundle_name(spec):
    return spec["repo_id"].replace("/", "--") + "--" + spec["subset"]


def read_archive(spec, archive, samples):
    """Read a bounded number of real records, preserving task column names."""
    result = {}
    with zipfile.ZipFile(archive) as zipped:
        for split in spec["splits"]:
            filename = (f"qasper/{split}.jsonl" if spec["subset"] == "qasper"
                        else f"{split}/{spec['subset']}.csv")
            with zipped.open(filename) as raw:
                text = io.TextIOWrapper(raw, encoding="utf-8-sig")
                records = (json.loads(line) for line in text) if filename.endswith("jsonl") else csv.DictReader(text)
                result[split] = list(itertools.islice(records, samples))
            required = {"id", "pid", "input", "output"} if spec["subset"] == "qasper" else {"Question", "A", "B", "C", "D", "Answer"}
            if not result[split] or not required <= result[split][0].keys():
                raise ValueError(f"Missing records/columns in {filename}")
    return result


def metric_path(cache):
    from huggingface_hub import hf_hub_download

    original = hf_hub_download(
        "tau/scrolls", "metrics/scrolls.py", repo_type="dataset",
        revision="refs/pr/5", cache_dir=str(cache / "hub"),
    )
    # Match _download_metric() in the downstream SCROLLS task exactly.
    renamed = Path(os.path.dirname(original) + os.path.basename(original).replace(".", "_") + ".py")
    shutil.copyfile(original, renamed)
    return renamed


def verify(root):
    """Check the exports, tokenizer, metric cache and checksums without network."""
    os.environ.update(
        HF_HOME=str(root), HF_HUB_CACHE=str(root / "hub"),
        HF_MODULES_CACHE=str(root / "modules"), HF_DATASETS_OFFLINE="1",
        HF_HUB_OFFLINE="1", HF_EVALUATE_OFFLINE="1", TRANSFORMERS_OFFLINE="1",
    )
    import socket

    def no_network(*args, **kwargs):
        raise RuntimeError("Bundle verification attempted network access")

    socket.socket.connect = no_network
    from datasets import load_from_disk
    from transformers import AutoTokenizer
    import evaluate

    manifest = json.loads((root / "bundle.json").read_text())
    for file, digest in manifest["sha256"].items():
        if hashlib.sha256((root / file).read_bytes()).hexdigest() != digest:
            raise ValueError(f"Checksum mismatch: {file}")
    for spec in manifest["datasets"]:
        dataset = load_from_disk(str(root / bundle_name(spec)))
        for split, count in spec["rows"].items():
            if len(dataset[split]) != count or count == 0:
                raise ValueError(f"Invalid split: {spec['benchmark']}/{split}")
    tokenizer = AutoTokenizer.from_pretrained(root / "tokenizer", local_files_only=True)
    if not tokenizer.encode("Offline smoke test"):
        raise ValueError("Tokenizer produced no tokens")
    metric = evaluate.load(str(metric_path(root)), config_name="qasper")
    result = metric.compute(predictions=["answer"], references=[["answer"]])
    if not result:
        raise ValueError("SCROLLS metric returned no result")
    print(f"Verified {len(manifest['datasets'])} datasets, tokenizer and SCROLLS metric offline")


def prepare(output, tokenizer_name, tokenizer_revision, samples, cache):
    if output.exists():
        raise ValueError(f"Output already exists: {output}; choose a new directory")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="offline-prepare-", dir=output.parent) as work:
        work = Path(work)
        bundle = work / "bundle"
        bundle.mkdir()
        # Set cache paths before importing datasets/evaluate/transformers.
        os.environ.update(HF_HOME=str(cache), HF_HUB_CACHE=str(cache / "hub"),
                          HF_MODULES_CACHE=str(cache / "modules"))
        from datasets import Dataset, DatasetDict
        from huggingface_hub import HfApi, hf_hub_download
        from transformers import AutoTokenizer
        import evaluate
        from evaluate.utils.file_utils import hash_url_to_filename

        specs = json.loads(MANIFEST.read_text())
        pile = None
        for spec in specs:
            print(f"Preparing {spec['benchmark']}", flush=True)
            if "pile_set_name" in spec:
                if pile is None:
                    parquet = hf_hub_download(spec["source_repo_id"], spec["parquet"],
                                              repo_type="dataset", revision=spec["revision"],
                                              cache_dir=str(cache / "hub"))
                    pile = Dataset.from_parquet(parquet)
                rows = [r for r in pile if r["meta"]["pile_set_name"] == spec["pile_set_name"]][:samples]
                if not rows:
                    raise ValueError(f"Pile sample has no {spec['pile_set_name']} records")
                exported = DatasetDict(train=Dataset.from_list([{"text": r["text"]} for r in rows]))
            else:
                archive = hf_hub_download(spec["repo_id"], spec["archive"], repo_type="dataset",
                                          revision=spec["revision"], cache_dir=str(cache / "hub"))
                exported = DatasetDict({split: Dataset.from_list(rows)
                                        for split, rows in read_archive(spec, archive, samples).items()})
            if any(len(split) == 0 for split in exported.values()):
                raise ValueError(f"Empty source split: {spec['benchmark']}")
            exported.save_to_disk(str(bundle / bundle_name(spec)))
            spec["rows"] = {name: len(split) for name, split in exported.items()}

        revision = HfApi().model_info(tokenizer_name, revision=tokenizer_revision).sha
        tokenizer = AutoTokenizer.from_pretrained(tokenizer_name, revision=revision, trust_remote_code=False)
        tokenizer.save_pretrained(bundle / "tokenizer")
        # SCROLLS loads a metric from HF even with local data. Warm both caches
        # and include plain files so the exported directory is relocatable.
        evaluate.load(str(metric_path(cache)), config_name="qasper")
        # evaluate 0.4.x looks up the URL-only cache name when offline, but
        # online downloads use URL+ETag names. Provide both for relocation.
        for metadata in (cache / "evaluate" / "downloads").glob("*.py.json"):
            info = json.loads(metadata.read_text())
            cached = metadata.with_suffix("")
            offline_name = metadata.parent / hash_url_to_filename(info["url"])
            if cached != offline_name:
                shutil.copyfile(cached, offline_name)
        metric_repo = "datasets--tau--scrolls"
        # Exclude archives and tokenizer download caches; only the SCROLLS
        # metric snapshot and its ref are needed in the job's Hub cache.
        metric_snapshot = cache / "hub" / metric_repo
        metric_files = [p for p in metric_snapshot.rglob("*") if p.is_file() and p.name.endswith(".py")]
        for p in metric_files:
            target = bundle / "hub" / p.relative_to(cache / "hub")
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(p, target)
        shutil.copytree(metric_snapshot / "refs", bundle / "hub" / metric_repo / "refs")
        shutil.copytree(cache / "modules" / "evaluate_modules", bundle / "modules" / "evaluate_modules",
                        ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        shutil.copytree(cache / "evaluate" / "downloads", bundle / "evaluate" / "downloads",
                        ignore=shutil.ignore_patterns("*.lock"))
        (bundle / "modules" / "__init__.py").touch()
        manifest = {
            "format_version": 1, "purpose": "smoke execution, not benchmark accuracy",
            "tokenizer": {"repo_id": tokenizer_name, "revision": revision}, "datasets": specs,
            "preparation_versions": {name: importlib.metadata.version(name)
                                     for name in ("datasets", "transformers", "evaluate", "pyarrow")},
            "sha256": {str(p.relative_to(bundle)): hashlib.sha256(p.read_bytes()).hexdigest()
                       for p in sorted(bundle.rglob("*")) if p.is_file()},
        }
        (bundle / "bundle.json").write_text(json.dumps(manifest, indent=2) + "\n")
        # A fresh process picks up cache settings at import time, as the job does.
        subprocess.run([sys.executable, str(Path(__file__).resolve()), "--verify", str(bundle)], check=True)
        bundle.rename(output)
    print(f"Prepared bundle: {output}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--tokenizer", default=os.environ.get("MODEL_NAME"))
    parser.add_argument("--tokenizer-revision", default="main")
    parser.add_argument("--samples", type=int, default=1)
    parser.add_argument("--cache-dir", type=Path, default=Path("bin/benchmark-offline-cache"))
    parser.add_argument("--verify", type=Path, help="verify an existing bundle without network")
    args = parser.parse_args()
    if args.verify:
        verify(args.verify.resolve())
        return
    if not args.output or not args.tokenizer or args.samples < 1:
        parser.error("--output, --tokenizer (or MODEL_NAME), and --samples >= 1 are required")
    prepare(args.output.resolve(), args.tokenizer, args.tokenizer_revision, args.samples, args.cache_dir.resolve())


if __name__ == "__main__":
    sys.dont_write_bytecode = True
    os.environ.setdefault("HF_HUB_ETAG_TIMEOUT", "60")
    os.environ.setdefault("HF_HUB_DOWNLOAD_TIMEOUT", "60")
    main()
