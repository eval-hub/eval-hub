# Generation agent loop

The runner-side agent is split into two Go packages: `internal/agent/loop`
contains the transport-independent loop and fixed tool registry, while
`internal/agent/llm` implements its model-client interface. The parent
`internal/agent` directory intentionally contains no Go source.

The loop receives a model name and a URL that the trusted runtime has already
rewritten to the local EvalHub sidecar. It accepts only `http` or `https` URLs
whose host is `localhost` or a loopback IP. It does not follow redirects, set an
Authorization header, or accept a destination from model output. The sidecar
resolves credentials from the job's Secret reference and adds the upstream
authorization. Do not put a real key in the request URL or model parameters.

## Required model API behavior

The configured model URL is the API base URL. The client appends
`/chat/completions`, preserving any path prefix and query string. For example,
`http://127.0.0.1:8080/llm/my-model/v1?api-version=2026-01` is called at
`/llm/my-model/v1/chat/completions?api-version=2026-01`. Local sidecar URLs such
as `http://localhost:8082/model/<job-id>/v1` retain the `/model/<job-id>` path.

The endpoint must accept a non-streaming `POST` with JSON containing `model`,
`messages`, `max_tokens`, and `stream: false`; when tools are registered, the
request also contains `tools` with `tool_choice: "auto"`. The client includes
`reasoning_effort` only when the trusted runner sets it. The sidecar and selected
provider must accept this optional field; the client does not negotiate provider
capabilities, so set it only for deployments known to support it. The endpoint
must return a JSON chat-completions response with
exactly one choice and an assistant message. The assistant message may contain
a string `content`, function `tool_calls` with string JSON arguments, or both.
Tool-call responses use `finish_reason: "tool_calls"`; final messages use a
non-tool finish reason such as `"stop"`. Tool calls must have unique IDs within
the conversation. Provider `usage` is optional; missing or partial token counts
are conservatively estimated from the serialized request and response.

Streaming responses, multiple choices, multimodal message content, provider
responses APIs, and provider-specific tool-call shapes are not supported by
this client. API keys and provider-specific headers must be supplied through an
auth mode implemented by the sidecar. The sidecar's current secret-backed model
auth contract injects a Bearer credential; providers that require a different
header or request signing need a separately specified sidecar change. An
OpenAI-compatible path alone does not guarantee compatible authentication,
versioning, or tool-call behavior across Azure, Ollama, and other providers.
Verify the selected deployment against this contract before enabling it.

## Budgets and progress

`loop.DefaultPresets` provides server-owned low, standard, and high defaults;
the service may configure individual limits under
`generation.effort_budget` (for example, `low.max_iterations`).
Omitted fields retain their library defaults. The preset set's `Validate` method
checks every effort budget against service hard caps, and `Presets.Resolve`
returns a value snapshot for one admitted job. An empty effort selects standard.
The request cannot override raw limits, and `strictness` is independent of the
loop budget.

The shipped defaults are:

| Effort | Iterations | Model calls | Tool calls | Total accounted tokens | Output tokens per response | Duration |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| low | 8 | 8 | 24 | 32,768 | 2,048 | 2 minutes |
| standard | 20 | 20 | 60 | 131,072 | 4,096 | 2 minutes |
| high | 40 | 40 | 120 | 524,288 | 8,192 | 10 minutes |

These are library defaults; the service can replace them with validated,
trusted deployment configuration, subject to the shipped hard ceilings.

The operator supplies these trusted settings through the EvalHub service config.
Budget fields are partial: omitted fields retain the table's default values.
The environment mappings are `EVALHUB_GENERATION_<EFFORT>_<LIMIT>`, where
`<EFFORT>` is `LOW`, `STANDARD`, or `HIGH`, and `<LIMIT>` is one of
`MAX_ITERATIONS`, `MAX_MODEL_CALLS`, `MAX_TOOL_CALLS`, `MAX_TOTAL_TOKENS`,
`MAX_OUTPUT_TOKENS`, or `MAX_DURATION`. Counts must be positive integers;
durations use Go duration strings such as `3m`. The effective three budgets
must remain monotonic and within the shipped hard ceilings;
invalid values fail service startup. The runner image is selected by
`EVALHUB_COLLECTION_GENERATION_RUNNER_IMAGE`; it must be an exact image reference
containing the worker binary. If it is unset, generation admission is disabled.

Terminal generation records are visible for 24 hours by default. Set
`generation.retention` or `EVALHUB_COLLECTION_GENERATION_RETENTION` to override
that window. See [resource maintenance](../../ARCHITECTURE.md#resource-maintenance-internaleval_hubmaintenance)
for the separate schedule that removes expired records from storage.

The loop tells the model its remaining call, turn, tool, and token budget. This
is guidance only: the loop enforces the immutable snapshot, caps each response,
and stops before dispatching a tool batch when no final model turn remains. A
complete final response returned within budget can succeed at the last allowed
call. If the budget or deadline expires before a final response, the loop
returns `ErrBudgetExceeded`; it does not return an incomplete proposal as a
success. Callers perform domain validation and trusted finalization on a
complete response.

Progress callbacks contain only runner-authored event kind, call number,
registered tool name, outcome, timestamp, and a bounded static summary. They do
not include prompts, model prose, tool arguments, tool results, URLs, or
credentials.
