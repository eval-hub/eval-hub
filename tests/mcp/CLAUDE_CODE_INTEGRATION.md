# Claude Code Integration for evalhub-mcp — Detailed Steps & Test Plan

**JIRA**: [RHOAIENG-60353](https://redhat.atlassian.net/browse/RHOAIENG-60353)
**Parent**: [RHAISTRAT-1381](https://redhat.atlassian.net/browse/RHAISTRAT-1381) (Requirements section 7)

---

## Prerequisites

Before starting integration verification, ensure:

1. **evalhub-mcp binary** is built and available on your `PATH` (or note the absolute path).
2. **Claude Code CLI** is installed and authenticated (`claude --version` to confirm).
3. **EvalHub backend** is running and reachable (or you have credentials for a remote instance).
4. **Environment variables** are set for authentication:

   ```bash
   export EVALHUB_BASE_URL="https://<evalhub-api-host>"
   export EVALHUB_TOKEN="<your-token>"
   export EVALHUB_TENANT="<your-tenant>"
   ```

   For dev/self-signed TLS environments, you may also need the `--insecure` flag on the binary.

---

## Part 1: stdio Transport Integration

### Step 1 — Register the MCP Server in Claude Code (stdio)

```bash
claude mcp add evalhub --transport stdio -- /path/to/evalhub-mcp
```

> Replace `/path/to/evalhub-mcp` with the actual binary path (e.g., `$HOME/bin/evalhub-mcp`, or just `evalhub-mcp` if it's on `PATH`).

**What this does**: Tells Claude Code to launch `evalhub-mcp` as a subprocess using stdio transport whenever it needs the evalhub MCP server. Claude Code manages the process lifecycle.

### Step 2 — Verify Server Launches Correctly

```bash
claude mcp list
```

**Expected**: `evalhub` appears in the list with transport `stdio` and status showing it is available.

If issues occur, verify the binary works standalone:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}' | /path/to/evalhub-mcp
```

This should return a JSON-RPC response with server capabilities (tools, resources, prompts).

### Step 3 — Verify MCP Resources Are Accessible

Open a Claude Code conversation and test resource discovery:

```text
List all available MCP resources from the evalhub server.
```

**Expected resources** (using `evalhub://` URI scheme):

| Resource | URI Pattern | Description |
|---|---|---|
| Providers | `evalhub://providers` / `evalhub://providers/{id}` | List/get evaluation providers |
| Benchmarks | `evalhub://benchmarks` / `evalhub://benchmarks/{id}` | List/get benchmarks (supports label filtering: rag, safety, agents) |
| Collections | `evalhub://collections` / `evalhub://collections/{id}` | List/get benchmark collections |
| Jobs | `evalhub://jobs` / `evalhub://jobs/{id}` | List/get evaluation jobs (supports status filtering) |
| Experiment Results | `evalhub://experiments/{id}/results` | Retrieve experiment results |
| Server Version | `evalhub://server/version` | Server version and build metadata |

**Verification commands** to try in conversation:

```text
Read the resource evalhub://providers
```

```text
Read the resource evalhub://benchmarks
```

```text
Read the resource evalhub://server/version
```

### Step 4 — Verify MCP Tools Are Accessible

Ask Claude Code to list available tools:

```text
What evalhub tools are available?
```

**Expected tools**:

| Tool | Parameters | Description |
|---|---|---|
| `submit_evaluation` | `benchmarks` or `collection`, `model`, `name`, `description`, `tags`, `experiment_config` | Submit an evaluation job |
| `cancel_job` | `job_id` | Cancel or hard-delete a running job |
| `get_job_status` | `job_id` | Poll status of a submitted job (includes progress info) |

**Functional test** — submit and track a job:

```text
Use the submit_evaluation tool to evaluate model "my-model-endpoint" using the "safety" benchmarks collection. Name it "claude-code-stdio-test".
```

Then follow up:

```text
Check the status of the job you just submitted using get_job_status.
```

### Step 5 — Verify MCP Prompts Are Accessible

Test each registered prompt:

```text
Use the edd_workflow prompt for application type "rag"
```

```text
Use the evaluate_model prompt
```

```text
Use the compare_runs prompt
```

**Expected prompts**:

| Prompt | Arguments | Description |
|---|---|---|
| `edd_workflow` | `application_type` (rag, agent, safety, classifier) | Guides through the Evaluation-Driven Development cycle: Define, Measure, Iterate |
| `evaluate_model` | model URL, benchmark preferences | Step-by-step guidance to evaluate a model end-to-end |
| `compare_runs` | job IDs or experiment IDs | Guides comparison of two or more evaluation runs |

### Step 6 — Verify Autocompletion

In a Claude Code conversation, type a partial resource URI and verify that autocompletion suggests valid completions:

- Type `evalhub://bench` — should suggest `evalhub://benchmarks` and `evalhub://benchmarks/{id}`
- Type `evalhub://jobs/` — should suggest available job IDs
- Type `evalhub://providers/` — should suggest available provider IDs

**Note**: Autocompletion behavior depends on Claude Code's MCP client implementation. Document any limitations observed.

---

## Part 2: HTTP/SSE Transport Integration

### Step 7 — Start the evalhub-mcp Server in HTTP Mode

In a separate terminal:

```bash
evalhub-mcp --transport http --host localhost --port 3001
```

**Expected**: Server starts and logs indicate it is listening on `http://localhost:3001`.

If using a dev environment with self-signed certs to the EvalHub backend:

```bash
evalhub-mcp --transport http --host localhost --port 3001 --insecure
```

### Step 8 — Register the MCP Server in Claude Code (HTTP)

First remove the stdio registration if still active, then add HTTP:

```bash
claude mcp remove evalhub
claude mcp add evalhub --transport http http://localhost:3001
```

### Step 9 — Verify Connection

```bash
claude mcp list
```

**Expected**: `evalhub` appears with transport `http` and shows connected status.

### Step 10 — Repeat All Functional Verification (Steps 3-6)

Run the same verification steps as stdio transport:

- [ ] Resources accessible (`evalhub://providers`, `evalhub://benchmarks`, `evalhub://server/version`, etc.)
- [ ] Tools accessible and functional (`submit_evaluation`, `cancel_job`, `get_job_status`)
- [ ] Prompts accessible (`edd_workflow`, `evaluate_model`, `compare_runs`)
- [ ] Autocompletion works for parameterized resource URIs

Both transports must produce identical results for all tool, resource, and prompt payloads.

---

## Part 3: Error Scenario Testing

### Step 11 — Server Not Running (HTTP)

1. Stop the `evalhub-mcp` HTTP server process.
2. Attempt to use a tool or read a resource from Claude Code.

**Expected**: Claude Code displays an actionable error indicating the server is unreachable (not a cryptic stack trace).

### Step 12 — Invalid Authentication

1. Set an invalid token:

   ```bash
   export EVALHUB_TOKEN="invalid-token-12345"
   ```

2. Restart the MCP server (stdio: re-register; HTTP: restart the process).
3. Try to list resources or call a tool.

**Expected**: The server returns an authentication error that Claude Code surfaces clearly.

### Step 13 — Backend Unreachable

1. Set an invalid backend URL:

   ```bash
   export EVALHUB_BASE_URL="https://nonexistent.example.com"
   ```

2. Restart the MCP server.
3. Verify the server still starts (requirement: server starts even without a reachable backend).
4. Try to call a tool or read a resource.

**Expected**: Server starts successfully. Operations that require the backend return actionable errors about connectivity.

---

## Part 4: End-to-End Workflow Test

### Step 14 — Full EDD Workflow via Claude Code

This is the "golden path" integration test. In a single Claude Code conversation:

1. **Discover**: "List all available benchmarks labeled 'rag' from evalhub."
2. **Submit**: "Submit an evaluation using the rag benchmarks collection against model `https://my-model.example.com/v1` — name it 'edd-integration-test'."
3. **Monitor**: "Check the status of the job you just submitted."
4. **Review**: "Show me the experiment results for that job." (once complete)
5. **Compare**: "Use the compare_runs prompt to compare this run with job `<previous-job-id>`."
6. **Clean up**: "Cancel the job if it's still running."

**Expected**: All steps complete without errors. Claude Code correctly chains tool calls and resource reads to support the workflow.

---

## Test Results Checklist

| # | Test Case | Transport | Pass/Fail | Notes |
|---|---|---|---|---|
| 1 | Server registered and launches | stdio | | |
| 2 | Server registered and connects | HTTP | | |
| 3 | `evalhub://providers` readable | stdio | | |
| 4 | `evalhub://providers` readable | HTTP | | |
| 5 | `evalhub://benchmarks` readable | stdio | | |
| 6 | `evalhub://benchmarks` readable | HTTP | | |
| 7 | `evalhub://benchmarks` label filtering | stdio | | |
| 8 | `evalhub://benchmarks` label filtering | HTTP | | |
| 9 | `evalhub://collections` readable | stdio | | |
| 10 | `evalhub://collections` readable | HTTP | | |
| 11 | `evalhub://jobs` readable | stdio | | |
| 12 | `evalhub://jobs` readable | HTTP | | |
| 13 | `evalhub://jobs` status filtering | stdio | | |
| 14 | `evalhub://jobs` status filtering | HTTP | | |
| 15 | `evalhub://experiments/{id}/results` readable | stdio | | |
| 16 | `evalhub://experiments/{id}/results` readable | HTTP | | |
| 17 | `evalhub://server/version` readable | stdio | | |
| 18 | `evalhub://server/version` readable | HTTP | | |
| 19 | `submit_evaluation` tool works | stdio | | |
| 20 | `submit_evaluation` tool works | HTTP | | |
| 21 | `get_job_status` tool works | stdio | | |
| 22 | `get_job_status` tool works | HTTP | | |
| 23 | `cancel_job` tool works | stdio | | |
| 24 | `cancel_job` tool works | HTTP | | |
| 25 | `edd_workflow` prompt works | stdio | | |
| 26 | `edd_workflow` prompt works | HTTP | | |
| 27 | `evaluate_model` prompt works | stdio | | |
| 28 | `evaluate_model` prompt works | HTTP | | |
| 29 | `compare_runs` prompt works | stdio | | |
| 30 | `compare_runs` prompt works | HTTP | | |
| 31 | Resource URI autocompletion | stdio | | |
| 32 | Resource URI autocompletion | HTTP | | |
| 33 | Error: server not running | HTTP | | |
| 34 | Error: invalid auth token | both | | |
| 35 | Error: backend unreachable | both | | |
| 36 | Full EDD workflow end-to-end | stdio | | |
| 37 | Full EDD workflow end-to-end | HTTP | | |

---

## Copy-Paste Setup Commands (for Documentation)

### stdio (Recommended for Individual Developer Use)

```bash
# Install (macOS via Homebrew)
brew install evalhub-mcp

# Or download binary from GitHub Releases
# https://github.com/<org>/evalhub-mcp/releases/latest

# Configure environment
export EVALHUB_BASE_URL="https://<your-evalhub-instance>"
export EVALHUB_TOKEN="<your-token>"
export EVALHUB_TENANT="<your-tenant>"

# Register with Claude Code
claude mcp add evalhub --transport stdio -- evalhub-mcp

# Verify
claude mcp list
```

### HTTP/SSE (For Shared/Team Deployments)

```bash
# Start the MCP server
evalhub-mcp --transport http --host localhost --port 3001

# In another terminal, register with Claude Code
claude mcp add evalhub --transport http http://localhost:3001

# Verify
claude mcp list
```

---

## Known Limitations / Client-Specific Behaviors to Document

- [ ] Does Claude Code support MCP resource autocompletion natively, or does it rely on the server's `completion/complete` handler?
- [ ] How does Claude Code handle server crashes mid-conversation for stdio transport? (Does it auto-restart?)
- [ ] Are there rate limits or timeout behaviors specific to Claude Code's MCP client?
- [ ] Does Claude Code correctly render multi-step prompt templates from MCP Prompts?
- [ ] Document any differences in behavior between stdio and HTTP transports observed during testing.
