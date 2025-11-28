"""Unit tests for TrackedExecutor base class."""

from datetime import datetime
from unittest.mock import AsyncMock, Mock
from uuid import uuid4

import pytest

from eval_hub.executors.base import ExecutionContext
from eval_hub.executors.tracked import TrackedExecutor
from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationResult,
    EvaluationStatus,
)


class ConcreteTrackedExecutor(TrackedExecutor):
    """Concrete implementation of TrackedExecutor for testing."""

    def _validate_config(self) -> None:
        """Validate configuration - no-op for testing."""
        pass

    @classmethod
    def get_backend_type(cls) -> str:
        """Get backend type for testing."""
        return "test-executor"

    async def health_check(self) -> bool:
        """Health check for testing."""
        return True

    async def execute_benchmark(self, context, progress_callback=None):
        """Execute benchmark for testing."""
        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="test",
            benchmark_id="test",
            benchmark_name="test",
            status=EvaluationStatus.COMPLETED,
        )


@pytest.fixture
def backend_config():
    """Create a basic backend config for testing."""
    return {
        "endpoint": "test-endpoint",
        "timeout": 30,
    }


@pytest.fixture
def tracked_executor(backend_config):
    """Create a TrackedExecutor instance for testing."""
    return ConcreteTrackedExecutor(backend_config)


@pytest.fixture
def mock_mlflow_client():
    """Create a mock MLFlow client."""
    client = Mock()
    client.create_experiment = AsyncMock(return_value="experiment-123")
    client.start_evaluation_run = AsyncMock(return_value="run-456")
    client.log_evaluation_result = AsyncMock()
    return client


@pytest.fixture
def execution_context(mock_mlflow_client):
    """Create a mock ExecutionContext."""
    benchmark_spec = BenchmarkSpec(
        name="test-benchmark",
        tasks=["test-task"],
        config={"category": "test", "metrics": ["accuracy"]},
    )

    backend_spec = BackendSpec(
        name="test-backend",
        type=BackendType.LMEVAL,
        config={},
        benchmarks=[benchmark_spec],
    )

    return ExecutionContext(
        evaluation_id=uuid4(),
        model_url="http://test-model:8000",
        model_name="test-model",
        backend_spec=backend_spec,
        benchmark_spec=benchmark_spec,
        timeout_minutes=30,
        retry_attempts=3,
        started_at=datetime.now(),
        mlflow_client=mock_mlflow_client,
        experiment_name="test-experiment",
    )


@pytest.fixture
def execution_context_no_mlflow():
    """Create an ExecutionContext without MLFlow client."""
    benchmark_spec = BenchmarkSpec(
        name="test-benchmark",
        tasks=["test-task"],
        config={"category": "test", "metrics": ["accuracy"]},
    )

    backend_spec = BackendSpec(
        name="test-backend",
        type=BackendType.LMEVAL,
        config={},
        benchmarks=[benchmark_spec],
    )

    return ExecutionContext(
        evaluation_id=uuid4(),
        model_url="http://test-model:8000",
        model_name="test-model",
        backend_spec=backend_spec,
        benchmark_spec=benchmark_spec,
        timeout_minutes=30,
        retry_attempts=3,
        started_at=datetime.now(),
        mlflow_client=None,
    )


@pytest.fixture
def sample_evaluation_result():
    """Create a sample EvaluationResult for testing."""
    return EvaluationResult(
        evaluation_id=uuid4(),
        provider_id="test-provider",
        benchmark_id="test-benchmark",
        benchmark_name="Test Benchmark",
        status=EvaluationStatus.COMPLETED,
        metrics={"accuracy": 0.95, "f1": 0.92},
        artifacts={"results": "/path/to/results.json"},
        started_at=datetime.now(),
        completed_at=datetime.now(),
        duration_seconds=120.5,
    )


class TestTrackedExecutor:
    """Test cases for TrackedExecutor."""

    def test_init(self, backend_config):
        """Test TrackedExecutor initialization."""
        executor = ConcreteTrackedExecutor(backend_config)
        assert executor.backend_config == backend_config
        assert executor.logger is not None

    @pytest.mark.asyncio
    async def test_track_start_success(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test successful tracking start."""
        run_id = await tracked_executor._track_start(execution_context, "test-provider")

        assert run_id == "run-456"
        mock_mlflow_client.create_experiment.assert_called_once()
        mock_mlflow_client.start_evaluation_run.assert_called_once()

    @pytest.mark.asyncio
    async def test_track_start_no_mlflow_client(
        self, tracked_executor, execution_context_no_mlflow
    ):
        """Test tracking start with no MLFlow client."""
        run_id = await tracked_executor._track_start(
            execution_context_no_mlflow, "test-provider"
        )

        assert run_id is None

    @pytest.mark.asyncio
    async def test_track_start_exception(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test tracking start with exception."""
        mock_mlflow_client.create_experiment.side_effect = Exception("MLFlow error")

        run_id = await tracked_executor._track_start(execution_context, "test-provider")

        assert run_id is None

    @pytest.mark.asyncio
    async def test_track_complete_success(
        self,
        tracked_executor,
        execution_context,
        sample_evaluation_result,
        mock_mlflow_client,
    ):
        """Test successful tracking completion."""
        result_data = sample_evaluation_result.model_dump()
        result_data["mlflow_run_id"] = "run-789"
        result_with_run_id = EvaluationResult(**result_data)

        await tracked_executor._track_complete(result_with_run_id, execution_context)

        mock_mlflow_client.log_evaluation_result.assert_called_once_with(
            result_with_run_id
        )

    @pytest.mark.asyncio
    async def test_track_complete_no_mlflow_client(
        self, tracked_executor, execution_context_no_mlflow, sample_evaluation_result
    ):
        """Test tracking completion with no MLFlow client."""
        # Should not raise an exception
        await tracked_executor._track_complete(
            sample_evaluation_result, execution_context_no_mlflow
        )

    @pytest.mark.asyncio
    async def test_track_complete_no_run_id(
        self,
        tracked_executor,
        execution_context,
        sample_evaluation_result,
        mock_mlflow_client,
    ):
        """Test tracking completion with no run ID."""
        # Should not raise an exception and should not call MLFlow
        await tracked_executor._track_complete(
            sample_evaluation_result, execution_context
        )

        mock_mlflow_client.log_evaluation_result.assert_not_called()

    @pytest.mark.asyncio
    async def test_track_complete_exception(
        self,
        tracked_executor,
        execution_context,
        sample_evaluation_result,
        mock_mlflow_client,
    ):
        """Test tracking completion with exception."""
        result_data = sample_evaluation_result.model_dump()
        result_data["mlflow_run_id"] = "run-789"
        result_with_run_id = EvaluationResult(**result_data)
        mock_mlflow_client.log_evaluation_result.side_effect = Exception("MLFlow error")

        # Should not raise an exception
        await tracked_executor._track_complete(result_with_run_id, execution_context)

    @pytest.mark.asyncio
    async def test_track_failure_with_existing_run_id(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test tracking failure with existing run ID."""
        existing_run_id = "existing-run-123"

        result_run_id = await tracked_executor._track_failure(
            execution_context, "test-provider", "Test error", existing_run_id
        )

        assert result_run_id == existing_run_id
        mock_mlflow_client.log_evaluation_result.assert_called_once()

        # Verify the logged result has the correct error details
        call_args = mock_mlflow_client.log_evaluation_result.call_args[0][0]
        assert call_args.status == EvaluationStatus.FAILED
        assert call_args.error_message == "Test error"
        assert call_args.mlflow_run_id == existing_run_id

    @pytest.mark.asyncio
    async def test_track_failure_create_new_run_id(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test tracking failure creating new run ID."""
        result_run_id = await tracked_executor._track_failure(
            execution_context, "test-provider", "Test error", None
        )

        assert result_run_id == "run-456"  # From mock
        mock_mlflow_client.create_experiment.assert_called_once()
        mock_mlflow_client.start_evaluation_run.assert_called_once()
        mock_mlflow_client.log_evaluation_result.assert_called_once()

    @pytest.mark.asyncio
    async def test_track_failure_no_mlflow_client(
        self, tracked_executor, execution_context_no_mlflow
    ):
        """Test tracking failure with no MLFlow client."""
        result_run_id = await tracked_executor._track_failure(
            execution_context_no_mlflow, "test-provider", "Test error", "run-123"
        )

        assert result_run_id == "run-123"  # Returns the provided run_id

    @pytest.mark.asyncio
    async def test_track_failure_exception(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test tracking failure with exception."""
        mock_mlflow_client.log_evaluation_result.side_effect = Exception("MLFlow error")

        result_run_id = await tracked_executor._track_failure(
            execution_context, "test-provider", "Test error", "run-123"
        )

        # Should still return the run_id even if logging fails
        assert result_run_id == "run-123"

    def test_with_tracking_with_run_id(
        self, tracked_executor, sample_evaluation_result
    ):
        """Test _with_tracking with valid run ID."""
        run_id = "test-run-456"

        result = tracked_executor._with_tracking(sample_evaluation_result, run_id)

        assert result.mlflow_run_id == run_id
        assert result.evaluation_id == sample_evaluation_result.evaluation_id
        assert result.provider_id == sample_evaluation_result.provider_id
        assert result.status == sample_evaluation_result.status
        assert result.metrics == sample_evaluation_result.metrics

    def test_with_tracking_with_none_run_id(
        self, tracked_executor, sample_evaluation_result
    ):
        """Test _with_tracking with None run ID."""
        result = tracked_executor._with_tracking(sample_evaluation_result, None)

        assert result.mlflow_run_id is None
        assert result.evaluation_id == sample_evaluation_result.evaluation_id
        assert result.provider_id == sample_evaluation_result.provider_id

    def test_with_tracking_preserves_all_fields(
        self, tracked_executor, sample_evaluation_result
    ):
        """Test that _with_tracking preserves all original fields."""
        run_id = "test-run-789"

        result = tracked_executor._with_tracking(sample_evaluation_result, run_id)

        # Check all fields are preserved
        assert result.evaluation_id == sample_evaluation_result.evaluation_id
        assert result.provider_id == sample_evaluation_result.provider_id
        assert result.benchmark_id == sample_evaluation_result.benchmark_id
        assert result.benchmark_name == sample_evaluation_result.benchmark_name
        assert result.status == sample_evaluation_result.status
        assert result.metrics == sample_evaluation_result.metrics
        assert result.artifacts == sample_evaluation_result.artifacts
        assert result.error_message == sample_evaluation_result.error_message
        assert result.started_at == sample_evaluation_result.started_at
        assert result.completed_at == sample_evaluation_result.completed_at
        assert result.duration_seconds == sample_evaluation_result.duration_seconds
        # Only mlflow_run_id should be different
        assert result.mlflow_run_id == run_id


class TestTrackedExecutorIntegration:
    """Integration tests for TrackedExecutor workflow."""

    @pytest.mark.asyncio
    async def test_full_tracking_workflow_success(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test full successful tracking workflow."""
        # Start tracking
        run_id = await tracked_executor._track_start(execution_context, "test-provider")
        assert run_id == "run-456"

        # Create result and attach tracking
        original_result = EvaluationResult(
            evaluation_id=execution_context.evaluation_id,
            provider_id="test-provider",
            benchmark_id="test-benchmark",
            benchmark_name="Test Benchmark",
            status=EvaluationStatus.COMPLETED,
            metrics={"accuracy": 0.95},
        )

        result_with_tracking = tracked_executor._with_tracking(original_result, run_id)
        assert result_with_tracking.mlflow_run_id == run_id

        # Complete tracking
        await tracked_executor._track_complete(result_with_tracking, execution_context)

        # Verify all calls were made
        mock_mlflow_client.create_experiment.assert_called_once()
        mock_mlflow_client.start_evaluation_run.assert_called_once()
        mock_mlflow_client.log_evaluation_result.assert_called_once()

    @pytest.mark.asyncio
    async def test_full_tracking_workflow_failure(
        self, tracked_executor, execution_context, mock_mlflow_client
    ):
        """Test full tracking workflow with failure."""
        # Start tracking
        run_id = await tracked_executor._track_start(execution_context, "test-provider")
        assert run_id == "run-456"

        # Simulate failure
        failed_run_id = await tracked_executor._track_failure(
            execution_context, "test-provider", "Simulated failure", run_id
        )
        assert failed_run_id == run_id

        # Verify experiment creation, run start, and failure logging
        mock_mlflow_client.create_experiment.assert_called_once()
        mock_mlflow_client.start_evaluation_run.assert_called_once()
        mock_mlflow_client.log_evaluation_result.assert_called_once()

        # Verify the logged result is a failure
        failure_result = mock_mlflow_client.log_evaluation_result.call_args[0][0]
        assert failure_result.status == EvaluationStatus.FAILED
        assert failure_result.error_message == "Simulated failure"
