"""Unit tests for KFP executor."""

from datetime import UTC, datetime
from unittest.mock import AsyncMock, Mock, patch
from uuid import uuid4

import pytest

from eval_hub.adapters.base import SchemaAdapter
from eval_hub.core.exceptions import BackendError
from eval_hub.executors.base import ExecutionContext
from eval_hub.executors.kfp import KFPExecutor
from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationResult,
    EvaluationStatus,
)


class MockAdapter(SchemaAdapter):
    """Mock schema adapter for testing."""

    def __init__(self):
        super().__init__("test-framework", "1.0.0")

    def get_kfp_component_spec(self):
        return {
            "name": "test-component",
            "description": "Test component",
            "inputs": [{"name": "model_name", "type": "String"}],
            "outputs": [{"name": "metrics", "type": "Metrics"}],
            "implementation": {
                "container": {
                    "image": "test-image:latest",
                    "command": ["python", "run.py"],
                }
            },
        }

    def transform_to_kfp_args(self, context, backend_config):
        return {
            "model_name": context.model_name,
            "benchmark": context.benchmark_spec.name,
        }

    def parse_kfp_output(self, artifacts, context):
        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="kubeflow-pipelines",
            benchmark_id=context.benchmark_spec.name,
            benchmark_name=context.benchmark_spec.name,
            status=EvaluationStatus.COMPLETED,
            started_at=context.started_at,
            completed_at=datetime.now(UTC),
            metrics={"accuracy": 0.95},
        )

    def validate_config(self, config):
        return True


@pytest.fixture
def mock_adapter():
    """Create a mock adapter."""
    return MockAdapter()


@pytest.fixture
def backend_config():
    """Create a basic backend config for testing."""
    return {
        "kfp_endpoint": "http://kfp.example.com",
        "namespace": "kubeflow",
        "experiment_name": "test-experiment",
        "framework": "test-framework",
        "poll_interval_seconds": 1,
        "enable_caching": True,
    }


@pytest.fixture
def kfp_executor(backend_config, mock_adapter):
    """Create a KFPExecutor instance for testing."""
    with (
        patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class,
        patch("eval_hub.executors.kfp.asyncio.sleep"),
    ):
        mock_registry = Mock()
        mock_registry.get_adapter.return_value = mock_adapter
        mock_registry_class.return_value = mock_registry
        executor = KFPExecutor(backend_config)
        executor.adapter_registry = mock_registry
        return executor


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
        type=BackendType.KFP,
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
class TestKFPExecutorConfiguration:
    """Test KFP executor configuration and initialization."""

    def test_initialization_with_valid_config(self, backend_config, mock_adapter):
        """Test executor initializes correctly with valid configuration."""
        with patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class:
            mock_registry = Mock()
            mock_registry.get_adapter.return_value = mock_adapter
            mock_registry_class.return_value = mock_registry

            executor = KFPExecutor(backend_config)

            assert executor.kfp_endpoint == "http://kfp.example.com"
            assert executor.namespace == "kubeflow"
            assert executor.experiment_name == "test-experiment"
            assert executor.framework == "test-framework"
            assert executor.poll_interval_seconds == 1
            assert executor.enable_caching is True

    def test_initialization_without_kfp_endpoint(self, backend_config, mock_adapter):
        """Test executor raises error when kfp_endpoint is missing."""
        config = backend_config.copy()
        del config["kfp_endpoint"]

        with (
            patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class,
            pytest.raises(ValueError, match="kfp_endpoint is required"),
        ):
            mock_registry = Mock()
            mock_registry.get_adapter.return_value = mock_adapter
            mock_registry_class.return_value = mock_registry
            KFPExecutor(config)

    def test_initialization_without_framework(self, backend_config, mock_adapter):
        """Test executor raises error when framework is missing."""
        config = backend_config.copy()
        del config["framework"]

        with (
            patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class,
            pytest.raises(ValueError, match="framework is required"),
        ):
            mock_registry = Mock()
            mock_registry.get_adapter.return_value = mock_adapter
            mock_registry_class.return_value = mock_registry
            KFPExecutor(config)

    def test_initialization_with_unregistered_adapter(self, backend_config):
        """Test executor raises error when adapter is not registered."""
        with (
            patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class,
            pytest.raises(ValueError, match="No adapter registered"),
        ):
            mock_registry = Mock()
            mock_registry.get_adapter.side_effect = ValueError("Adapter not found")
            mock_registry_class.return_value = mock_registry
            KFPExecutor(backend_config)

    def test_default_configuration_values(self, mock_adapter):
        """Test default configuration values are applied correctly."""
        config = {
            "kfp_endpoint": "http://kfp.example.com",
            "framework": "test-framework",
        }

        with patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class:
            mock_registry = Mock()
            mock_registry.get_adapter.return_value = mock_adapter
            mock_registry_class.return_value = mock_registry

            executor = KFPExecutor(config)

            assert executor.namespace == "kubeflow"
            assert executor.experiment_name == "eval-hub"
            assert executor.poll_interval_seconds == 10
            assert executor.enable_caching is True

    def test_get_backend_type(self, kfp_executor):
        """Test get_backend_type returns correct identifier."""
        assert kfp_executor.get_backend_type() == "kubeflow-pipelines"

    def test_supports_parallel_execution(self, kfp_executor):
        """Test executor supports parallel execution."""
        assert kfp_executor.supports_parallel_execution() is True

    def test_get_recommended_timeout_minutes(self, kfp_executor):
        """Test recommended timeout is 120 minutes."""
        assert kfp_executor.get_recommended_timeout_minutes() == 120


@pytest.mark.unit
class TestKFPExecutorHealthCheck:
    """Test KFP executor health check functionality."""

    @pytest.mark.asyncio
    async def test_health_check_success(self, kfp_executor):
        """Test health check succeeds when KFP is accessible."""
        mock_client = Mock()
        mock_client.list_experiments = Mock(return_value=[])

        with patch.object(kfp_executor, "_get_kfp_client", return_value=mock_client):
            result = await kfp_executor.health_check()

            assert result is True
            mock_client.list_experiments.assert_called_once()

    @pytest.mark.asyncio
    async def test_health_check_failure(self, kfp_executor):
        """Test health check fails when KFP is not accessible."""
        mock_client = Mock()
        mock_client.list_experiments = Mock(side_effect=Exception("Connection refused"))

        with patch.object(kfp_executor, "_get_kfp_client", return_value=mock_client):
            result = await kfp_executor.health_check()

            assert result is False

    @pytest.mark.asyncio
    async def test_health_check_client_creation_failure(self, kfp_executor):
        """Test health check fails when client cannot be created."""
        with patch.object(
            kfp_executor,
            "_get_kfp_client",
            side_effect=BackendError("Failed to create client"),
        ):
            result = await kfp_executor.health_check()

            assert result is False


@pytest.mark.unit
class TestKFPExecutorClientCreation:
    """Test KFP client creation."""

    def test_get_kfp_client_creates_client(self, kfp_executor):
        """Test KFP client is created correctly."""
        mock_kfp_module = Mock()
        mock_client = Mock()
        mock_kfp_module.Client.return_value = mock_client

        with patch.dict("sys.modules", {"kfp": mock_kfp_module}):
            client = kfp_executor._get_kfp_client()

            assert client == mock_client
            mock_kfp_module.Client.assert_called_once_with(
                host="http://kfp.example.com", namespace="kubeflow"
            )

    def test_get_kfp_client_caches_instance(self, kfp_executor):
        """Test KFP client instance is cached."""
        mock_kfp_module = Mock()
        mock_client = Mock()
        mock_kfp_module.Client.return_value = mock_client

        with patch.dict("sys.modules", {"kfp": mock_kfp_module}):
            client1 = kfp_executor._get_kfp_client()
            client2 = kfp_executor._get_kfp_client()

            assert client1 == client2
            # Client should only be created once
            assert mock_kfp_module.Client.call_count == 1

    def test_get_kfp_client_missing_kfp_package(self, kfp_executor):
        """Test error raised when kfp package is not installed."""
        with (
            patch.dict("sys.modules", {"kfp": None}),
            patch(
                "builtins.__import__", side_effect=ImportError("No module named kfp")
            ),
            pytest.raises(BackendError, match="kfp package is required"),
        ):
            kfp_executor._get_kfp_client()


@pytest.mark.unit
class TestKFPExecutorBenchmarkExecution:
    """Test KFP executor benchmark execution."""

    @pytest.mark.asyncio
    async def test_execute_benchmark_success(
        self, kfp_executor, execution_context, mock_adapter
    ):
        """Test successful benchmark execution."""
        # Mock pipeline creation and execution
        mock_run_result = {
            "status": "Succeeded",
            "run_id": "test-run-123",
            "artifacts": {"output_metrics": "/path/to/metrics.json"},
            "duration_seconds": 120,
        }

        with (
            patch.object(
                kfp_executor,
                "_create_and_run_pipeline",
                new_callable=AsyncMock,
                return_value=mock_run_result,
            ),
            patch.object(
                kfp_executor.adapter_registry,
                "get_adapter",
                return_value=mock_adapter,
            ),
        ):
            result = await kfp_executor.execute_benchmark(execution_context)

            assert isinstance(result, EvaluationResult)
            assert result.status == EvaluationStatus.COMPLETED
            assert result.evaluation_id == execution_context.evaluation_id
            assert result.benchmark_name == "mmlu"
            assert result.benchmark_id == "mmlu"
            assert "accuracy" in result.metrics

    @pytest.mark.asyncio
    async def test_execute_benchmark_with_progress_callback(
        self, kfp_executor, execution_context, mock_adapter
    ):
        """Test benchmark execution with progress callbacks."""
        progress_updates = []

        def progress_callback(eval_id, progress, message):
            progress_updates.append((eval_id, progress, message))

        mock_run_result = {
            "status": "Succeeded",
            "run_id": "test-run-123",
            "artifacts": {},
            "duration_seconds": 120,
        }

        with (
            patch.object(
                kfp_executor,
                "_create_and_run_pipeline",
                new_callable=AsyncMock,
                return_value=mock_run_result,
            ),
            patch.object(
                kfp_executor.adapter_registry,
                "get_adapter",
                return_value=mock_adapter,
            ),
        ):
            await kfp_executor.execute_benchmark(execution_context, progress_callback)

            # Verify progress updates were made
            assert len(progress_updates) > 0
            # Check that progress values increase
            assert progress_updates[0][1] < progress_updates[-1][1]

    @pytest.mark.asyncio
    async def test_execute_benchmark_validation_failure(
        self, kfp_executor, execution_context, mock_adapter
    ):
        """Test benchmark execution fails when config validation fails."""
        mock_adapter.validate_config = Mock(return_value=False)

        with patch.object(
            kfp_executor.adapter_registry, "get_adapter", return_value=mock_adapter
        ):
            result = await kfp_executor.execute_benchmark(execution_context)

            assert result.status == EvaluationStatus.FAILED
            assert "validation failed" in result.error_message.lower()

    @pytest.mark.asyncio
    async def test_execute_benchmark_pipeline_failure(
        self, kfp_executor, execution_context, mock_adapter
    ):
        """Test benchmark execution handles pipeline failures."""
        with (
            patch.object(
                kfp_executor,
                "_create_and_run_pipeline",
                new_callable=AsyncMock,
                side_effect=BackendError("Pipeline failed"),
            ),
            patch.object(
                kfp_executor.adapter_registry,
                "get_adapter",
                return_value=mock_adapter,
            ),
        ):
            result = await kfp_executor.execute_benchmark(execution_context)

            assert result.status == EvaluationStatus.FAILED
            assert "Pipeline failed" in result.error_message


@pytest.mark.unit
class TestKFPExecutorPipelineManagement:
    """Test KFP pipeline creation and management."""

    @pytest.mark.asyncio
    async def test_create_and_run_pipeline(self, kfp_executor, execution_context):
        """Test pipeline creation and execution."""
        component_spec = {"name": "test-component"}
        kfp_args = {"model_name": "test-model"}

        mock_client = Mock()
        mock_experiment = Mock()
        mock_experiment.id = "exp-123"
        mock_run = Mock()
        mock_run.id = "run-123"

        mock_client.create_experiment = Mock(return_value=mock_experiment)
        mock_client.run_pipeline = Mock(return_value=mock_run)

        mock_monitor_result = {
            "status": "Succeeded",
            "run_id": "run-123",
            "artifacts": {},
            "duration_seconds": 60,
        }

        with (
            patch.object(kfp_executor, "_get_kfp_client", return_value=mock_client),
            patch.object(
                kfp_executor,
                "_monitor_pipeline_run",
                new_callable=AsyncMock,
                return_value=mock_monitor_result,
            ),
            patch("kfp.compiler.Compiler") as mock_compiler_class,
            patch("kfp.components.load_component_from_text"),
            patch("kfp.dsl.pipeline"),
        ):
            mock_compiler = Mock()
            mock_compiler_class.return_value = mock_compiler

            result = await kfp_executor._create_and_run_pipeline(
                execution_context, component_spec, kfp_args
            )

            assert result["status"] == "Succeeded"
            assert result["run_id"] == "run-123"
            mock_compiler.compile.assert_called_once()
            mock_client.run_pipeline.assert_called_once()

    @pytest.mark.asyncio
    async def test_monitor_pipeline_run_success(self, kfp_executor, execution_context):
        """Test monitoring a successful pipeline run."""
        mock_client = Mock()
        mock_run_detail = Mock()
        mock_run_detail.run.status = "Succeeded"

        mock_client.get_run = Mock(return_value=mock_run_detail)

        with patch.object(
            kfp_executor,
            "_extract_pipeline_artifacts",
            new_callable=AsyncMock,
            return_value={"metrics": "/path/to/metrics.json"},
        ):
            result = await kfp_executor._monitor_pipeline_run(
                mock_client, "run-123", execution_context
            )

            assert result["status"] == "Succeeded"
            assert result["run_id"] == "run-123"
            assert "metrics" in result["artifacts"]

    @pytest.mark.asyncio
    async def test_monitor_pipeline_run_failure(self, kfp_executor, execution_context):
        """Test monitoring a failed pipeline run."""
        mock_client = Mock()
        mock_run_detail = Mock()
        mock_run_detail.run.status = "Failed"
        mock_run_detail.run.error = "Test error"

        mock_client.get_run = Mock(return_value=mock_run_detail)

        with pytest.raises(BackendError, match="Pipeline execution failed"):
            await kfp_executor._monitor_pipeline_run(
                mock_client, "run-123", execution_context
            )

    @pytest.mark.asyncio
    async def test_monitor_pipeline_run_timeout(self, kfp_executor, execution_context):
        """Test pipeline monitoring times out correctly."""
        # Set very short timeout
        execution_context.timeout_minutes = 0.001  # Less than 1 second

        mock_client = Mock()
        mock_run_detail = Mock()
        mock_run_detail.run.status = "Running"

        mock_client.get_run = Mock(return_value=mock_run_detail)

        with (
            patch("time.time", side_effect=[0, 100]),  # Simulate time passing
            pytest.raises(TimeoutError, match="exceeded timeout"),
        ):
            await kfp_executor._monitor_pipeline_run(
                mock_client, "run-123", execution_context
            )

    @pytest.mark.asyncio
    async def test_extract_pipeline_artifacts(self, kfp_executor):
        """Test extracting artifacts from pipeline run."""
        mock_client = Mock()
        mock_run_detail = Mock()

        # Mock workflow manifest with artifacts
        manifest_yaml = """
status:
  outputs:
    artifacts:
      - name: output_metrics
        s3:
          key: /artifacts/metrics.json
      - name: output_results
        path: /results/output.json
"""

        mock_run_detail.pipeline_runtime.workflow_manifest = manifest_yaml

        mock_client.get_run = Mock(return_value=mock_run_detail)

        artifacts = await kfp_executor._extract_pipeline_artifacts(
            mock_client, "run-123"
        )

        assert "output_metrics" in artifacts
        assert "output_results" in artifacts
        assert artifacts["output_metrics"] == "/artifacts/metrics.json"
        assert artifacts["output_results"] == "/results/output.json"
