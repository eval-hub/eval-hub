"""Unit tests for NeMO Executor."""

from datetime import UTC, datetime
from unittest.mock import AsyncMock, MagicMock, patch
from uuid import uuid4

import pytest

from eval_hub.executors.nemo import NemoEvaluatorExecutor
from eval_hub.executors.base import ExecutionContext
from eval_hub.models.nemo import (
    NemoEvaluationConfig,
    NemoConfigParams,
    NemoContainerConfig,
    NemoEvaluationResult
)
from eval_hub.models.evaluation import (
    BenchmarkSpec,
    EvaluationResult,
    EvaluationStatus,
    BackendSpec,
    BackendType,
)

@pytest.fixture
def backend_config():
    """Create a basic backend config for testing."""
    return {
        "endpoint": "http://nemo.example.com",
        "namespace": "test",
        "batch_size": "1",
        "log_samples": True,
        "deploy_crs": False,  # Disable actual CR deployment for unit tests
    }

@pytest.fixture
def nemo_executor(backend_config):
    """Create a NemoEvaluatorExecutor instance for testing."""
    return NemoEvaluatorExecutor(backend_config)


@pytest.fixture
def execution_context():
    """Create an ExecutionContext for testing."""
    evaluation_id = uuid4()
    benchmark_spec = BenchmarkSpec(
        name="mmlu",
        tasks=["mmlu"],
        config={},
    )
    backend_spec = BackendSpec(
        name="test-backend",
        type=BackendType.NEMO_EVALUATOR,
        config={},
        benchmarks=[benchmark_spec],
    )
    return ExecutionContext(
        evaluation_id=evaluation_id,
        model_url="http://test-server:8000",
        model_name="test-model",
        backend_spec=backend_spec,
        benchmark_spec=benchmark_spec,
        timeout_minutes=60,
        retry_attempts=3,
        started_at=datetime.now(UTC),
    )


@pytest.mark.unit
class TestNemoExecutorConfiguration:
    """Test NemoExecutor configuration and initialization."""

    def test_initialization_with_valid_config(self, backend_config):
        """Test executor initializes correctly with valid configuration."""
        executor = NemoEvaluatorExecutor(backend_config)

        assert executor.container_config == NemoContainerConfig(
            endpoint= "http://nemo.example.com",
            port=3825,
            timeout_seconds=3600,
            max_retries=3,
            health_check_endpoint=None,
            auth_token=None,
            verify_ssl=True,
        )

    @pytest.mark.asyncio
    async def test_build_nemo_evaluation_request(self, nemo_executor, execution_context):
        """Test building a NeMo evaluation request."""
        nemo_request = await nemo_executor._build_nemo_evaluation_request(execution_context)

        assert nemo_request.command == "evaluate {{ config.type }}"
        assert nemo_request.framework_name == "eval-hub"
        assert nemo_request.pkg_name == "mmlu"
        assert nemo_request.config == NemoEvaluationConfig(
            output_dir=f"/tmp/nemo_eval_{execution_context.evaluation_id}_{execution_context.benchmark_spec.name}",
            params=NemoConfigParams(limit_samples=None,
            max_new_tokens=512,
            max_retries=3,
            parallelism=1,
            task='mmlu',
            temperature=0.0,
            request_timeout=60,
            top_p=0.95, extra={}),
            supported_endpoint_types=['chat'],
            type='mmlu'
        )

    def test_initialization_without_nemo_endpoint(self, backend_config):
        """Test executor raises error when the nemo endpoint is missing."""
        config = backend_config.copy()
        del config["endpoint"]
        with pytest.raises(ValueError, match="NeMo Evaluator configuration missing required field: endpoint"):
            NemoEvaluatorExecutor(config)

    def test_initialization_without_nemo_endpoint_type(self, backend_config):
        """Test executor raises error when the nemo endpoint type is not a string."""
        config = backend_config.copy()
        config["endpoint"] = ""
        with pytest.raises(ValueError, match="NeMo Evaluator endpoint must be a non-empty string"):
            NemoEvaluatorExecutor(config)

    def test_get_backend_type(self, backend_config):
        """Test get_backend_type returns correct identifier."""
        config = backend_config.copy()
        assert NemoEvaluatorExecutor(config).get_backend_type() == "nemo-evaluator"

    def test_get_recommended_timeout_minutes(self, backend_config):
        """Test recommended timeout is 60 minutes."""
        config = backend_config.copy()
        assert NemoEvaluatorExecutor(config).get_recommended_timeout_minutes() == 60


@pytest.mark.unit
class TestNemoExecutorHealthCheck:
    """Test NemoExecutor health check functionality."""

    @pytest.mark.asyncio
    async def test_health_check_success(self, nemo_executor):
        """Test health check succeeds when NemoExecutor is accessible."""
        mock_response = MagicMock()
        mock_response.status_code = 200

        mock_client = AsyncMock()
        mock_client.__aenter__.return_value.post = AsyncMock(return_value=mock_response)

        with patch("httpx.AsyncClient", return_value=mock_client):
            assert await nemo_executor.health_check() is True

    @pytest.mark.asyncio
    async def test_health_check_failure(self, nemo_executor):
        """Test health check fails when NemoExecutor is not accessible."""

        mock_client = AsyncMock()
        mock_client.__aenter__.return_value.post = AsyncMock(side_effect=Exception("Connection refused"))

        with patch("httpx.AsyncClient", return_value=mock_client):
            assert await nemo_executor.health_check() is False


@pytest.mark.unit
class TestNemoExecutorBenchmarkExecution:
    """Test NemoExecutor benchmark execution."""

    @pytest.mark.asyncio
    async def test_execute_benchmark_success(self, nemo_executor, execution_context):
        """Test successful benchmark execution."""
        # Create a mock NeMo result
        mock_nemo_result = NemoEvaluationResult(
            tasks={},
            groups={},
        )

        # Create expected eval result
        mock_eval_result = EvaluationResult(
            evaluation_id=execution_context.evaluation_id,
            provider_id="nemo-evaluator",
            benchmark_id="mmlu",
            benchmark_name="mmlu",
            status=EvaluationStatus.COMPLETED,
            metrics={"accuracy": 0.95},
            artifacts={"output_metrics": "/path/to/metrics.json"},
        )

        with patch.object(nemo_executor, "_track_start", new_callable=AsyncMock, return_value="test-run-123"), \
             patch.object(nemo_executor, "_execute_with_retries", new_callable=AsyncMock, return_value=mock_nemo_result), \
             patch.object(nemo_executor, "_convert_nemo_result_to_eval_hub", new_callable=AsyncMock, return_value=mock_eval_result), \
             patch.object(nemo_executor, "_track_complete", new_callable=AsyncMock):

            result = await nemo_executor.execute_benchmark(execution_context)

            assert isinstance(result, EvaluationResult)
            assert result.evaluation_id == execution_context.evaluation_id
            assert result.benchmark_name == "mmlu"
            assert result.status == EvaluationStatus.COMPLETED

    @pytest.mark.asyncio
    async def test_execute_benchmark_with_progress_callback(self, nemo_executor, execution_context):
        """Test benchmark execution with progress callback."""
        progress_updates = []

        def progress_callback(eval_id, progress, message):
            progress_updates.append((eval_id, progress, message))

        # Create a mock NeMo result
        mock_nemo_result = NemoEvaluationResult(
            tasks={},
            groups={},
        )

        # Create expected eval result
        mock_eval_result = EvaluationResult(
            evaluation_id=execution_context.evaluation_id,
            provider_id="nemo-evaluator",
            benchmark_id="mmlu",
            benchmark_name="mmlu",
            status=EvaluationStatus.COMPLETED,
            metrics={"accuracy": 0.95},
            artifacts={"output_metrics": "/path/to/metrics.json"},
        )

        with patch.object(nemo_executor, "_track_start", new_callable=AsyncMock, return_value="test-run-123"), \
             patch.object(nemo_executor, "_execute_with_retries", new_callable=AsyncMock, return_value=mock_nemo_result), \
             patch.object(nemo_executor, "_convert_nemo_result_to_eval_hub", new_callable=AsyncMock, return_value=mock_eval_result), \
             patch.object(nemo_executor, "_track_complete", new_callable=AsyncMock):

            await nemo_executor.execute_benchmark(execution_context, progress_callback)

            assert len(progress_updates) > 0
            assert progress_updates[0][1] < progress_updates[-1][1]
