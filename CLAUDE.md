# Agent instructions

@AGENTS.md

## CVE fixing

### Instructions for CVE fixing

Find any CVEs in the repository dependencies and create a PR with the proposed fix in the repository.

Verify that there is not already an open `PR` that provides this fix, if an open `PR` already
exists then report the `PR` number and skip the rest.

#### Updating the golang version

Before updating to a new golang version check that this version is supported in the go-toolset that can be found here `registry.access.redhat.com/ubi9/go-toolset`. If the new golang version is not yet supported in `registry.access.redhat.com/ubi9/go-toolset` then move to the latest supported version, if possible, and report that the desired version is not yet supported by go-toolset.
The PR should also update the major golang version, if needed, in the Containerfile.

The go.mod must not be updated until the same version exists in go-toolset.

If there are other files in the repository that require updating due to new golang version then mention them in the PR.
Use `go-version-file: "go.mod"` in the github actions where possible.

Use the project skill [`.claude/skills/golang-version-update/SKILL.md`](.claude/skills/golang-version-update/SKILL.md)
for the full procedure (registry check, file updates, `go mod tidy`, tests, and PR).

##### Run from Cursor

In Agent chat, ask in natural language, for example:

- “Bump the Go version using the golang-version-update skill”
- “Check whether we can update Go / go-toolset”

Cursor loads the skill from `.claude/skills/golang-version-update/` and follows it.
Optionally attach `@.claude/skills/golang-version-update/SKILL.md` to force the procedure into context.

##### Run from Claude Code terminal

From the repo root, start Claude Code (`claude`), then invoke the skill by name:

```text
/golang-version-update
```

Or ask naturally (the skill auto-matches on its description), for example:

- “Bump Go to the latest version supported by ubi9/go-toolset”

Type `/` in Claude Code to list available skills if needed.

##### After the skill runs

Expect a summary (previous/new Go version, files touched, test results). Create a PR only if
requested, following the CVE-fix flow above (skip if an open Go bump PR already exists).

#### npm devDependencies

Ensure that version pinning is correct and pins to a **single version**,
regenerate package-lock.json by running npm install after modifying the overrides in package.json.

If updating any dependencies related to `npm` then verify that the documentation
build still works by running `make documentation`.
If `make documentation` changes any files in the `docs` directory then add them to the `PR`.
