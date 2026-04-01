# CVE Fix Guidance — eval-hub/eval-hub
<!-- last-analyzed: 2026-04-01 | cve-merged: 9 | cve-closed: 10 -->

## Titles

**NPM/Node.js dependencies:**
`chore(deps): bump <package> from <old-version> to <new-version>` (6/9 merged)
`chore(deps-dev): bump <package> from <old-version> to <new-version>` (4/9 merged)

**Python dependencies:**
`chore(deps): bump <package> from <old-version> to <new-version> in /python-server` (1/9 merged)

**Manual CVE fixes:**
`fix(cve): <description>` (1/9 merged + 5 closed)

## Branches

**Dependabot (NPM):**
`dependabot/npm_and_yarn/<package>-<version>` (6/9 merged)
`dependabot/npm_and_yarn/multi-<hash>` for multiple packages (2/9 merged)

**Dependabot (Python):**
`dependabot/uv/python-server/<package>-<version>` (1/9 merged)

**Manual CVE fixes:**
`fix/cve-<year>-<description>-attempt-<N>` (1/9 merged + 5 closed)

## Files — JavaScript/NPM

Update `package-lock.json` only (7/9 merged)
Do not modify `package.json` — let lock file manage versions (1 closed PR modified both)

## Files — Go

For Go stdlib updates: update `go.mod` only (1/9 merged)
Do not include `go.sum` changes for stdlib-only updates (3 closed PRs included it)
Do not modify `Containerfile` when updating Go stdlib (5 closed PRs modified it)

## Files — Python

Update `python-server/uv.lock` only for Python deps (1/9 merged)

## Labels

Dependabot PRs use: `dependencies` + language label (`javascript`, `go`, `python:uv`) (8/9 merged)
Manual `fix(cve)` PRs typically have no labels (1/9 merged)

## Don'ts

- Don't modify Containerfile when updating Go stdlib — go.mod changes only (5 closed: #392, #390, #383, #382, #379)
- Don't modify package.json when bumping NPM deps — package-lock.json only (1 closed: #398)
- Don't include go.sum for Go stdlib-only updates (3 closed: #390, #382, #379)