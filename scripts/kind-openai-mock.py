"""OpenAI-compatible mock server for the kind development cluster.

scripts/dev-deploy.sh mounts this file into the evalhub-kind-mock-target and
evalhub-kind-mock-judge Deployments through the evalhub-kind-openai-mock-code
ConfigMap. It emulates the endpoint contract the evaluation adapters rely on:
chat completions, the Responses API subset Inspect needs, and the vLLM
/v1/completions echo contract (logprobs, token-id prompts,
return_token_ids/prompt_token_ids) used for loglikelihood scoring by
LightEval's logprob patch and LM Evaluation Harness's local-completions
backend. Scores are deterministic and not meaningful.

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
        if ROLE == "target" and endpoint.endswith("/responses/input_tokens"):
            self.reply(200, {"object": "response.input_tokens", "input_tokens": 1})
            print(json.dumps({"role": ROLE, "endpoint": "responses.input_tokens", "auth": "accepted"}), flush=True)
            return

        if ROLE == "target" and endpoint.endswith("/v1/responses"):
            text = "I will follow the requested constraints."
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
            print(json.dumps({"role": ROLE, "endpoint": "responses", "auth": "accepted"}), flush=True)
            return

        if ROLE == "judge":
            # Search the complete serialized request because OpenAI-compatible
            # clients may represent prompt content as strings or structured parts.
            request_text = json.dumps(request, ensure_ascii=False)
            grade_prompt = re.search(
                r"answer\s+with\s+grade\s*:", request_text, re.IGNORECASE
            ) is not None
            if grade_prompt:
                # CyberSecEval 2's model_graded_qa scorer requires this
                # grade format; an unparseable reply yields NaN and
                # prevents the SDK from reporting the terminal status.
                content = "GRADE: C"
                response_format = "grade"
            else:
                match = re.search(r"with\s+(\d+)\s+values", request_text, re.IGNORECASE)
                count = int(match.group(1)) if match else 1
                content = "[" + ", ".join('"YES"' for _ in range(count)) + "]"
                response_format = "yes-array"
        else:
            content = "I will follow the requested constraints."
        self.reply(200, {
            "id": "chatcmpl-kind-mock",
            "object": "chat.completion",
            "created": 0,
            "model": request.get("model", MODEL),
            "choices": [{"index": 0, "message": {"role": "assistant", "content": content}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
        })
        print(json.dumps({"role": ROLE, "auth": "accepted", "response_format": response_format if ROLE == "judge" else "target"}), flush=True)

# A thread per request: a kind benchmark run keeps KIND_BENCHMARK_PARALLEL
# evaluation jobs (plus their readiness probes) in flight against these mocks,
# and one slow client must not block the others the way a serial HTTPServer
# does. The handler only reads module constants and prints, so threads are safe.
ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
