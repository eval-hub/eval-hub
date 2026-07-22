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

### Neutral CodeQL / “configuration not found” on pull requests

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
