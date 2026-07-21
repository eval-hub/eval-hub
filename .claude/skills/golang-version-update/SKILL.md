---
name: golang-version-update
description: >
  Update the Go version across the repository when a newer version is available
  in the Red Hat UBI9 go-toolset image. Checks registry.access.redhat.com/ubi9/go-toolset
  for supported tags, validates compatibility, and updates go.mod, Containerfile,
  and CI workflows. Use when bumping the Go version, checking for Go updates,
  or remediating Go stdlib CVEs that require a toolchain upgrade.
allowed-tools:
  - Read
  - Edit
  - Write
  - WebFetch
  - Bash(grep *)
  - Bash(find *)
  - Bash(git *)
  - Bash(gh *)
  - Bash(go *)
  - Bash(make *)
  - Bash(curl *)
  - Bash(jq *)
  - Bash(sort *)
  - Bash(skopeo *)
  - Bash(sed *)
---

# Go Version Update Skill

Update the Go toolchain version across the repository, gated on availability
in the Red Hat UBI9 go-toolset container image.

## Procedure

Follow these steps **in order**. Do not skip steps.

### Step 1 — Determine the current Go version

Read `go.mod` and extract the `go` directive (e.g. `go 1.26.4`).
Also note the current go-toolset major tag in `Containerfile` (e.g. `go-toolset:1.26`).

### Step 2 — Check for an existing open PR

Run:

```bash
gh pr list --state open --search "go update OR golang OR go-toolset OR go.mod" --limit 20
```

If an open PR already bumps the Go version, report its number and **stop**.

### Step 3 — Query the go-toolset registry for available tags

Use the Red Hat container catalog API to list available go-toolset tags:

```bash
skopeo list-tags docker://registry.access.redhat.com/ubi9/go-toolset 2>/dev/null \
  | jq -r '.Tags[]' \
  | grep -E '^[0-9]+\.[0-9]+(\.[0-9]+)?(-[0-9]+)?$' \
  | sort -t. -k1,1n -k2,2n -k3,3n \
  | tail -20
```

If `skopeo` is not available, fall back to the Red Hat catalog API:

```bash
curl -s "https://catalog.redhat.com/api/containers/v1/repositories/registry/registry.access.redhat.com/repository/ubi9/go-toolset/tags?page_size=100&page=0" \
  | jq -r '.data[].name' \
  | grep -E '^[0-9]+\.[0-9]+(\.[0-9]+)?(-[0-9]+)?$' \
  | sort -t. -k1,1n -k2,2n -k3,3n \
  | tail -20
```

Identify:

- The **latest major.minor tag** (e.g. `1.26`) — this goes in the Containerfile.
- The **latest major.minor.patch tag** (e.g. `1.26.5`) — this goes in `go.mod` and CI.

### Step 4 — Decide whether to update

Compare the latest available patch version from go-toolset with the current
version in `go.mod`.

- If the current version **matches** the latest available → report "Go version
  is already up to date" and **stop**.
- If a **newer patch** is available within the same major.minor → proceed with
  the update.
- If a **newer major.minor** is available → proceed, and also update the
  Containerfile tag.
- If the user requested a specific version that is **not available** in
  go-toolset → report that the desired version is not yet supported by
  go-toolset, suggest the latest available version, and **stop** (do not
  update go.mod to a version that doesn't exist in go-toolset).

### Step 5 — Update files

Update the following files with the new version. Use the values determined in
Step 3.

#### 5a. `go.mod`

Update the `go` directive to the new patch version:

```text
go 1.XX.Y
```

If there is a commented-out `// toolchain` line, update it to match.

#### 5b. `Containerfile`

Update **all** `go-toolset:` image references to use the new major.minor tag:

```dockerfile
FROM --platform=$BUILDPLATFORM registry.access.redhat.com/ubi9/go-toolset:1.XX AS builder
```

#### 5c. GitHub Actions workflows

Search for **hardcoded** Go version strings in `.github/workflows/`:

```bash
grep -rn 'GOTOOLCHAIN\|GOSECGOVERSION\|go-version:' .github/workflows/
```

Update any hardcoded version strings (e.g. `GOTOOLCHAIN: go1.XX.Y`,
`GOSECGOVERSION: go1.XX.Y`) to the new version.

**Do not** change lines that use `go-version-file: "go.mod"` — those
automatically pick up the version from `go.mod`.

#### 5d. Any other files

Search for other references to the old Go version:

```bash
grep -rn "go1\.<old_minor>\.<old_patch>\|go-toolset:<old_major_minor>" . \
  --include='*.yml' --include='*.yaml' --include='*.json' --include='*.md' \
  --include='*.sh' --include='Makefile' --include='Containerfile' --include='Dockerfile'
```

Update any that are pinned to the old version.

### Step 6 — Update dependencies and run `go mod tidy`

```bash
go get -t -u ./...
go mod tidy
```

### Step 7 — Validate

Run formatting and linting:

```bash
make fmt lint
```

If there are compilation errors, investigate and fix them before proceeding.

### Step 8 — Run tests

Run unit tests to verify nothing is broken:

```bash
make test
```

### Step 9 — Report

Summarize what was updated:

- Previous Go version
- New Go version
- go-toolset tag used
- Files modified
- Any files that reference the Go version but were not updated (and why)
- Test results

If the user wants a PR, create one following conventional commit format:

```text
build(go): bump Go toolchain to 1.XX.Y

Update Go version to 1.XX.Y, matching the latest available
registry.access.redhat.com/ubi9/go-toolset image.

Files updated:
- go.mod
- Containerfile
- .github/workflows/ci.yml
```
