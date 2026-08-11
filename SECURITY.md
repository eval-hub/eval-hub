# Security Policy

## Supported Versions

Security fixes are applied on the `main` branch and included in subsequent releases.
Older release branches may receive backports when maintainers determine they remain supported.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Use [GitHub private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability) for this repository:

1. Open the repository **Security** tab.
2. Click **Report a vulnerability**.
3. Include affected versions, impact, and steps to reproduce where possible.

Maintainers will acknowledge the report, investigate, and coordinate remediation and disclosure.

## Automated Security Checks

This repository runs automated security checks in CI, including:

- CodeQL analysis for Go and Python (`.github/workflows/codeql.yml`)
- Gosec static analysis for Go code
- OpenSSF Scorecard for repository security posture

Results may appear under the repository **Security** tab when SARIF upload is enabled.

### Neutral CodeQL / "configuration not found" on pull requests

If a PR shows a grey (neutral) **CodeQL** / **Code scanning results** check with a warning like:

> Code scanning cannot determine the alerts introduced by this pull request, because 1 configuration present on `refs/heads/main` was not found: Actions workflow (`codeql.yml`) — `/language:<lang>`

then `main` still has a stored Code scanning analysis for a language (or category) that the current workflow no longer uploads on pull requests. That often happens after a language is removed from the CodeQL matrix (for example, dropping `ruby` while leaving old `/language:ruby` analyses on `main`).

GitHub treats `neutral` as a passing required check, but the warning remains until the leftover analyses are deleted.

**Cleanup (maintainers):**

1. List leftover analyses for the missing category on `main` (replace `/language:ruby` as needed). Only the newest analysis in each set is deletable (`deletable: true`); older IDs in the same chain cannot be deleted directly:

   ```bash
   gh api 'repos/eval-hub/eval-hub/code-scanning/analyses?ref=refs/heads/main&per_page=100' --paginate \
     --jq '[.[] | select(.category == "/language:ruby" and .deletable == true)] | sort_by(.created_at) | reverse | .[0] | {id, category, created_at, commit_sha, deletable, results_count}'
   ```

2. Delete that deletable analysis ID (listing alone does not remove it):

   ```bash
   gh api -X DELETE 'repos/eval-hub/eval-hub/code-scanning/analyses/<ANALYSIS_ID>?confirm_delete=true'
   ```

   A successful delete returns `next_analysis_url` and `confirm_delete_url` for the previous analysis in the set. Use `confirm_delete_url` (or DELETE with `?confirm_delete=true`) to keep removing analyses until both URLs are `null`. Prefer `confirm_delete_url` when clearing a leftover language entirely; `next_analysis_url` stops before the last analysis in the set.

3. List again with the same filter. If another deletable analysis remains for the category, repeat steps 2–3 until the filter returns nothing (or `null`).

4. Re-run checks on open PRs (or push a new commit). Existing PRs keep the old warning until Code scanning runs again against the cleaned `main` configuration.

### OpenSSF Scorecard: pin pip installs by hash

Scorecard's **Pinned-Dependencies** check flags workflow steps that run `pip install` with version pins only (for example `pip install pyyaml==6.0.3`). Version pins are not enough; Scorecard expects hash verification via a requirements file:

```bash
python -m pip install --require-hashes -r <requirements.txt>
```

Inline `pip install pkg==version --hash=...` is often still flagged. Prefer `-r` plus `--require-hashes`.

The TrustyAI operator ConfigMap sync workflow (`.github/workflows/check-trustyai-service-operator-configmap-sync.yml`) installs Python deps this way from:

| File | Role |
|------|------|
| `.github/requirements-configmap-sync.in` | Direct pins (`pip`, `pyyaml`, `requests`) — edit this when bumping versions |
| `.github/requirements-configmap-sync.txt` | Generated lockfile with transitive deps and SHA-256 hashes — do not hand-edit |

**Regenerate the lockfile** after changing the `.in` file ([uv](https://docs.astral.sh/uv/) required):

```bash
uv pip compile --generate-hashes \
  .github/requirements-configmap-sync.in \
  -o .github/requirements-configmap-sync.txt
```

Commit both the `.in` and `.txt` changes together. Verify locally in a clean venv if needed:

```bash
python3 -m venv /tmp/reqcheck
/tmp/reqcheck/bin/pip install --require-hashes -r .github/requirements-configmap-sync.txt
```

Apply the same pattern for any new GitHub Actions `pip install` steps so Scorecard does not reopen `pipCommand not pinned by hash` alerts.

## Verifying GitHub Releases

GitHub Releases for `v*` tags are created by `.github/workflows/signed-release.yml` with project-level assets (`eval-hub-<tag>.tar.gz`, `SHA256SUMS`) and SLSA provenance (`provenance.sigstore.json`). Provenance is generated using GitHub's native artifact attestation (`actions/attest`) and stored in both the GitHub attestation API and as a release asset.

OpenSSF Scorecard's **Signed-Releases** check does not yet query the GitHub attestation API ([ossf/scorecard#4667](https://github.com/ossf/scorecard/issues/4667)). It detects `provenance.sigstore.json` by filename and awards a score of **8** (signature present). The maximum score of 10 (SLSA provenance) will apply once Scorecard adds native attestation API support. This is independent of optional MCP binary uploads from `.github/workflows/release-mcp.yml`.

### Verify provenance with gh attestation verify

1. Download a release asset from the GitHub Release (for example `eval-hub-vX.Y.Z.tar.gz`).
1. Verify online (queries the GitHub attestation API and fetches the trust root):

```bash
gh attestation verify eval-hub-vX.Y.Z.tar.gz \
  --repo eval-hub/eval-hub \
  --signer-workflow eval-hub/eval-hub/.github/workflows/signed-release-tag.yml
```

1. Or verify fully offline using the bundle and trusted root from the release:

```bash
gh attestation verify eval-hub-vX.Y.Z.tar.gz \
  --bundle provenance.sigstore.json \
  --custom-trusted-root trusted_root.jsonl \
  --repo eval-hub/eval-hub \
  --signer-workflow eval-hub/eval-hub/.github/workflows/signed-release-tag.yml
```

`SHA256SUMS` is an integrity aid for the archive; it is not a Scorecard-recognized signature. The Scorecard-recognized artifact is the `provenance.sigstore.json` bundle (score 8 until Scorecard adds attestation API support).

> **Note:** Releases prior to this workflow migration may have `*.intoto.jsonl` provenance (generated by `slsa-github-generator`). Those can be verified with [`slsa-verifier`](https://github.com/slsa-framework/slsa-verifier) instead.

### Backfilled releases

Existing releases can be signed via **Actions → Signed release → Run workflow** (`workflow_dispatch`):

| Input | Use |
|-------|-----|
| `tag` | Sign/backfill one tag (e.g. `v1.0.0`) |
| `backfill_last_n` | When `tag` is empty, process this many recent unpublished-provenance releases (default `5`) |
| `publish` | Set `true` only when the release should be undrafted after provenance (new releases). Leave `false` when backfilling already-published releases. |

Provenance attached this way attests that GitHub Actions hashed the subject files during the backfill job; it does **not** claim those bytes were produced by the original historical build. New tagged releases use draft → project assets → provenance → publish so provenance is present before the release is published. Publish waits briefly for optional MCP binaries when that workflow is still running, but still undrafts if MCP fails or never uploads; MCP may attach binaries later with `gh release upload --clobber` on mutable releases.

If the repository enables [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases), assets cannot be added to already-published releases; backfill will fail for those tags and only new draft→publish releases can be signed. With immutable releases enabled, MCP binaries must land before publish or they cannot be added afterward.

### Post-merge validation

1. Merge the Signed-Releases workflows to `main`.
2. Run **Signed release** with `backfill_last_n=5` (or specific tags) and confirm each release gains `provenance.sigstore.json` (and project archive/`SHA256SUMS` if they were missing).
3. Optionally push a new `v*` tag and confirm draft→publish includes provenance without requiring the MCP workflow.
4. Re-run **OpenSSF Scorecard** (`.github/workflows/scorecard.yml`) and check **Signed-Releases**.
