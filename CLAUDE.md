# Agent instructions

@AGENTS.md

Repo-specific architecture notes live in `.claude/rules/`. Maintainer workflows
(`evalhub-bug-review`, `fix-fvt-test`, `golang-toolchain-update`,
`release-notes`) are **not** kept in this repository — they come from the
external [`eval-hub/eval-hub-skills`](https://github.com/eval-hub/eval-hub-skills)
repository (`dev-skills/`), and are published as opt-in plugins. The committed
`.claude/settings.json` declares the `evalhub` marketplace and marks the four
plugins as enabled, but it does not download or install them: a fresh clone has
**none** of the four skills available until each collaborator installs them
explicitly with `claude plugin install <skill>@evalhub --scope project`. Claude
Code may also fetch the enabled plugins in the background once a session starts,
and that fetch needs network access, so install the ones you rely on before you
need them. An install only helps a later offline session once its download has
finished — offline work can use just the plugins that are already downloaded, and
nothing new is fetched without network. (`make install-dev-skills` links the same
skills into `~/.claude/skills` from a local clone for other harnesses.) See
`AGENTS.md` → Development Skills.

## Release notes

Each GitHub release must include human-readable release notes (OpenSSF): a summary
of major changes and upgrade impact — **not** raw `git log` or unedited
`--generate-notes` output.

OpenSSF allows several delivery mechanisms (e.g. `NEWS` / `CHANGELOG` files, or
version-control release UIs). This project uses the **GitHub Releases** workflow
as the primary channel.

Use the `release-notes` dev skill to draft and (after confirmation) apply notes
for a given tag/version:
[`dev-skills/release-notes/SKILL.md`](https://github.com/eval-hub/eval-hub-skills/blob/main/dev-skills/release-notes/SKILL.md).

### Run release notes

- **Claude Code (installed `release-notes@evalhub` plugin):**
  `/release-notes:release-notes` — plugin skills are namespaced
  `<plugin>:<skill>`.
- **Claude Code (local skill from `make install-dev-skills`):** `/release-notes`
- **Cursor / Codex CLI / other harnesses:** ask naturally, e.g. “Generate release
  notes for v1.0.0 using the release-notes skill”, or attach the skill file from
  the clone made by `make install-dev-skills`
  (`@~/.cache/eval-hub-skills/dev-skills/release-notes/SKILL.md`).

### After the release notes skill runs

Expect a curated markdown body with Summary and Upgrade impact. The skill must
not publish or overwrite a GitHub release body without explicit confirmation.

## CVE fixing

### Instructions for CVE fixing

Find any CVEs in the repository dependencies and create a PR with the proposed fix in the repository.

Verify that there is not already an open `PR` that provides this fix, if an open `PR` already
exists then report the `PR` number and skip the rest.

#### Updating the golang version

Before updating to a new golang version check that this version is supported in the go-toolset that can be found here `registry.access.redhat.com/ubi10/go-toolset`. If the new golang version is not yet supported in `registry.access.redhat.com/ubi10/go-toolset` then move to the latest supported version, if possible, and report that the desired version is not yet supported by go-toolset.
The PR should also update the major golang version, if needed, in the Containerfile.

The go.mod must not be updated until the same version exists in go-toolset.

If there are other files in the repository that require updating due to new golang version then mention them in the PR.
Use `go-version-file: "go.mod"` in the github actions where possible.

Use the `golang-toolchain-update` dev skill for the full procedure (govulncheck
scan, go-toolset registry gate, file updates, `go mod tidy`, tests, and PR):
[`dev-skills/golang-toolchain-update/SKILL.md`](https://github.com/eval-hub/eval-hub-skills/blob/main/dev-skills/golang-toolchain-update/SKILL.md).
It replaces the earlier repo-local `golang-version-update` / `golang-cve-update`
skills and resolves the go-toolset image from the `Containerfile`.

Pick the mode:

- `cve` (default) — `govulncheck` reports findings, or the user mentions CVEs,
  vulnerabilities, or security fixes. Bumps to the **minimum** version that
  clears every stdlib and toolchain finding, and only touches modules with
  findings.
- `latest` — the user asks to bump Go, check for Go updates, or refresh the
  toolchain. Bumps to the **newest** patch of the current major.minor (or the
  newest major.minor when requested).

#### Run the Go toolchain update

- **Claude Code:** `/golang-toolchain-update` — the committed
  `.claude/settings.json` enables it from the `evalhub` marketplace, and Claude
  Code fetches it in the background. Install it explicitly with
  `claude plugin install golang-toolchain-update@evalhub --scope project` when you
  need it immediately or before a session that runs offline; in another
  repository, add the marketplace first
  with `/plugin marketplace add eval-hub/eval-hub-skills`.

- **Cursor / Codex CLI / other harnesses:** ask naturally, e.g. “Bump the Go
  version using the golang-toolchain-update skill” / “Check whether we can
  update Go / go-toolset”, or attach the skill file from the clone made by
  `make install-dev-skills`.

#### After the golang update skill runs

Expect a summary (previous/new Go version, matched go-toolset tag, files touched,
test results).

- **CVE-driven Go upgrades:** Follow the CVE-fix flow above — check for an existing open PR that provides the fix; if one exists, report its number and skip the rest; otherwise create a PR with the fix.
- **Non-CVE Go upgrades:** Create a PR only if requested; still skip if an open Go bump PR already exists.

#### npm devDependencies

Ensure that version pinning is correct and pins to a **single version**,
regenerate package-lock.json by running npm install after modifying the overrides in package.json.

If updating any dependencies related to `npm` then verify that the documentation
build still works by running `make documentation`.
If `make documentation` changes any files in the `docs` directory then add them to the `PR`.
