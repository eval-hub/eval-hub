# Offline data for the 12 loader-dependent smoke benchmarks

The bundle contains CMMLU (4), ACLUE (1), Pile (6), and SCROLLS/Qasper (1).
The 11 CMMLU, ACLUE and Pile job fixtures use a prepared HF dataset repository
instead of invoking legacy dataset loaders in evaluation pods. Qasper's data
is prepared but its job fixture remains unchanged pending the adapter fix below.
The selected model's tokenizer must be packaged with the data.

`prepare.py` reads pinned upstream CSV/JSONL archives and Parquet directly. It
does not execute dataset loading scripts or download model weights. It exports
one real row per required split by default, using `DatasetDict.save_to_disk()`.
The six Pile subsets use matching `meta.pile_set_name` records from the pinned
`NeelNanda/pile-10k` sample mirror, rather than the original full Pile corpus.
These are smoke execution inputs, not full benchmark or accuracy datasets.

## Prepare a bundle for the served model

From the repository root:

```bash
uv run --no-project --python 3.11 \
  --with-requirements tests/benchmarks/offline/requirements.txt \
  python tests/benchmarks/offline/prepare.py \
  --tokenizer Qwen/Qwen3-VL-30B-A3B-Instruct \
  --tokenizer-revision 9c4b90e1e4ba969fd3b5378b57d966d725f1b86c \
  --output bin/benchmark-offline-qwen
```

Choose the tokenizer repository and revision matching the served model. If
`--tokenizer` is omitted, `MODEL_NAME` is used. `--samples N` increases the
maximum exported rows per split; it does not change job sample counts.
An existing output directory is rejected. Downloads are cached separately in
`bin/benchmark-offline-cache` (`--cache-dir` overrides it).

The output contains:

- `tokenizer/`: a complete `save_pretrained()` export, including special tokens.
- Dataset directories such as `haonan-li--cmmlu--high_school_mathematics/`: the exact
  names the downstream runtime resolves, each with `dataset_dict.json` and split
  Arrow files. Pile directories retain `EleutherAI--pile--pile_<subset>` names.
- `hub/`, `modules/`, `evaluate/downloads/`: SCROLLS metric and imported helper
  caches. Its task otherwise attempts HF access even with local datasets.
- `bundle.json`: dataset/model revisions, sample provenance, split row counts,
  and SHA-256 checksums. Dataset source details are in `datasets.json`.

Preparation verifies exports, checksums, local tokenizer loading and the Qasper
metric in a fresh process with network connections blocked. It only publishes
the output directory locally after verification succeeds. Preparation executes
the upstream SCROLLS metric to populate its caches; evaluation uses that cached
metric. Dataset formats and metric caches must be compatible with the deployed
runtime; use a runtime that implements offline `DatasetDict` loading in
`lm_eval/api/task.py` and auto-detects local tokenizer/data in its adapter.

To check a relocated bundle independently:

```bash
uv run --no-project --python 3.11 \
  --with-requirements tests/benchmarks/offline/requirements.txt \
  python tests/benchmarks/offline/prepare.py --verify /path/to/bundle
```

## Reference the prepared HF repository in jobs

Create an HF **dataset** repository you control and upload the **contents** of
the output directory at its root, retaining all directory names. Do not upload
only a zip/tar archive: `test_data_ref.hf` downloads files without unpacking them.
The preparation command does not create or upload to any remote repository.
The original dataset/model repositories and their terms remain the sources for
the exported sample records and tokenizer; source IDs/revisions are recorded in
the bundle rather than attributing these assets to EvalHub.

Configure the checked-in fixtures with the published bundle's repository ID and
commit SHA:

```bash
export LM_EVAL_OFFLINE_HF_REPO_ID=my-org/my-model-smoke-bundle
export LM_EVAL_OFFLINE_HF_REVISION=<published-bundle-commit-sha>
export MODEL_NAME=Qwen/Qwen3-VL-30B-A3B-Instruct
make test-benchmarks BENCHMARK_RUN='TestBenchmarkSmoke/lm_evaluation_harness/'
```

The 11 enabled fixtures and generator defaults send:

```json
{
  "parameters": {
    "num_examples": 1,
    "num_fewshot": 0,
    "tokenizer": "/test_data/tokenizer"
  },
  "test_data_ref": {
    "hf": {
      "repo_id": "${LM_EVAL_OFFLINE_HF_REPO_ID}",
      "revision": "${LM_EVAL_OFFLINE_HF_REVISION}"
    }
  }
}
```

For a private bundle repository, add `hf.secret_ref` naming an existing Secret
with a `token` key. These HF references require cluster execution. The smoke
runner explicitly skips fixtures when either required bundle variable is absent.
Existing FVT Jsonnet jobs are separate from these smoke fixtures and are not
changed by this workflow.

## Checks

```bash
python3 -m unittest discover -s tests/benchmarks/offline
go test ./tests/benchmarks/...
make fmt lint
```

Offline bundle verification confirms assets can be loaded without network;
running the enabled jobs against the deployed LM runtime is still required to
confirm end-to-end behavior. A bundle must be rebuilt for a different model tokenizer.

## Qasper adapter follow-up

EvalHub mounts `/test_data` read-only on the adapter. The downstream SCROLLS task's
`_download_metric()` always copies the cached metric to another file under that
directory. Evaluation-module loading and dataset deduplication may also write
cache files. Prepared data cannot correct those writes by itself.

Qasper's bundle entry is therefore marked `job_ready: false`, and its checked-in
job fixture and generator profile are deliberately left unchanged. The adapter
must copy required metric/module caches to a writable directory, keep dataset
transforms in memory or use writable caches, and then pass a read-only mount
integration test before Qasper can use this offline job profile. The current
bundle verifier checks network independence on a writable directory; it does
not claim Qasper works with the production read-only mount.
