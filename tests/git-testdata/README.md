# Git offline test data (FVT `@git`)

Minimal Hugging Face / lm-eval offline cache used by evaluation-job FVT that clones
test data from git (`test_data_ref.git`).

## Contents

| Path | Purpose |
| --- | --- |
| `tokenizer/` | Offline tokenizer for the default (`arc_easy`) git scenarios |
| `allenai--ai2_arc--ARC-Easy/` | Offline ARC-Easy dataset for `arc_easy` |
| `staging_sub_path/tokenizer/` | Tokenizer for the nested `sub_path` scenario |
| `staging_sub_path/truthful_qa--multiple_choice/` | Offline TruthfulQA MC data for `truthfulqa_mc1` |

Root layout is used with `sub_path=tests/git-testdata` (`arc_easy`).
Nested `staging_sub_path` uses a **different** FVT benchmark (`truthfulqa_mc1`) so the
`git.sub_path` scenario is not another arc_easy clone.

## Usage

FVT defaults clone this repository and check out this directory:

- `TEST_DATA_GIT_URL` → `https://github.com/eval-hub/eval-hub`
- `TEST_DATA_GIT_REF` → branch/tag/SHA that contains this folder (e.g. `main` after merge)
- `TEST_DATA_GIT_SUB_PATH` → `tests/git-testdata` (default for arc_easy)
- `TEST_DATA_GIT_NESTED_SUB_PATH` → `tests/git-testdata/staging_sub_path` (truthfulqa_mc1)

Until this folder is on the target ref, set `TEST_DATA_GIT_REF` to a branch that has it.

## Maintenance

These files are a **vendored offline lm-eval cache** checked into git so FVT can clone a
self-contained tree. They are not generated on every run. If benchmark tokenizer paths or
HF offline layout change, refresh this directory (same sources as PVC/offline staging data)
and re-run `@git` FVT before merging.

Keeping a copy here is intentional: FVT validates git clone + checkout, not live Hugging
Face downloads. Drift from other offline fixtures is low risk because scenarios only assert
job completion and metrics thresholds, not byte-for-byte parity with PVC/S3 caches.
