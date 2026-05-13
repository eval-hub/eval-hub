# EvalHub MCP Server — VS Code & Cursor Integration Test Plan

**JIRA**: [RHOAIENG-60353](https://redhat.atlassian.net/browse/RHOAIENG-60353)
**Parent**: [RHAISTRAT-1381](https://redhat.atlassian.net/browse/RHAISTRAT-1381) (Requirements S7, S9, S10)
**Date**: 2026-05-13
**Author**: AI-generated from JIRA requirements
**Status**: Draft

---

## 0. Automated Test Scripts

An executable test harness is provided alongside this plan in `test-scripts/`. It communicates directly with the evalhub-mcp binary over both transports using the MCP JSON-RPC protocol.

### Quick Start

```bash
# 1. Configure
cp test-scripts/test.env.example test-scripts/test.env
vi test-scripts/test.env  # fill in binary path, token, tenant, backend URL, test data IDs

# 2. Generate client configs (VS Code + Cursor, stdio + HTTP)
./test-scripts/generate_client_configs.sh

# 3. Run all tests — stdio transport
./test-scripts/run_tests.sh stdio

# 4. Start HTTP server, then run HTTP tests
evalhub-mcp mcp --transport http --host localhost --port 3001
./test-scripts/run_tests.sh http

# 5. Run both transports
./test-scripts/run_tests.sh

# 6. Run a single suite (e.g., resources only, stdio)
./test-scripts/run_tests.sh stdio 02

# 7. Run multiple suites (e.g., tools + errors, HTTP)
./test-scripts/run_tests.sh http 03,06
```

### Script → Test Case Mapping

| Script | Suite | Test Cases |
|--------|-------|------------|
| `tests/01_server_discovery.sh` | Server Discovery | SD-01 to SD-04 |
| `tests/02_resources.sh` | MCP Resources | RES-01 to RES-09, RES-ALL |
| `tests/03_tools.sh` | MCP Tools | TOOL-01 to TOOL-07 |
| `tests/04_prompts.sh` | MCP Prompts | PRM-01 to PRM-06 |
| `tests/05_autocompletion.sh` | Autocompletion | AC-01 to AC-03 |
| `tests/06_error_scenarios.sh` | Error Scenarios | ERR-01 to ERR-06 |
| `tests/07_e2e_workflow.sh` | E2E Workflows | E2E-01 to E2E-03 |
| `tests/08_client_specific.sh` | Client-Specific | CS-01 to CS-04 |

### Output

- Console: color-coded PASS/FAIL/SKIP per test
- JUnit XML: `test-scripts/reports/test-results-*.xml` (CI-compatible)
- Cross-transport diff: `test-scripts/reports/parity_{stdio,http}.txt`
- Prompt fidelity diff: `test-scripts/reports/prompt_fidelity_{stdio,http}.json`
- Generated client configs: `test-scripts/reports/client-configs/`

---

## 1. Scope

This test plan covers end-to-end integration verification of the **evalhub-mcp** Go binary with:

| Client | MCP Extension / Mechanism | Transports |
|--------|---------------------------|------------|
| **VS Code** | GitHub Copilot MCP extension | stdio, HTTP/SSE |
| **Cursor** | Built-in MCP support | stdio, HTTP/SSE |

### Out of Scope
- Claude Code integration (covered separately in RHOAIENG-60353 comment)
- Backend EvalHub API correctness (assumed functional)
- Binary cross-platform compilation (covered by CI)

---

## 2. Prerequisites

| Item | Details |
|------|---------|
| evalhub-mcp binary | Built for test platform (darwin/arm64, darwin/amd64, linux/amd64) |
| EvalHub backend | Running and accessible with valid API endpoint |
| Auth token | Valid `EVALHUB_TOKEN` set in environment or config file |
| Tenant | Valid `EVALHUB_TENANT` configured |
| VS Code | Version 1.96+ with GitHub Copilot and GitHub Copilot MCP extension installed |
| Cursor | Version 0.45+ with MCP support enabled |
| Test data | At least 1 provider, 2+ benchmarks (including one with `rag` label), 1 collection, and 1 completed job in the backend |

---

## 3. Test Environment Setup

### 3.1 VS Code — stdio Transport

Add to `.vscode/settings.json` or VS Code User Settings:

```json
{
  "github.copilot.chat.mcp.servers": {
    "evalhub": {
      "type": "stdio",
      "command": "/path/to/evalhub-mcp",
      "args": ["mcp"],
      "env": {
        "EVALHUB_TOKEN": "<token>",
        "EVALHUB_TENANT": "<tenant>",
        "EVALHUB_BASE_URL": "https://evalhub.example.com"
      }
    }
  }
}
```

### 3.2 VS Code — HTTP/SSE Transport

Start the server manually:
```bash
evalhub-mcp mcp --transport http --host localhost --port 3001
```

Add to VS Code settings:
```json
{
  "github.copilot.chat.mcp.servers": {
    "evalhub": {
      "type": "sse",
      "url": "http://localhost:3001/sse"
    }
  }
}
```

### 3.3 Cursor — stdio Transport

Add to `.cursor/mcp.json` (project-level) or `~/.cursor/mcp.json` (global):

```json
{
  "mcpServers": {
    "evalhub": {
      "command": "/path/to/evalhub-mcp",
      "args": ["mcp"],
      "env": {
        "EVALHUB_TOKEN": "<token>",
        "EVALHUB_TENANT": "<tenant>",
        "EVALHUB_BASE_URL": "https://evalhub.example.com"
      }
    }
  }
}
```

### 3.4 Cursor — HTTP/SSE Transport

Start the server manually (same as VS Code):
```bash
evalhub-mcp mcp --transport http --host localhost --port 3001
```

Add to Cursor MCP config:
```json
{
  "mcpServers": {
    "evalhub": {
      "url": "http://localhost:3001/sse"
    }
  }
}
```

---

## 4. Test Cases

### Legend
- **P0** = Must pass for release (blocking)
- **P1** = Should pass (important)
- **P2** = Nice to verify (informational)

---

### 4.1 Server Discovery & Connection

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| SD-01 | P0 | stdio server launches on client start | 1. Configure stdio transport. 2. Open client. 3. Open MCP panel/chat. | Server process starts; client shows "evalhub" as connected MCP server. | [ ] | [ ] |
| SD-02 | P0 | HTTP server connects from client | 1. Start evalhub-mcp in HTTP mode. 2. Configure client for SSE. 3. Open MCP panel/chat. | Client connects to running server; shows "evalhub" as available. | [ ] | [ ] |
| SD-03 | P0 | Server metadata visible | 1. Connect via either transport. 2. Inspect server info in client MCP panel. | Server name, version, and capabilities listed. | [ ] | [ ] |
| SD-04 | P1 | Reconnection after server restart | 1. Connect via HTTP. 2. Kill and restart the server process. 3. Retry a tool call. | Client reconnects or prompts user; tool call succeeds after reconnect. | [ ] | [ ] |

---

### 4.2 MCP Resources

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| RES-01 | P0 | List all providers | Ask agent: "List all EvalHub providers" | Returns provider list via `evalhub://providers` resource. | [ ] | [ ] |
| RES-02 | P0 | List all benchmarks | Ask agent: "Show me all available benchmarks" | Returns benchmark list via `evalhub://benchmarks`. | [ ] | [ ] |
| RES-03 | P1 | Filter benchmarks by label | Ask agent: "Show benchmarks labeled 'rag'" | Returns filtered benchmarks via `evalhub://benchmarks?label=rag`. Only rag-labeled benchmarks returned. | [ ] | [ ] |
| RES-04 | P0 | List collections | Ask agent: "List evaluation collections" | Returns collection list via `evalhub://collections`. | [ ] | [ ] |
| RES-05 | P0 | List jobs | Ask agent: "Show my evaluation jobs" | Returns job list via `evalhub://jobs`. | [ ] | [ ] |
| RES-06 | P1 | Filter jobs by status | Ask agent: "Show running jobs" | Returns filtered jobs via `evalhub://jobs?status=running`. | [ ] | [ ] |
| RES-07 | P1 | Get experiment results | Ask agent: "Show results for experiment X" | Returns experiment results via `evalhub://experiments/{id}/results`. | [ ] | [ ] |
| RES-08 | P1 | Get server version | Ask agent: "What version of the evalhub server is running?" | Returns version/build metadata via `evalhub://version`. | [ ] | [ ] |
| RES-09 | P0 | Get individual item by ID | Ask agent: "Get details for benchmark {id}" | Returns single benchmark entity with full detail. | [ ] | [ ] |

**Repeat all RES-* tests for both stdio and HTTP transports.**

---

### 4.3 MCP Tools

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| TOOL-01 | P0 | Tool discovery | Open MCP tools panel in client. | All 3 tools visible: `submit_evaluation`, `cancel_job`, `get_job_status`. Each has parameter schema, description, and examples. | [ ] | [ ] |
| TOOL-02 | P0 | submit_evaluation — basic | Ask agent: "Submit an evaluation using benchmark X against model endpoint Y" | Tool invoked with correct params. Job submitted. Job ID returned. | [ ] | [ ] |
| TOOL-03 | P1 | submit_evaluation — with collection | Ask agent: "Run collection Z against model endpoint Y" | Tool invoked with collection reference. Job created successfully. | [ ] | [ ] |
| TOOL-04 | P1 | submit_evaluation — full params | Ask agent to submit with name, description, tags, and experiment config. | All optional fields passed through. Job created with metadata visible in backend. | [ ] | [ ] |
| TOOL-05 | P0 | get_job_status | After submitting a job, ask: "What's the status of job {id}?" | Returns structured status with progress information. | [ ] | [ ] |
| TOOL-06 | P0 | cancel_job | Ask agent: "Cancel job {id}" | Job cancelled. Status confirmed as cancelled. | [ ] | [ ] |
| TOOL-07 | P1 | Tool parameter validation | Invoke submit_evaluation with missing required fields. | Agent receives clear error; does not crash. Error message is actionable. | [ ] | [ ] |

**Repeat all TOOL-* tests for both stdio and HTTP transports.**

---

### 4.4 MCP Prompts

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| PRM-01 | P0 | Prompt discovery | Open MCP prompts panel in client. | All 3 prompts visible: `edd_workflow`, `evaluate_model`, `compare_runs`. Each shows description and arguments. | [ ] | [ ] |
| PRM-02 | P0 | edd_workflow — RAG type | Invoke edd_workflow prompt with `app_type=rag`. | Agent receives structured EDD guidance (Define, Measure, Iterate) tailored to RAG applications. | [ ] | [ ] |
| PRM-03 | P1 | edd_workflow — other types | Invoke edd_workflow with `app_type=agent`, then `safety`, then `classifier`. | Guidance adapts to each application type. | [ ] | [ ] |
| PRM-04 | P0 | evaluate_model | Invoke evaluate_model prompt. | Agent walks through model URL collection, benchmark selection, and job submission steps. | [ ] | [ ] |
| PRM-05 | P0 | compare_runs | Invoke compare_runs prompt with 2+ job/experiment IDs. | Agent guides comparison of evaluation results across runs. | [ ] | [ ] |
| PRM-06 | P1 | Prompt rendering fidelity | Compare prompt output between VS Code and Cursor for the same prompt. | Content and structure are identical across clients. | [ ] | [ ] |

**Repeat all PRM-* tests for both stdio and HTTP transports.**

---

### 4.5 Autocompletion

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| AC-01 | P1 | Resource URI autocompletion | In chat, start typing a resource URI (e.g., `evalhub://bench`). | Client offers completion suggestions for resource URIs. | [ ] | [ ] |
| AC-02 | P1 | Parameter autocompletion | Start typing a label filter value. | Client suggests available label values. | [ ] | [ ] |
| AC-03 | P2 | Tool argument autocompletion | When agent is constructing a tool call, verify argument names and types are suggested. | Agent correctly auto-fills parameter schemas from tool definitions. | [ ] | [ ] |

> **Note**: Autocompletion behavior is client-dependent. Document any differences between VS Code and Cursor in the results.

---

### 4.6 Error Scenarios

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| ERR-01 | P0 | Server not running (HTTP) | 1. Configure HTTP transport. 2. Do NOT start the server. 3. Try to use a tool. | Client shows actionable error: "Cannot connect to evalhub server at localhost:3001" or similar. No silent failure. | [ ] | [ ] |
| ERR-02 | P0 | Invalid auth token | 1. Set `EVALHUB_TOKEN` to an invalid value. 2. Try to list providers. | Clear authentication error surfaced to user. Not a generic 500. | [ ] | [ ] |
| ERR-03 | P1 | Backend unreachable | 1. Set `EVALHUB_BASE_URL` to unreachable host. 2. Start server (should succeed). 3. Try to list resources. | Server starts normally. Operations return connectivity error with the target URL. | [ ] | [ ] |
| ERR-04 | P1 | Invalid binary path (stdio) | 1. Configure stdio with non-existent binary path. 2. Open client. | Client shows error that server binary was not found. | [ ] | [ ] |
| ERR-05 | P1 | Server crash mid-operation | 1. Start server in HTTP mode. 2. Kill server process during a long-running operation. | Client reports connection lost. Does not hang indefinitely. | [ ] | [ ] |
| ERR-06 | P2 | Expired auth token mid-session | 1. Start with valid token. 2. Invalidate/expire token server-side. 3. Make a new request. | Error message indicates auth failure; suggests re-authentication. | [ ] | [ ] |

---

### 4.7 End-to-End Workflow (Golden Path)

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| E2E-01 | P0 | Full EDD cycle | In a single chat session: 1. Discover benchmarks labeled "rag". 2. Submit evaluation against a model endpoint. 3. Monitor job status. 4. Review experiment results. 5. Compare with a previous run using `compare_runs` prompt. 6. Cancel the job. | Each step completes successfully. Agent maintains context across the full workflow. | [ ] | [ ] |
| E2E-02 | P1 | Multi-tool orchestration | Ask agent: "Find all RAG benchmarks, pick the first one, and submit an evaluation against endpoint X." | Agent chains resource reads and tool calls in sequence without manual intervention. | [ ] | [ ] |
| E2E-03 | P1 | Prompt-to-action flow | Invoke `evaluate_model` prompt and follow through to job submission. | Prompt guides the agent to collect inputs and execute the submission tool. | [ ] | [ ] |

---

### 4.8 Client-Specific Behavior

| ID | Priority | Test Case | Steps | Expected Result | VS Code | Cursor |
|----|----------|-----------|-------|-----------------|---------|--------|
| CS-01 | P1 | Transport payload parity | Run the same tool call via stdio and HTTP in the same client. Compare responses. | Payloads are identical regardless of transport. | [ ] | [ ] |
| CS-02 | P2 | Concurrent sessions | Open 2 chat windows, both using the same MCP server. | Both sessions can use tools/resources without interference. | [ ] | [ ] |
| CS-03 | P2 | Client restart recovery | 1. Connect to server. 2. Close and reopen client. | Server reconnects (HTTP) or relaunches (stdio) without manual reconfiguration. | [ ] | [ ] |
| CS-04 | P2 | Large response handling | Request a resource with 100+ items. | Client renders the full response without truncation or crash. | [ ] | [ ] |

---

## 5. Test Matrix Summary

| Category | Test Cases | P0 | P1 | P2 | Total Checks (x2 clients x2 transports) |
|----------|-----------|----|----|----|-----------------------------------------|
| Server Discovery | 4 | 3 | 1 | 0 | 16 |
| Resources | 9 | 5 | 4 | 0 | 36 |
| Tools | 7 | 4 | 3 | 0 | 28 |
| Prompts | 6 | 4 | 2 | 0 | 24 |
| Autocompletion | 3 | 0 | 2 | 1 | 12 |
| Error Scenarios | 6 | 2 | 3 | 1 | 24 |
| E2E Workflow | 3 | 1 | 2 | 0 | 12 |
| Client-Specific | 4 | 0 | 1 | 3 | 16 |
| **Totals** | **42** | **19** | **18** | **5** | **168** |

---

## 6. Known Limitations to Investigate

Document findings for each during test execution:

1. **Autocompletion support** — GitHub Copilot MCP extension and Cursor may differ in how they surface completions for resource URIs
2. **Prompt template rendering** — Verify both clients render MCP prompt arguments UI correctly
3. **SSE reconnection** — Behavior when HTTP/SSE connection drops may vary by client
4. **Rate limiting** — Whether clients implement any request throttling to the MCP server
5. **stdio stderr handling** — How each client surfaces server-side log output
6. **Tool approval UX** — Whether clients prompt users before executing tools (Cursor has explicit tool approval; VS Code Copilot may differ)
7. **MCP protocol version** — Confirm both clients support the MCP protocol version used by evalhub-mcp

---

## 7. Exit Criteria

### Pass
- All **P0** tests pass on both clients across both transports
- No data loss or silent failures observed
- Setup commands documented and verified as copy-paste ready

### Conditional Pass
- All P0 pass; some P1 fail with documented workarounds or client-specific limitations logged

### Fail
- Any P0 test fails on either client
- Server crashes or hangs during normal operation
- Error messages are missing or non-actionable

---

## 8. Deliverables

| Deliverable | Location |
|-------------|----------|
| Completed test checklist (this document with checkboxes filled) | Attach to RHOAIENG-60353 |
| Copy-paste setup commands (VS Code stdio, VS Code HTTP, Cursor stdio, Cursor HTTP) | Project documentation site |
| Screenshots of working integration (both clients) | Attach to RHOAIENG-60353 |
| Known limitations and client-specific behaviors | Project documentation site |
| Bug tickets for any failures | Linked to RHOAIENG-60353 |
