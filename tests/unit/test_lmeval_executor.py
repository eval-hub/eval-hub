"""Unit tests for LM Evaluation Harness executor."""

from datetime import datetime
from unittest.mock import patch
from uuid import uuid4

import pytest

from eval_hub.executors.base import ExecutionContext
from eval_hub.executors.lmeval import LMEvalExecutor
from eval_hub.models.evaluation import BackendSpec, BackendType, BenchmarkSpec


@pytest.fixture
def backend_config():
    """Create a basic backend config for testing."""
    return {
        "namespace": "test",
        "batch_size": "1",
        "log_samples": True,
        "deploy_crs": False,  # Disable actual CR deployment for unit tests
    }


@pytest.fixture
def lmeval_executor(backend_config):
    """Create an LMEvalExecutor instance for testing."""
    with (
        patch("eval_hub.executors.lmeval.config.load_incluster_config"),
        patch("eval_hub.executors.lmeval.config.load_kube_config"),
    ):
        return LMEvalExecutor(backend_config)


@pytest.fixture
def execution_context():
    """Create an ExecutionContext for testing."""
    evaluation_id = uuid4()
    benchmark_spec = BenchmarkSpec(
        name="arc_easy",
        tasks=["arc_easy"],
        config={},
    )
    backend_spec = BackendSpec(
        name="test-backend",
        type=BackendType.LMEVAL,
        config={},
        benchmarks=[benchmark_spec],
    )
    return ExecutionContext(
        evaluation_id=evaluation_id,
        model_url="http://test-server:8000",
        model_name="tinyllama",
        backend_spec=backend_spec,
        benchmark_spec=benchmark_spec,
        timeout_minutes=60,
        retry_attempts=3,
        started_at=datetime.utcnow(),
    )


@pytest.mark.unit
class TestLMEvalExecutorBaseUrl:
    """Test base_url handling in LMEvalExecutor."""

    def test_base_url_from_context_with_v1_completions_suffix(
        self, lmeval_executor, execution_context
    ):
        """Test that base_url from context is correctly added with /v1/completions suffix."""
        # Set base_url in context
        execution_context.model_url = "http://model-server:8000"

        # Build the CR
        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        # Verify base_url is in modelArgs
        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None, "base_url should be in modelArgs"
        assert base_url_arg["value"] == "http://model-server:8000/v1/completions"
        assert base_url_arg["value"].endswith("/v1/completions")

    def test_base_url_from_context_without_suffix(
        self, lmeval_executor, execution_context
    ):
        """Test that /v1/completions suffix is added if not present."""
        # Set base_url without /v1/completions
        execution_context.model_url = "http://model-server:8000"

        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None
        assert base_url_arg["value"] == "http://model-server:8000/v1/completions"

    def test_base_url_from_context_with_existing_suffix(
        self, lmeval_executor, execution_context
    ):
        """Test that base_url with existing /v1/completions suffix is not duplicated."""
        # Set base_url with /v1/completions already present
        execution_context.model_url = "http://model-server:8000/v1/completions"

        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None
        assert base_url_arg["value"] == "http://model-server:8000/v1/completions"
        # Ensure it's not duplicated
        assert base_url_arg["value"].count("/v1/completions") == 1

    def test_base_url_from_context_with_trailing_slash(
        self, lmeval_executor, execution_context
    ):
        """Test that trailing slashes are handled correctly."""
        # Set base_url with trailing slash
        execution_context.model_url = "http://model-server:8000/"

        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None
        # Trailing slash should be removed before adding /v1/completions
        assert base_url_arg["value"] == "http://model-server:8000/v1/completions"

    def test_base_url_from_backend_config_fallback(
        self, backend_config, execution_context
    ):
        """Test that base_url from backend config is used as fallback."""
        # Set base_url in backend config but not in context
        backend_config["base_url"] = "http://config-server:9000"
        execution_context.model_url = None

        with (
            patch("eval_hub.executors.lmeval.config.load_incluster_config"),
            patch("eval_hub.executors.lmeval.config.load_kube_config"),
        ):
            executor = LMEvalExecutor(backend_config)

        cr = executor._build_lmeval_job_cr(execution_context, ["arc_easy"], "tinyllama")

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None
        assert base_url_arg["value"] == "http://config-server:9000/v1/completions"

    def test_base_url_priority_context_over_config(
        self, backend_config, execution_context
    ):
        """Test that context base_url takes priority over backend config."""
        # Set both context and config base_url
        execution_context.model_url = "http://context-server:8000"
        backend_config["base_url"] = "http://config-server:9000"

        with (
            patch("eval_hub.executors.lmeval.config.load_incluster_config"),
            patch("eval_hub.executors.lmeval.config.load_kube_config"),
        ):
            executor = LMEvalExecutor(backend_config)

        cr = executor._build_lmeval_job_cr(execution_context, ["arc_easy"], "tinyllama")

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None
        # Context should take priority
        assert base_url_arg["value"] == "http://context-server:8000/v1/completions"
        assert "config-server" not in base_url_arg["value"]

    def test_base_url_empty_handling(self, lmeval_executor, execution_context):
        """Test that empty base_url is handled correctly."""
        # No base_url in context or config
        execution_context.model_url = None

        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        # base_url should still be present even if empty
        assert base_url_arg is not None
        assert base_url_arg["name"] == "base_url"
        assert base_url_arg["value"] == ""

    def test_base_url_in_model_args_list(self, lmeval_executor, execution_context):
        """Test that base_url is correctly included in the modelArgs list."""
        execution_context.model_url = "http://model-server:8000"

        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        model_args = cr["spec"]["modelArgs"]
        assert isinstance(model_args, list)

        # Verify base_url is in the list
        base_url_names = [arg["name"] for arg in model_args]
        assert "base_url" in base_url_names

        # Verify other expected args are present
        assert "model" in base_url_names
        assert "num_concurrent" in base_url_names
        assert "max_retries" in base_url_names
        assert "tokenized_requests" in base_url_names
        assert "tokenizer" in base_url_names

    def test_base_url_with_https(self, lmeval_executor, execution_context):
        """Test that HTTPS URLs are handled correctly."""
        execution_context.model_url = "https://secure-server:8443"

        cr = lmeval_executor._build_lmeval_job_cr(
            execution_context, ["arc_easy"], "tinyllama"
        )

        model_args = cr["spec"]["modelArgs"]
        base_url_arg = next(
            (arg for arg in model_args if arg["name"] == "base_url"), None
        )

        assert base_url_arg is not None
        assert base_url_arg["value"] == "https://secure-server:8443/v1/completions"
        assert base_url_arg["value"].startswith("https://")
