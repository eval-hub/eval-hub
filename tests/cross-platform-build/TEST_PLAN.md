# Test Plan: Cross-Platform Build & CI/CD (RHOAIENG-60352)

## Overview

This test plan validates the cross-platform build infrastructure for the `evalhub-mcp`
binary across all 5 target platforms, container image builds, release automation,
and distribution channels (GitHub Releases, Homebrew).

**Jira:** [RHOAIENG-60352](https://redhat.atlassian.net/browse/RHOAIENG-60352)
**Binary:** `evalhub-mcp`
**Version:** See `VERSION` file at repo root

## Platforms Under Test

| # | OS      | Architecture | Binary Name                 |
|---|---------|-------------|-----------------------------|
| 1 | linux   | amd64       | `evalhub-mcp-linux-amd64`   |
| 2 | linux   | arm64       | `evalhub-mcp-linux-arm64`   |
| 3 | darwin  | amd64       | `evalhub-mcp-darwin-amd64`  |
| 4 | darwin  | arm64       | `evalhub-mcp-darwin-arm64`  |
| 5 | windows | amd64       | `evalhub-mcp-windows-amd64.exe` |

## Makefile Targets

All test targets are prefixed with `test-mcp-` and defined in the project `Makefile`.
Run the full suite with:

```bash
make test-mcp-cross-platform
```

### Test Matrix

| # | Makefile Target               | What It Tests                                      | Type      |
|---|-------------------------------|----------------------------------------------------|-----------|
| 1 | `test-mcp-build-all`          | All 5 platform binaries compile without errors     | Automated |
| 2 | `test-mcp-binary-info`        | Each binary has correct file type and architecture | Automated |
| 3 | `test-mcp-binary-naming`      | Binaries follow the `evalhub-mcp-{OS}-{ARCH}` convention | Automated |
| 4 | `test-mcp-version`            | `--version` outputs version, commit, and build date | Automated |
| 5 | `test-mcp-no-runtime-deps`    | Binaries are statically linked (no external deps)  | Automated |
| 6 | `test-mcp-container-build`    | Container image builds successfully                | Automated |
| 7 | `test-mcp-container-http`     | Container starts in HTTP mode and responds to MCP initialize | Automated |
| 8 | `test-mcp-checksums`          | SHA256 checksums are generated and match binaries  | Automated |
| 9 | `test-mcp-formula-syntax`     | Homebrew formula is valid Ruby                     | Automated |
| 10 | `test-mcp-cross-platform`    | Runs all tests 1-9 in sequence                     | Automated |

---

## Detailed Test Descriptions

### 1. test-mcp-build-all

**Purpose:** Verify that all 5 platform binaries compile successfully.

**Procedure:**
```bash
make test-mcp-build-all
```

**Pass Criteria:**
- `make build-all-platforms-mcp` exits with code 0
- All 5 binaries exist in `bin/`:
  - `bin/evalhub-mcp-linux-amd64`
  - `bin/evalhub-mcp-linux-arm64`
  - `bin/evalhub-mcp-darwin-amd64`
  - `bin/evalhub-mcp-darwin-arm64`
  - `bin/evalhub-mcp-windows-amd64.exe`

---

### 2. test-mcp-binary-info

**Purpose:** Confirm each binary has the expected file type and target architecture using `file(1)`.

**Procedure:**
```bash
make test-mcp-binary-info
```

**Pass Criteria:**
- `linux-amd64`: ELF 64-bit, x86-64
- `linux-arm64`: ELF 64-bit, aarch64
- `darwin-amd64`: Mach-O 64-bit x86_64
- `darwin-arm64`: Mach-O 64-bit arm64
- `windows-amd64.exe`: PE32+ executable, x86-64

---

### 3. test-mcp-binary-naming

**Purpose:** Verify binaries follow the required naming convention: `evalhub-mcp-{OS}-{ARCH}`.

**Procedure:**
```bash
make test-mcp-binary-naming
```

**Pass Criteria:**
- Each binary in `bin/` matches the pattern `evalhub-mcp-{OS}-{ARCH}[.exe]`
- No unexpected files exist matching `evalhub-mcp-*`
- Exactly 5 platform binaries are present

---

### 4. test-mcp-version

**Purpose:** Verify `--version` outputs correct build metadata (version, git hash, build date).

**Procedure:**
```bash
make test-mcp-version
```

**Precondition:** Requires a native-platform binary. The target builds for the current host platform.

**Pass Criteria:**
- Output contains `evalhub-mcp version` followed by the version from `VERSION` file
- Output contains `build:` with the version string
- Output contains `commit:` with a git short hash
- Output contains `built:` with a date string

---

### 5. test-mcp-no-runtime-deps

**Purpose:** Verify binaries are statically linked (`CGO_ENABLED=0`) and require no external runtime dependencies.

**Procedure:**
```bash
make test-mcp-no-runtime-deps
```

**Pass Criteria:**
- Linux binaries: `file` output contains `statically linked`
- All binaries built with `CGO_ENABLED=0` (no dynamic libc dependency)

---

### 6. test-mcp-container-build

**Purpose:** Verify the container image builds successfully from `Containerfile`.

**Procedure:**
```bash
make test-mcp-container-build
```

**Pass Criteria:**
- Container build exits with code 0
- Image `evalhub-mcp-test:latest` exists in the local container runtime
- Image contains `/app/evalhub-mcp` binary

---

### 7. test-mcp-container-http

**Purpose:** Verify the container starts in HTTP mode and responds to an MCP initialize request.

**Procedure:**
```bash
make test-mcp-container-http
```

**Pass Criteria:**
- Container starts and binds to port 3001
- An HTTP POST to `http://localhost:3001/mcp` with `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}` returns a valid JSON-RPC response
- Response contains `serverInfo` field
- Container stops cleanly on SIGTERM

---

### 8. test-mcp-checksums

**Purpose:** Verify SHA256 checksums are generated and match the binary contents.

**Procedure:**
```bash
make test-mcp-checksums
```

**Pass Criteria:**
- `bin/checksums-sha256.txt` is generated
- Contains one checksum entry per platform binary (5 entries)
- `sha256sum --check` verifies all checksums pass

---

### 9. test-mcp-formula-syntax

**Purpose:** Verify the Homebrew formula is syntactically valid Ruby.

**Procedure:**
```bash
make test-mcp-formula-syntax
```

**Pass Criteria:**
- `ruby -c formula/evalhub-mcp.rb` exits with code 0 (syntax OK)
- Formula file references all 4 non-Windows platforms (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64)
- Formula test block includes `--version` assertion

---

### 10. test-mcp-cross-platform (combined)

**Purpose:** Run all automated tests in sequence.

**Procedure:**
```bash
make test-mcp-cross-platform
```

**Pass Criteria:** All individual tests pass.

---

## Manual Verification Checklist

These tests require access to physical or virtual machines running the target OS.
They cannot be automated via Makefile targets.

| # | Platform              | Steps                                               | Expected Result |
|---|-----------------------|-----------------------------------------------------|-----------------|
| 1 | macOS 14 (Apple Silicon) | Download `evalhub-mcp-darwin-arm64`, `chmod +x`, run `--version` | Version output shown |
| 2 | macOS 14 (Intel)      | Download `evalhub-mcp-darwin-amd64`, `chmod +x`, run `--version` | Version output shown |
| 3 | RHEL 9                | Download `evalhub-mcp-linux-amd64`, `chmod +x`, run `--version` | Version output shown |
| 4 | Ubuntu 22.04          | Download `evalhub-mcp-linux-amd64`, `chmod +x`, run `--version` | Version output shown |
| 5 | Windows 11            | Download `evalhub-mcp-windows-amd64.exe`, run `--version` in CMD/PowerShell | Version output shown |

---

## CI Integration

These test targets integrate into the existing CI pipeline:

- **ci-mcp.yml:** Add `make test-mcp-build-all test-mcp-binary-info test-mcp-binary-naming` after the `build-platforms` job
- **release-mcp.yml:** Add `make test-mcp-checksums` after checksum generation
- **Pre-merge gate:** `make test-mcp-cross-platform` can run as a required check on PRs touching `cmd/evalhub_mcp/`, `internal/evalhub_mcp/`, or `Makefile`
