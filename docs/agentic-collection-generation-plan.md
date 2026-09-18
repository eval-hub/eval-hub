# JIRA Issue: Agentic Collection Generation REST API

## JIRA Fields

- **Project:** RHAI
- **Type:** Epic
- **Summary:** `EvalHub: Agentic Collection Generation API`
- **Labels:** `api`, `agent`, `collections`, `llm`
- **Components:** `AI Evaluations`
- **Links:** Relates to RHAISTRAT-2574 (Trustworthy Adversarial Evaluation Infrastructure)

---

## Description (paste into JIRA)

### Summary

Add a `POST /api/v1/evaluations/collections/generation` endpoint with an in-service agent that takes a natural-language evaluation goal, selects appropriate benchmarks from the live catalog, calibrates weights and thresholds, and returns a proposed `CollectionConfig` — ready to persist via the existing collections API.

The agent operates as an agentic tool-use loop (not a single-shot prompt) — it can query providers, search benchmarks, inspect existing collections, and iteratively validate its draft against the CollectionConfig schema before returning a result.

### Motivation

Organizations evaluating LLMs often lack specialized evaluation expertise to choose which benchmarks to include, how to weight them, and where to set pass/fail thresholds. An API that translates a plain-language evaluation goal into a validated collection configuration lowers the barrier to entry and produces well-calibrated evaluation suites.

### Background

**PR #1015** (`feat/mcp-collection-generator` branch) adds a `design_collection` MCP prompt that assembles rich context (benchmark catalog, collection examples, calibration guidelines) and returns MCP prompt messages for the client's LLM to process. This REST API endpoint brings equivalent functionality to non-MCP HTTP clients (web UIs, CI pipelines, programmatic tools) and handles the LLM call server-side with an agentic loop.

### Relationship to RHAISTRAT-2574 (Trustworthy Adversarial Evaluation Infrastructure)

RHAISTRAT-2574 delivers **infrastructure-level isolation** (KataContainers, NetworkPolicy, OSSM, simulated internet) for safely running adversarial agents that might try to escape their sandbox — a K8s runtime concern addressing documented evaluation-to-real-world incidents.

The collection generation agent operates at a different layer: it is an **application-level in-process agent** with no adversarial risk, no external network access, and no escape concern. It runs in "Tier 0: Sealed" equivalent — purely operating on in-service data (providers, benchmarks, collections).

**Shared patterns:**
- **Tool-use framework**: The tool definition, dispatch, and execution patterns established by the collection generation agent (search benchmarks, validate collection, etc.) inform how RHAISTRAT-2574's agent evaluation tasks expose tools to agents under test in the sandbox
- **Action audit trails**: The collection generation response includes an actions log of tool calls and results; RHAISTRAT-2574's EvalCard records similar agent action metadata for evaluation integrity
- **Resource limits**: Max iterations, timeout, token budget — both agent contexts need configurable execution bounds

This epic establishes the in-service agent runtime that RHAISTRAT-2574's agent evaluation can later build upon.

----

### Technical Approach

#### Agent architecture (tool-use loop, not single-shot)

Instead of stuffing the entire benchmark catalog (188+ benchmarks across 12 providers) into a single LLM prompt, the agent operates as an iterative tool-use loop:

{code}
System prompt (calibration guidelines, output format) →
  LLM decides which tools to call →
    Execute tool, return result →
      LLM calls more tools or produces final answer →
        Validate → Return (or retry with errors)
{code}

**Agent tools:**

||Tool||Access||Purpose||
|`search_benchmarks`|Read-only|Find benchmarks by capability, domain, tag — agent queries selectively instead of consuming the entire catalog|
|`get_provider_details`|Read-only|Inspect a specific provider's benchmarks, metrics, score ranges, agent metadata|
|`list_collections`|Read-only|Browse existing curated collections as calibration references|
|`validate_collection`|Sandbox|Validate a draft collection against the CollectionConfig schema + benchmark catalog — returns structured errors|
|`estimate_thresholds`|Read-only|Get calibrated threshold ranges for specific benchmarks based on predefined collection data|
|`submit_draft`|Sandbox|"Submit" the final collection — triggers full validation and enrichment, returns the complete result|

The agent's "sandbox" is a read-only view of the provider/collection catalog with a validation workspace — no production side effects. Tools execute in-process against the Storage interface.

#### Shared package (reuses MCP prompt logic)

Extract prompt assembly from `internal/evalhub_mcp/server/prompts.go` into a shared `internal/collectiondesign/` package. Both the MCP prompt handler and the REST agent import it. A `CatalogSource` interface abstracts over Storage (API service) vs. evalhubclient (MCP server).

#### Zero-dependency LLM client with tool-use support

`internal/agent/llm/` — a `net/http` wrapper for the OpenAI `/v1/chat/completions` endpoint with tool definitions (`tools` array), tool call parsing (`tool_calls` in response), and tool result messages (`tool` role). Covers vLLM, OpenAI, Azure OpenAI, Ollama. No external SDK.

#### Reusable agent loop

`internal/agent/` — an agentic loop that sends messages to the LLM, parses tool calls, dispatches them via a tool registry, feeds results back, and detects completion. Configurable resource limits (max iterations, timeout, token budget). Action log captures every tool call and result for the response audit trail.

----

### API Surface

**Request:**
{code}
POST /api/v1/evaluations/collections/generation
{code}

{code:json}
{
  "evaluation_goal": "Enterprise deployment requiring safety, instruction following, and 64k context support",
  "provider_filter": "lm_evaluation_harness,lighteval",
  "max_benchmarks": 10,
  "strictness": "moderate"
}
{code}

||Field||Required||Description||
|`evaluation_goal`|Yes|Natural-language description of the evaluation use-case|
|`provider_filter`|No|Comma-separated provider IDs to restrict benchmark selection|
|`max_benchmarks`|No|Cap benchmark count (default: 12)|
|`strictness`|No|`lenient` / `moderate` / `strict` — shifts thresholds. Default: `moderate`|

**Response (200 OK):**
{code:json}
{
  "collection": {
    "name": "enterprise-safety-v1",
    "category": "safety",
    "description": "...",
    "tags": ["safety", "instruction_following", "long_context"],
    "pass_criteria": { "threshold": 0.72 },
    "benchmarks": [ ... ]
  },
  "yaml": "name: enterprise-safety-v1\ncategory: safety\n...",
  "rationale": "Selected safety-and-fairness benchmarks because...",
  "actions": [
    { "tool": "search_benchmarks", "input": {"query": "safety"}, "output_summary": "Found 7 benchmarks" },
    { "tool": "validate_collection", "input": { "..." }, "output_summary": "Valid" },
    { "tool": "submit_draft", "input": { "..." }, "output_summary": "Collection with 6 benchmarks" }
  ]
}
{code}

**Response (503 — no LLM configured):**
{code:json}
{
  "message_code": "collection_generation_not_configured",
  "message": "Collection generation requires agent configuration (agent.endpoint and agent.model).",
  "trace": "<request-id>"
}
{code}

----

### Configuration

{code:yaml}
agent:
  endpoint: ""          # OpenAI-compatible chat completions URL
  model: ""             # Model identifier
  api_key: ""           # Bearer token (supports secrets.mappings)
  max_tokens: 4096
  timeout: 120s         # longer for multi-turn agent loop
  max_iterations: 20    # cap on tool-call rounds
{code}

Env mappings: `AGENT_ENDPOINT`, `AGENT_MODEL`, `AGENT_MAX_TOKENS`, `AGENT_TIMEOUT`, `AGENT_MAX_ITERATIONS`. API key via `secrets.mappings.agent_api_key`.

----

### Package Structure

{code}
internal/agent/                      # Reusable agent runtime
  agent.go                           # Agentic loop: messages → LLM → parse tool_calls → execute → repeat
  tools.go                           # ToolRegistry: define, register, dispatch tools
  limits.go                          # ResourceLimits: max iterations, timeout, token budget
  audit.go                           # ActionLog: captures tool calls + results
  llm/
    client.go                        # OpenAI-compatible client with tool-use protocol
    types.go                         # ChatMessage, ToolDefinition, ToolCall, ToolResult

internal/collectiondesign/           # Collection generation (first agent consumer)
  catalog.go                         # CatalogSource interface + adapters
  prompt.go                          # System prompt for collection design
  tools.go                           # Collection-specific tools (implement agent.Tool)
  generator.go                       # Creates agent with tools, runs, returns result
  validate.go                        # ValidateGeneratedCollection()
  types.go                           # GenerationRequest, GenerationResult
{code}

----

### Files to Modify

||File||Change||
|`internal/agent/` (new)|Reusable agent runtime: loop, tool registry, action log, LLM client|
|`internal/collectiondesign/` (new)|Collection-specific tools, prompt, generator, validation|
|`internal/eval_hub/config/config.go`|Add `Agent *AgentConfig` field to `Config` struct|
|`internal/eval_hub/handlers/handlers.go`|Add optional `collectionGenerator` field + `WithCollectionGenerator` option|
|`internal/eval_hub/handlers/collection_generation.go` (new)|`HandleGenerateCollection` handler|
|`internal/eval_hub/server/server.go`|Wire `POST .../collections/generation` route; construct generator from config|
|`internal/evalhub_mcp/server/prompts.go`|Refactor to delegate to `collectiondesign` shared package|
|`config/config.yaml`|Add commented-out `agent` section + env_mappings|
|`docs/src/openapi.yaml`|Add path for the new endpoint|

----

### Safeguards

* *Benchmark ID validation* — every benchmark ID must exist in the loaded provider catalog
* *Metric validation* — `primary_score.metric` must match a metric the provider reports for that benchmark
* *Threshold bounds* — clamp to `[0.0, 1.0]` for accuracy metrics
* *Schema validation* — reuse `go-playground/validator` with same rules as collection creation
* *Validation retry* — on validation failure, feed errors back to the LLM as conversation context (up to 2 retries)
* *Resource limits* — max 20 tool-call iterations, configurable timeout (default 120s)
* *Dry-run only* — does not persist; user reviews and explicitly POSTs to create
* *Action audit trail* — every tool call and result recorded in the response

----

### Acceptance Criteria

* [ ] `POST /api/v1/evaluations/collections/generation` accepts `evaluation_goal` and optional parameters, returns a proposed `CollectionConfig` with rationale and action trail
* [ ] Returns `503 Service Unavailable` when `agent` config is not set
* [ ] Agent uses tool-use loop to selectively query benchmarks (not bulk context stuffing)
* [ ] Every benchmark ID validated against the provider catalog
* [ ] Every `primary_score.metric` validated against the provider's benchmark metrics
* [ ] Thresholds clamped to `[0.0, 1.0]` for accuracy metrics
* [ ] Full `CollectionConfig` struct validation (same rules as `POST /api/v1/evaluations/collections`)
* [ ] Resource limits enforced: max iterations and timeout
* [ ] Action audit trail included in response
* [ ] Dry-run only — does not persist the collection
* [ ] The returned `CollectionConfig` can be directly POSTed to `POST /api/v1/evaluations/collections`
* [ ] MCP `design_collection` prompt continues to work after shared-package refactor
* [ ] `internal/agent/` package is reusable (not coupled to collection generation)
* [ ] OpenAPI spec updated for the new endpoint
* [ ] Unit tests for: agent loop, tool dispatch, LLM client, prompt assembly, validation, handler
* [ ] FVT scenarios for: request validation errors, 503 when agent not configured

----

### Dependencies

* Depends on PR #1015 (`feat/mcp-collection-generator`) being merged first — refactors its prompt assembly logic into a shared package
* Relates to RHAISTRAT-2574 — shares tool-use patterns and action audit trail design; the agent runtime established here informs how agent evaluation tasks expose tools in the adversarial sandbox

---

## Implementation Notes (not part of JIRA issue)

### Implementation sequence

1. Create `internal/agent/` with agent loop, tool registry, action log, resource limits
2. Create `internal/agent/llm/` with OpenAI-compatible client + tool-use protocol
3. Create `internal/collectiondesign/` — extract shared logic from MCP `prompts.go`, add CatalogSource adapters
4. Implement collection-specific tools (`search_benchmarks`, `get_provider_details`, `list_collections`, `validate_collection`, `estimate_thresholds`, `submit_draft`)
5. Implement `Generator` (creates agent with tools, runs, returns result)
6. Add `AgentConfig` to config package
7. Add `HandleGenerateCollection` handler with functional options
8. Wire route in `server.go`
9. Refactor MCP `prompts.go` to delegate to `collectiondesign`
10. Add FVT scenarios, update OpenAPI spec

### Verification

1. `make test` — all unit tests pass
2. `make test-fvt` — new FVT scenarios pass
3. `go test ./internal/evalhub_mcp/...` — MCP tests pass after refactor
4. `make fmt lint` clean
5. Manual: start without agent config → 503
6. Manual: start with agent config → collection generated with action trail → POST to create succeeds
