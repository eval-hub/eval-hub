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
2. Use that as the target; the GitHub tag form is `v` + version (e.g. `v1.0.0`).

Normalize:

- **Version:** `X.Y.Z` (no `v`)
- **Tag:** `vX.Y.Z`

Confirm the tag exists locally or on the remote (`git fetch --tags` if needed):

```bash
git rev-parse --verify "refs/tags/vX.Y.Z^{}"
```

If the tag does not exist yet, still draft notes for the range
`previous_tag..HEAD` (or `previous_tag..main`) and say the release is not
published.

### Step 2 — Find the previous release tag

```bash
git tag --list 'v*' --sort=-v:refname
```

Pick the highest SemVer tag **strictly older** than the target. If none,
treat the range as the full history to the target (first release) and say so.

### Step 3 — Collect change inputs (not the notes)

Gather material between `previous_tag` and `target_tag` (or `HEAD`):

**Merged PRs** (preferred):

```bash
gh pr list --state merged --limit 200 \
  --search "merged:>=YYYY-MM-DD" \
  --json number,title,labels,mergedAt,author,body
```

Prefer filtering by merge time of the previous tag when possible:

```bash
git log -1 --format=%cI previous_tag
```

**Conventional commits** (supplement; do not dump raw):

```bash
git log previous_tag..target_tag --pretty=format:'%s' --no-merges
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

If the release **exists**:

```bash
gh release edit "vX.Y.Z" --notes-file /tmp/evalhub-release-notes.md
```

If the release **does not exist** and the user wants a **draft**:

```bash
gh release create "vX.Y.Z" --draft --title "vX.Y.Z" \
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

If this work is only documentation/skill files, use:

```text
chore(skills): add release-notes skill for OpenSSF-compliant GitHub releases
```
