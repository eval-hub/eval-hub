"""Protocol regression tests for the kind mock OpenAI-compatible server.

Run from the repository root with `make test-scripts`, or directly:

    python3 -m unittest discover -s scripts

Each test class starts scripts/kind-openai-mock.py on a free localhost port
and exercises the request shapes the evaluation adapters send: the vLLM
/v1/completions echo contract (return_token_ids/prompt_token_ids, token-id
prompts, max_tokens=0 echo scoring) that LightEval's loglikelihood patch
needs, LM Evaluation Harness local-completions parsing, generation text,
chat responses, and bearer-token authentication.
"""

import json
import math
import socket
import subprocess
import sys
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path

MOCK_PATH = Path(__file__).resolve().parent / "kind-openai-mock.py"


def free_port():
    """Return a TCP port that is currently free on loopback."""
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class MockServerTestCase(unittest.TestCase):
    """Starts the mock once per class and stops it after the last test."""

    role = "target"
    api_keys = "target-test-token,judge-test-token"
    api_key = "target-test-token"
    model = "kind-target-model"

    @classmethod
    def setUpClass(cls):
        """Start the mock on a free loopback port and wait for /v1/models to answer."""
        cls.port = free_port()
        cls.base = f"http://127.0.0.1:{cls.port}"
        cls.process = subprocess.Popen(
            [sys.executable, "-B", str(MOCK_PATH)],
            env={
                "MOCK_ROLE": cls.role,
                "MOCK_MODEL": cls.model,
                "MOCK_API_KEYS": cls.api_keys,
                "MOCK_BIND": "127.0.0.1",
                "MOCK_PORT": str(cls.port),
            },
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(f"{cls.base}/v1/models", timeout=1) as response:
                    if response.status == 200:
                        return
            except OSError:
                if cls.process.poll() is not None:
                    break
                time.sleep(0.1)
        cls.tearDownClass()
        raise RuntimeError("mock server did not become ready")

    @classmethod
    def tearDownClass(cls):
        """Terminate the mock process started by setUpClass."""
        process = getattr(cls, "process", None)
        if process is not None and process.poll() is None:
            process.terminate()
            process.wait(timeout=5)

    def post(self, path, payload, key=None):
        """POST a JSON payload to the mock and return the decoded JSON response."""
        request = urllib.request.Request(
            f"{self.base}{path}",
            data=json.dumps(payload).encode(),
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {key or self.api_key}",
            },
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=10) as response:
            return json.loads(response.read())

    def echo_choice(self, prompt, add_special_tokens=None):
        """Replicates lighteval_logprob_patch._echo_choice."""
        payload = {
            "model": self.model,
            "prompt": prompt,
            "max_tokens": 0,
            "echo": True,
            "logprobs": 1,
            "return_token_ids": True,
        }
        if add_special_tokens is not None:
            payload["add_special_tokens"] = add_special_tokens
        choice = self.post("/v1/completions", payload)["choices"][0]
        self.assertTrue(choice.get("logprobs"))
        return choice

    @staticmethod
    def returned_token_ids(choice):
        """Replicates lighteval_logprob_patch._returned_token_ids."""
        ids = choice.get("prompt_token_ids")
        assert isinstance(ids, list) and ids, "no prompt_token_ids"
        assert all(isinstance(i, int) for i in ids), "non-int prompt_token_ids"
        return ids


class TargetMockTests(MockServerTestCase):
    def test_lighteval_pairwise_loglikelihood(self):
        """The flow that failed before prompt_token_ids support: tokenize the
        context and each choice separately, then score the concatenated ids."""
        context = "Question: Which is the function of the g?\nAnswer:"
        choices = [" Paris", " London", " Berlin"]
        stripped = context.rstrip()
        whitespace = context[len(stripped):]
        context_ids = self.returned_token_ids(self.echo_choice(stripped))
        self.assertEqual(context_ids, [ord(c) for c in stripped])
        for choice_text in choices:
            ids = self.returned_token_ids(
                self.echo_choice(whitespace + choice_text, add_special_tokens=False)
            )
            prompt_ids = [*context_ids, *ids]
            scored = self.echo_choice(prompt_ids)
            self.assertEqual(scored["prompt_token_ids"], prompt_ids)
            token_logprobs = scored["logprobs"]["token_logprobs"]
            self.assertEqual(len(token_logprobs), len(prompt_ids))
            for i in range(len(context_ids), len(prompt_ids)):
                self.assertIsNotNone(token_logprobs[i])
                self.assertTrue(math.isfinite(token_logprobs[i]))

    def test_lighteval_joint_loglikelihood(self):
        """Replicates lighteval_logprob_patch._score_one_choice with
        _anchor_offsets, including a multibyte prompt."""
        context, choice_text = "Question: caff\u00e8?\nAnswer:", " Par\u00eds"
        full_prompt = context + choice_text
        boundary = len(context.rstrip())
        logprobs = self.post("/v1/completions", {
            "model": self.model,
            "prompt": full_prompt,
            "max_tokens": 0,
            "echo": True,
            "logprobs": 1,
        })["choices"][0]["logprobs"]
        tokens = logprobs.get("tokens") or []
        text_offsets = logprobs["text_offset"]
        if tokens and len(tokens) == len(text_offsets):
            echoed_len = text_offsets[-1] + len(tokens[-1])
            self.assertGreaterEqual(echoed_len, len(full_prompt))
            shift = echoed_len - len(full_prompt)
            if shift:
                covered = 0
                for token in tokens:
                    covered += len(token)
                    if covered >= shift:
                        break
                self.assertEqual(covered, shift)
                text_offsets = [max(0, offset - shift) for offset in text_offsets]
        positions = 0
        for i, offset in enumerate(text_offsets):
            end = text_offsets[i + 1] if i + 1 < len(text_offsets) else len(full_prompt)
            if end > boundary:
                value = logprobs["token_logprobs"][i]
                self.assertIsNotNone(value)
                self.assertTrue(math.isfinite(value))
                positions += 1
        self.assertGreater(positions, 0)

    def test_lm_eval_loglikelihood_parsing(self):
        """Replicates lm-eval harness local-completions parse_logprobs for a
        batched token-id prompt with echo=True and max_tokens=1."""
        context_encoding, continuation_encoding = [10, 20, 30, 40], [50, 60]
        prompt_ids = context_encoding + continuation_encoding
        choices = self.post("/v1/completions", {
            "model": self.model,
            "prompt": [prompt_ids, prompt_ids],
            "temperature": 0,
            "max_tokens": 1,
            "logprobs": 1,
            "seed": 1,
            "echo": True,
        })["choices"]
        self.assertEqual(len(choices), 2)
        for choice in sorted(choices, key=lambda c: c["index"]):
            logprobs = choice["logprobs"]
            self.assertEqual(len(logprobs["token_logprobs"]), len(prompt_ids) + 1)
            ctxlen = len(context_encoding)
            token_logprobs = logprobs["token_logprobs"][ctxlen:-1]
            top_logprobs = logprobs["top_logprobs"][ctxlen:-1]
            self.assertEqual(len(token_logprobs), len(continuation_encoding))
            self.assertEqual(len(top_logprobs), len(continuation_encoding))
            for value, top in zip(token_logprobs, top_logprobs):
                self.assertTrue(top)
                self.assertEqual(value, max(top.values()))

    def test_generation_keeps_prompt_echo_text(self):
        """Generation requests (no echo) keep the historical mock text."""
        choice = self.post("/v1/completions", {
            "model": self.model,
            "prompt": "Q: hi\nA:",
            "max_tokens": 8,
            "temperature": 0,
            "stop": ["\n"],
            "seed": 1,
        })["choices"][0]
        self.assertEqual(choice["text"], "Q: hi\nA: ")
        self.assertIn("prompt_token_ids", choice)

    def test_string_prompt_batch(self):
        """A batch of string prompts returns one indexed choice each, with token ids."""
        choices = self.post("/v1/completions", {
            "model": self.model,
            "prompt": ["one", "two"],
            "max_tokens": 0,
            "echo": True,
            "logprobs": 1,
            "return_token_ids": True,
        })["choices"]
        self.assertEqual([c["index"] for c in choices], [0, 1])
        self.assertEqual(choices[0]["prompt_token_ids"], [ord(c) for c in "one"])

    def test_chat_completions_fallthrough(self):
        """Target chat requests get the neutral mock reply."""
        message = self.post("/v1/chat/completions", {
            "model": self.model,
            "messages": [{"role": "user", "content": "hi"}],
        })["choices"][0]["message"]
        self.assertEqual(message["content"], "I will follow the requested constraints.")

    def test_bearer_token_authentication(self):
        """An unknown bearer token is rejected with HTTP 401."""
        with self.assertRaises(urllib.error.HTTPError) as raised:
            self.post("/v1/completions", {"prompt": "x"}, key="wrong-token")
        with raised.exception:  # close the error body to avoid a ResourceWarning
            pass
        self.assertEqual(raised.exception.code, 401)


class JudgeMockTests(MockServerTestCase):
    role = "judge"
    api_keys = "judge-test-token"
    api_key = "judge-test-token"
    model = "kind-judge-model"

    def test_grade_format(self):
        """CyberSecEval 2's model_graded_qa scorer requires this format."""
        content = self.post("/v1/chat/completions", {
            "model": self.model,
            "messages": [{"role": "user", "content": "answer with grade: is this good?"}],
        })["choices"][0]["message"]["content"]
        self.assertEqual(content, "GRADE: C")

    def test_yes_array_format(self):
        """A 'reply with N values' prompt yields an N-element YES array."""
        content = self.post("/v1/chat/completions", {
            "model": self.model,
            "messages": [{"role": "user", "content": "reply with 3 values"}],
        })["choices"][0]["message"]["content"]
        self.assertEqual(json.loads(content), ["YES", "YES", "YES"])


if __name__ == "__main__":
    unittest.main()
