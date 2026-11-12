"""OpenAI-compatible mock server for the kind development cluster.

scripts/dev-deploy.sh mounts this file into the evalhub-kind-mock-target and
evalhub-kind-mock-judge Deployments through the evalhub-kind-openai-mock-code
ConfigMap. It emulates the endpoint contract the evaluation adapters rely on:
chat completions, the Responses API subset Inspect needs, and the vLLM
/v1/completions echo contract (logprobs, token-id prompts,
return_token_ids/prompt_token_ids) used for loglikelihood scoring by
LightEval's logprob patch and LM Evaluation Harness's local-completions
backend. Scores are deterministic and not meaningful.

Requests that declare the structured-answer ``answer`` tool (inspect_scout's
generate_answer, used by Petri/Bloom judges and similar graders), or force a
specific tool through tool_choice (inspect_evals' schema_tool_graded_scorer,
for example simpleqa's ``submit``), are answered with a single tool call whose
arguments conform to the tool's own JSON schema. Text-graded prompts get the
reply shape their scorer parses (CyberSecEval 2's ``GRADE:`` letters, MASK's
``Final Answer:`` letters and JSON bounds, agentic-misalignment's <answer>
tags, both for its blackmail yes/no classifier and its three-category
deliberation classifier). A plain-text reply to any of these leaves the sample
unscored, the NaN metrics that follow break JSON serialization of the results
event, and the job then never reports its terminal status and hangs in
post_processing until its deadline.

Protocol regression tests: scripts/test_kind_openai_mock.py, run with
`make test-scripts` from the repository root.
"""

import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROLE = os.environ["MOCK_ROLE"]
MODEL = os.environ["MOCK_MODEL"]
API_KEYS = {item.strip() for item in os.environ["MOCK_API_KEYS"].split(",") if item.strip()}
# The Deployments never set these; tests use them to bind a free localhost port.
HOST = os.environ.get("MOCK_BIND", "0.0.0.0")
PORT = int(os.environ.get("MOCK_PORT", "8000"))


def declared_answer_tool(tools):
    """Return the request's structured-answer tool declaration, or None.

    OpenAI-compatible clients declare tools as {"type": "function",
    "function": {...}}; tolerate a flattened {"name": ..., "parameters": ...}
    form as well. Only the ``answer`` tool is auto-called: agent scaffolds
    declare their own tools (viewers, submit_understanding, ...) that the
    benchmark, not the mock, must drive.
    """
    for tool in tools or []:
        if not isinstance(tool, dict):
            continue
        function = tool.get("function") if isinstance(tool.get("function"), dict) else tool
        if function.get("name") == "answer":
            return function
    return None


def forced_tool(tool_choice, tools):
    """Return the tool declaration a forced tool_choice names, if any.

    tool_choice={"type": "function", "function": {"name": ...}} requires the
    model to call that exact function: inspect_evals' schema_tool_graded_scorer
    (simpleqa and similar graders) forces its ``submit`` tool and treats any
    other reply as an unscored sample.
    """
    if not isinstance(tool_choice, dict) or tool_choice.get("type") != "function":
        return None
    function = tool_choice.get("function")
    name = function.get("name") if isinstance(function, dict) else tool_choice.get("name")
    if not isinstance(name, str) or not name:
        return None
    for tool in tools or []:
        if not isinstance(tool, dict):
            continue
        candidate = tool.get("function") if isinstance(tool.get("function"), dict) else tool
        if candidate.get("name") == name:
            return candidate
    return None


def graded_text_response(request_text):
    """Match judge/grader prompt shapes that smoke frameworks parse strictly.

    Returns (content, response_format) or None for plain conversation prompts.
    An unparseable reply leaves the sample unscored; the resulting NaN metrics
    break JSON serialization of the results event, so the adapter never reports
    the terminal job status and the job hangs in post_processing until its
    fixture deadline. Order matters: each pattern is more specific than the
    next, and several prompts share the "final answer" and <answer> wording
    of the benchmarks below.
    """
    if re.search(r"deliberates_thinks_(?:eval|real)|no_deliberation", request_text, re.IGNORECASE):
        # agentic-misalignment's eval_awareness_scorer (the DeliberationClassifier)
        # asks for one of three categories inside <answer> tags and raises
        # ValueError on anything else, including the yes/no reply its blackmail
        # scorer wants. Answer with the category that claims no deliberation.
        return "<answer>no_deliberation</answer>", "deliberation-answer"
    if re.search(r"answer\s+with\s+grade\s*:|GRADE\s*:", request_text, re.IGNORECASE):
        # CyberSecEval 2's model_graded_qa templates (interpreter abuse and
        # prompt injection) require a "GRADE: <letter>" reply.
        return "GRADE: C", "grade"
    if "lower_bound" in request_text and "upper_bound" in request_text:
        # MASK's numerical judge parses JSON bounds. Null bounds are its
        # documented refusal shape, but a null (empty) estimate makes MASK's
        # accuracy normalisation divide by zero, and the resulting inf breaks
        # JSON serialization of the results event exactly like NaN does, so
        # report a finite, non-degenerate interval instead. This prompt also
        # says "focus on its final answer", so its JSON keys must be matched
        # before the multiple-choice phrasing below.
        return '{"lower_bound": 0.0, "upper_bound": 1.0}', "bounds-json"
    if re.search(r"final\s+answer.{0,200}<answer>", request_text, re.IGNORECASE):
        # MASK's binary judges parse "Final Answer: <A/B/C/D>".
        return "Final Answer: A", "final-answer"
    if "<answer>" in request_text:
        # agentic-misalignment's blackmail classifier parses <answer> tags.
        return "<answer>no</answer>", "answer-tag"
    match = re.search(r"with\s+(\d+)\s+values", request_text, re.IGNORECASE)
    if match:
        count = int(match.group(1))
        return "[" + ", ".join('"YES"' for _ in range(count)) + "]", "yes-array"
    return None


def schema_conforming_value(schema):
    """Synthesize a value that satisfies one JSON-schema property."""
    if not isinstance(schema, dict):
        return "mock"
    enum = schema.get("enum")
    if isinstance(enum, list) and enum:
        return enum[0]
    if "type" not in schema and isinstance(schema.get("anyOf"), list):
        # Optional and union properties often carry no top-level type, for
        # example {"anyOf": [{"type": "integer"}, {"type": "null"}]}. Pick the
        # first non-null branch and synthesize its value so the argument
        # conforms to that branch; recursion covers nested anyOf schemas.
        branch = next(
            (item for item in schema["anyOf"]
             if not (isinstance(item, dict) and item.get("type") == "null")),
            None,
        )
        if branch is not None:
            return schema_conforming_value(branch)
    kind = schema.get("type", "string")
    if isinstance(kind, list):
        kind = next((item for item in kind if item != "null"), "string")
    if kind in ("integer", "number"):
        # Petri dimension scores are integers bounded by minimum/maximum; the
        # minimum (1, "not at all") is also the rubric's correct score for a
        # transcript without target messages, which is what this mock serves.
        value = schema.get("minimum")
        if not isinstance(value, (int, float)) or isinstance(value, bool):
            value = 1
        maximum = schema.get("maximum")
        if isinstance(maximum, (int, float)) and not isinstance(maximum, bool) and value > maximum:
            value = maximum
        return int(value) if kind == "integer" else float(value)
    if kind == "boolean":
        return True
    if kind == "array":
        return []
    if kind == "object":
        return schema_conforming_arguments(schema)
    return "mock answer"


def schema_conforming_arguments(parameters):
    """Build tool-call arguments covering every declared property."""
    properties = (parameters or {}).get("properties")
    if not isinstance(properties, dict) or not properties:
        return {}
    return {name: schema_conforming_value(schema) for name, schema in properties.items()}


def tool_call_completion(request, function):
    """A chat completion that calls the declared tool exactly once."""
    return {
        "id": "chatcmpl-kind-mock",
        "object": "chat.completion",
        "created": 0,
        "model": request.get("model", MODEL),
        "choices": [{
            "index": 0,
            "message": {
                "role": "assistant",
                "content": None,
                "tool_calls": [{
                    "id": "call-kind-mock",
                    "type": "function",
                    "function": {
                        "name": function.get("name") or "answer",
                        "arguments": json.dumps(schema_conforming_arguments(function.get("parameters"))),
                    },
                }],
            },
            "finish_reason": "tool_calls",
        }],
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
    }


def responses_tool_call(request, function):
    """A Responses API reply that calls the declared tool exactly once.

    Inspect's OpenAI provider parses non-streaming Responses API output items
    with the openai SDK's ResponseFunctionToolCall, so the item carries both an
    item id and a call id, and the arguments are a JSON string.
    """
    return {
        "id": "resp-kind-mock",
        "object": "response",
        "created_at": 0,
        "status": "completed",
        "model": request.get("model", MODEL),
        "output": [{
            "type": "function_call",
            "id": "fc-kind-mock",
            "call_id": "call-kind-mock",
            "name": function.get("name") or "answer",
            "arguments": json.dumps(schema_conforming_arguments(function.get("parameters"))),
            "status": "completed",
        }],
        "parallel_tool_calls": False,
        "tool_choice": "auto",
        "tools": [],
        "error": None,
        "incomplete_details": None,
        "instructions": None,
        "metadata": {},
        "temperature": 1.0,
        "top_p": 1.0,
        "max_output_tokens": None,
        "previous_response_id": None,
        "reasoning": None,
        "text": {"format": {"type": "text"}},
        "truncation": "disabled",
        "usage": {
            "input_tokens": 1,
            "input_tokens_details": {"cached_tokens": 0, "cache_write_tokens": 0},
            "output_tokens": 1,
            "output_tokens_details": {"reasoning_tokens": 0},
            "total_tokens": 2,
        },
        "user": None,
    }


class Handler(BaseHTTPRequestHandler):
    """Answers one mock HTTP request per handler instance."""

    def log_message(self, _format, *_args):
        """Suppress the default stderr line; each accepted reply is logged as JSON."""
        pass

    def reply(self, status, value):
        """Send `value` as a JSON body with the given HTTP status."""
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        """Serve the model list at /v1/models and 404 for every other path."""
        if self.path.rstrip("/").endswith("/models"):
            self.reply(200, {"object": "list", "data": [{"id": MODEL, "object": "model"}]})
        else:
            self.reply(404, {"error": {"message": "not found"}})

    def do_POST(self):
        """Authenticate the caller, then answer completions, responses, and chat requests."""
        token = self.headers.get("Authorization", "")
        if token not in {"Bearer " + key for key in API_KEYS}:
            self.reply(401, {"error": {"message": "mock rejected credential"}})
            print(json.dumps({"role": ROLE, "auth": "rejected"}), flush=True)
            return

        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        request = json.loads(raw or b"{}")
        endpoint = self.path.split("?", 1)[0]
        if ROLE == "target" and endpoint.endswith("/v1/completions"):
            # Emulate the vLLM completions contract the loglikelihood
            # adapters rely on: echo=True returns one deterministic logprob
            # per token, max_tokens=0 scores exactly the submitted tokens,
            # token-id prompts are echoed unchanged, and return_token_ids
            # reports the ids a text prompt tokenized into (prompt_token_ids
            # per choice). Token ids are deterministic per character, so the
            # ids of a prompt are a prefix of the ids of any extension and
            # decoding an id reproduces its character. Test scores are not
            # meaningful.
            raw_prompt = request.get("prompt", "")
            if isinstance(raw_prompt, str):
                prompts = [raw_prompt]
            elif isinstance(raw_prompt, list) and raw_prompt and all(isinstance(item, int) for item in raw_prompt):
                # A bare list of ints is a single token-id prompt, not many.
                prompts = [raw_prompt]
            elif isinstance(raw_prompt, list):
                prompts = raw_prompt
            else:
                prompts = [str(raw_prompt)]
            echo = bool(request.get("echo"))
            max_tokens = request.get("max_tokens", 16)
            if not isinstance(max_tokens, int) or max_tokens < 0:
                max_tokens = 16
            choices = []
            prompt_tokens = 0
            for index, prompt_value in enumerate(prompts):
                if isinstance(prompt_value, list) and prompt_value and all(isinstance(item, int) for item in prompt_value):
                    token_ids = list(prompt_value)
                    token_texts = [chr(item) if 0 <= item <= 0x10FFFF else "mock" for item in token_ids]
                    text = ""
                else:
                    prompt = prompt_value if isinstance(prompt_value, str) else str(prompt_value)
                    token_ids = [ord(character) for character in prompt] or [1]
                    token_texts = list(prompt) or [chr(1)]
                    text = prompt
                prompt_tokens += len(token_ids)
                # vLLM echoes the prompt tokens first and then generates
                # max_tokens more; max_tokens=0 scores only the prompt.
                tokens = (token_texts + ["mock"] * max_tokens) if echo else ["mock"] * max(max_tokens, 1)
                text_offset = []
                offset = 0
                for token in tokens:
                    text_offset.append(offset)
                    offset += len(token)
                if not echo:
                    text = (prompt_value if isinstance(prompt_value, str) else str(prompt_value)) + " "
                choices.append({
                    "index": index,
                    "text": text,
                    "prompt_token_ids": token_ids,
                    "logprobs": {
                        "tokens": tokens,
                        "token_logprobs": [-0.1] * len(tokens),
                        "top_logprobs": [{"mock": -0.1}] * len(tokens),
                        "text_offset": text_offset,
                    },
                    "finish_reason": "stop",
                })
            self.reply(200, {
                "id": "cmpl-kind-mock",
                "object": "text_completion",
                "created": 0,
                "model": request.get("model", MODEL),
                "choices": choices,
                "usage": {"prompt_tokens": prompt_tokens, "completion_tokens": 1, "total_tokens": prompt_tokens + 1},
            })
            print(json.dumps({"role": ROLE, "endpoint": "completions", "auth": "accepted"}), flush=True)
            return

        # Inspect AI's OpenAI-compatible provider calls the Responses API by
        # default, so emulate the subset of it that a chat completion needs.
        # Without this, inspect fails with
        # TypeError("'NoneType' object is not iterable") on response.output.
        # Graders and scorers that infer the Responses API reach this branch,
        # so it honors the same tool and text-grading contracts as chat.
        if endpoint.endswith("/responses/input_tokens"):
            self.reply(200, {"object": "response.input_tokens", "input_tokens": 1})
            print(json.dumps({"role": ROLE, "endpoint": "responses.input_tokens", "auth": "accepted"}), flush=True)
            return

        if endpoint.endswith("/v1/responses"):
            forced = forced_tool(request.get("tool_choice"), request.get("tools"))
            # tool_choice "none" forbids tool calls, so a declared answer tool
            # must be answered with text like any other unselected tool.
            answer_tool = None if request.get("tool_choice") == "none" else declared_answer_tool(request.get("tools"))
            tool = forced if forced is not None else answer_tool
            if tool is not None:
                self.reply(200, responses_tool_call(request, tool))
                print(json.dumps({"role": ROLE, "endpoint": "responses", "auth": "accepted", "response_format": "forced-tool" if forced is not None else "answer-tool"}), flush=True)
                return
            graded = graded_text_response(json.dumps(request, ensure_ascii=False))
            text = graded[0] if graded is not None else "I will follow the requested constraints."
            self.reply(200, {
                "id": "resp-kind-mock",
                "object": "response",
                "created_at": 0,
                "status": "completed",
                "model": request.get("model", MODEL),
                "output": [{
                    "type": "message",
                    "id": "msg-kind-mock",
                    "status": "completed",
                    "role": "assistant",
                    "content": [{"type": "output_text", "text": text, "annotations": []}],
                }],
                "parallel_tool_calls": False,
                "tool_choice": "auto",
                "tools": [],
                "error": None,
                "incomplete_details": None,
                "instructions": None,
                "metadata": {},
                "temperature": 1.0,
                "top_p": 1.0,
                "max_output_tokens": None,
                "previous_response_id": None,
                "reasoning": None,
                "text": {"format": {"type": "text"}},
                "truncation": "disabled",
                "usage": {
                    "input_tokens": 1,
                    "input_tokens_details": {"cached_tokens": 0, "cache_write_tokens": 0},
                    "output_tokens": 1,
                    "output_tokens_details": {"reasoning_tokens": 0},
                    "total_tokens": 2,
                },
                "user": None,
            })
            print(json.dumps({"role": ROLE, "endpoint": "responses", "auth": "accepted", "response_format": graded[1] if graded is not None else "target"}), flush=True)
            return

        forced = forced_tool(request.get("tool_choice"), request.get("tools"))
        if forced is not None:
            # A forced tool_choice requires calling that exact function
            # (inspect_evals' schema_tool_graded_scorer forces ``submit``):
            # answer with a schema-conforming call so the sample scores.
            self.reply(200, tool_call_completion(request, forced))
            print(json.dumps({"role": ROLE, "auth": "accepted", "response_format": "forced-tool"}), flush=True)
            return

        # tool_choice "none" forbids tool calls, so reply with text instead of
        # auto-calling the declared answer tool.
        answer_tool = None if request.get("tool_choice") == "none" else declared_answer_tool(request.get("tools"))
        if answer_tool is not None:
            # Structured-answer contract (inspect_scout's generate_answer,
            # used by Petri/Bloom judges and similar graders): the framework
            # declares an ``answer`` tool whose JSON schema is the expected
            # response and waits for a valid call. Answer with a single tool
            # call so the sample scores instead of hanging the job.
            self.reply(200, tool_call_completion(request, answer_tool))
            print(json.dumps({"role": ROLE, "auth": "accepted", "response_format": "answer-tool"}), flush=True)
            return

        # Text-graded prompts reach either mock (graders may route at the
        # target's endpoint), so match the shapes their scorers parse; the
        # judge keeps its historical yes-array default for unmatched prompts.
        request_text = json.dumps(request, ensure_ascii=False)
        graded = graded_text_response(request_text)
        if graded is not None:
            content, response_format = graded
        elif ROLE == "judge":
            content = '["YES"]'
            response_format = "yes-array"
        else:
            content = "I will follow the requested constraints."
            response_format = "target"
        self.reply(200, {
            "id": "chatcmpl-kind-mock",
            "object": "chat.completion",
            "created": 0,
            "model": request.get("model", MODEL),
            "choices": [{"index": 0, "message": {"role": "assistant", "content": content}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
        })
        print(json.dumps({"role": ROLE, "auth": "accepted", "response_format": response_format}), flush=True)

# A thread per request: a kind benchmark run keeps KIND_BENCHMARK_PARALLEL
# evaluation jobs (plus their readiness probes) in flight against these mocks,
# and one slow client must not block the others the way a serial HTTPServer
# does. The handler only reads module constants and prints, so threads are safe.
ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
