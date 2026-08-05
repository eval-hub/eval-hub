---
name: release-notes
description: >
  Draft human-readable GitHub release notes for a given version or tag.
  Summarizes major changes and upgrade impact from merged PRs and conventional
  commits (never raw git log). Use when writing release notes, preparing a
  GitHub release, satisfying OpenSSF release-notes requirements, or when the
  user asks to generate notes for a tag like v1.0.0.
allowed-tools:
  - Read
  - Edit
  - Bash(git *)
  - Bash(gh *)
  - Bash(grep *)
  - Bash(jq *)
  - Bash(sort *)
  - Bash(sed *)
  - Bash(cat *)
---

# Release Notes Skill

Draft **human-readable** release notes for an EvalHub GitHub release so users can
decide whether to upgrade and what the upgrade impact will be.

## OpenSSF requirement (must satisfy)

> The project MUST provide, in each release, release notes that are a
> human-readable summary of major changes in that release to help users
> determine if they should upgrade and what the upgrade impact will be. The
> release notes MUST NOT be the raw output of a version control log (e.g., the
> "git log" command results are not release notes).

Git history and `gh` PR lists are **inputs only**. The published body must be a
curated summary. GitHub `--generate-notes` alone is **not** sufficient output
for this skill.

## OpenSSF allowed implementations (may)

> The release notes MAY be implemented in a variety of ways. Many projects
> provide them in a file named "NEWS", "CHANGELOG", or "ChangeLog", optionally
> with extensions such as ".txt", ".md", or ".html". Historically the term
> "change log" meant a log of every change, but to meet these criteria what is
> needed is a human-readable summary. The release notes MAY instead be provided
> by version control system mechanisms such as the GitHub Releases workflow.

**EvalHub choice:** publish curated notes via the **GitHub Releases** body for
each tagged release (`vX.Y.Z`). Do not add a `NEWS` / `CHANGELOG` file unless
the user explicitly asks for that as an additional channel. Either approach is
valid under OpenSSF as long as the content is a human-readable summary (not a
raw change log of every commit).

## How to invoke

**Cursor:** Ask e.g. “Generate release notes for v1.0.0 using the release-notes
skill”. Optionally attach `@.claude/skills/release-notes/SKILL.md`.

**Claude Code:** `/release-notes` or ask naturally (e.g. “Draft release notes
for the next tag”).

## Procedure

Follow these steps **in order**. Do not skip steps.

### Step 1 — Resolve the target version

Accept an explicit tag/version from the user (e.g. `v1.0.0` or `1.0.0`).

If none is given:

1. Read `VERSION` (unprefixed SemVer, e.g. `1.0.0`).
2. Use that as the target.

**Validate before any shell interpolation.** The supplied value (user input or
`VERSION`) MUST match `^v?[0-9]+\.[0-9]+\.[0-9]+$`. Reject anything else and
stop; do not interpolate unvalidated strings into Git or `gh` commands.

Normalize a valid value to the canonical tag form:

- **Tag (`target_tag`):** `vX.Y.Z` (always with a leading `v`)
- **Version:** `X.Y.Z` (no `v`; strip a leading `v` if present)

Use only `target_tag` in all Git/`gh` commands below.

Confirm the tag exists locally or on the remote (`git fetch --tags` if needed):

```bash
git rev-parse --verify "refs/tags/${target_tag}^{}"
```

If the tag does not exist yet, still draft notes using the ranges in Step 2–3
(with `HEAD` or `main` as the upper bound) and say the release is not published.

### Step 2 — Find the previous release tag

```bash
git tag --list 'v*' --sort=-v:refname
```

Pick the highest SemVer tag **strictly older** than `target_tag`. Store it as
`previous_tag`.

**If a previous tag exists** — use ranges `previous_tag..target_tag` (or
`previous_tag..HEAD` / `previous_tag..main` when the target tag is not
published yet).

**If none (first release)** — there is no `previous_tag`. Do **not** invent or
reference an undefined `previous_tag`. Use the first-release branch:

- Commit range: `target_tag` (or `HEAD` / `main` if the tag is not published)
- PR time window: from the repository’s earliest commit date through the
  target tag’s (or `HEAD`’s) committer date
- State clearly that this is the first release

### Step 3 — Collect change inputs (not the notes)

Gather material for the range from Step 2.

**Merged PRs** (preferred) — derive both merge-time bounds from tags (or the
first-release upper bound only):

```bash
# Upper bound (always): committer date of target_tag, or HEAD if unpublished
upper_ref="$target_tag"   # use HEAD if the target tag is not published yet
upper_date=$(git log -1 --format=%cI "$upper_ref")

# Lower bound (previous-release path only; omit for first release)
lower_date=$(git log -1 --format=%cI "$previous_tag")
```

Search with both bounds when `previous_tag` exists; for a first release use
only `merged:<=${upper_date}`. Read up to **1000** matching PRs
(`--limit 1000`). If the result count equals that limit, split the date window
and merge pages until every matching PR is collected:

```bash
# previous-release path (both bounds); drop merged:>=… for first release
gh pr list --state merged --limit 1000 \
  --search "merged:>=${lower_date} merged:<=${upper_date}" \
  --json number,title,labels,mergedAt,author,body
```

**Verify before drafting:** every collected PR’s `mergedAt` MUST fall within
the intended window (strictly after `previous_tag` when present, and on or
before `upper_ref`). Confirm each PR’s merge commit is reachable in
`previous_tag..target_tag` (or the first-release range). Drop any PR outside
that window.

**Conventional commits** (supplement; do not dump raw):

```bash
# previous-release path
git log "${previous_tag}..${target_tag}" --pretty=format:'%s' --no-merges

# first-release path (no previous_tag)
git log "${target_tag}" --pretty=format:'%s' --no-merges
# or, if target tag unpublished: git log HEAD --pretty=format:'%s' --no-merges
```

Group by type prefix (`feat`, `fix`, `perf`, `refactor`, `docs`, `build`,
`chore`, `ci`, `test`, `bump`, `revert`, `style`). Ignore noise-only chores
unless they affect users (deps with CVE fixes, Go bumps, image tags).

Also skim:

- `COMPATIBILITY.md` — what counts as breaking / upgrade impact
- Notable version-bump or dependency PRs in the range

### Step 4 — Draft the notes

Write notes using the structure in [template.md](template.md).

Rules:

1. **Lead with a short summary** (2–4 sentences): what this release is for and
   who should care.
2. **Always include an Upgrade impact** section (even if “No breaking changes;
   safe to upgrade for most users”).
3. Call out **breaking changes**, API/OpenAPI changes, config/env changes,
   image tag policy changes, and required client/SDK alignment.
4. Prefer **user-facing** language over internal refactors. Link important PRs
   as `(#NNN)`.
5. Omit empty sections rather than leaving placeholders.
6. **Never** paste `git log` / full commit SHA lists / unedited
   `--generate-notes` output as the release body.

### Step 5 — Present for human review

Show the full markdown body to the user. Ask whether to:

- **A.** Only keep the draft (copy/paste later), or
- **B.** Apply it to GitHub.

Do **not** publish or overwrite a release body without explicit confirmation.

### Step 6 — Apply to GitHub (only if confirmed)

Use the validated `target_tag` (e.g. `v1.0.0`) in every command.

If the release **exists**:

```bash
gh release edit "${target_tag}" --notes-file /tmp/evalhub-release-notes.md
```

If the release **does not exist** and the user confirmed **publication** (not a
draft):

```bash
gh release create "${target_tag}" --title "${target_tag}" \
  --notes-file /tmp/evalhub-release-notes.md
```

If the release **does not exist** and the user wants a **draft**:

```bash
gh release create "${target_tag}" --draft --title "${target_tag}" \
  --notes-file /tmp/evalhub-release-notes.md
```

Do not attach binaries here unless the user asks; MCP/asset releases are handled
by `.github/workflows/release-mcp.yml`.

If CI already created a release with auto-generated notes, **replace** the body
with the curated notes (after confirmation). Leaving only `--generate-notes`
output does not meet this skill’s OpenSSF bar.

### Step 7 — Report

Summarize:

- Target version / tag and previous tag
- Whether the GitHub release was updated, left as draft, or draft-only in chat
- Any gaps (missing PR bodies, first release, unclear breaking changes)

## Commit / PR when adding skill-only changes

If this work is only documentation/skill files, commit with `git commit -s`
(DCO sign-off). Do **not** add a `Signed-off-by` line yourself; `-s` appends it
from the author’s configured `user.name` and `user.email`.

Use a Conventional Commits subject (example for the initial skill; adapt the
subject for later skill-only edits):

```text
chore(skills): add release-notes skill for OpenSSF-compliant GitHub releases
```

Append **one** approved AI attribution trailer at the end of the commit message
body (after the subject and any description), for example:

```text
Assisted-by: Cursor
```

or `Made-with: Cursor` / `Generated with: Claude Code` as appropriate. Do not
stack multiple attribution trailers.
