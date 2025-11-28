"""Unit tests for schema adapter base class."""

from typing import Any
from uuid import uuid4

import pytest

from eval_hub.adapters.base import SchemaAdapter
from eval_hub.executors.base import ExecutionContext
from eval_hub.models.evaluation import (
    BackendSpec,
    BenchmarkSpec,
    EvaluationResult,
    EvaluationStatus,
)


class MockAdapter(SchemaAdapter):
    """Mock adapter for testing."""

    def __init__(self):
        super().__init__(framework_name="mock", version="1.0")

    def get_kfp_component_spec(self) -> dict[str, Any]:
        return {
            "name": "mock-evaluate",
            "description": "Mock evaluation component",
            "inputs": [],
            "outputs": [],
        }

    def transform_to_kfp_args(
        self, context: ExecutionContext, backend_config: dict[str, Any]
    ) -> dict[str, Any]:
        return {
            "model_url": context.model_url,
            "model_name": context.model_name,
        }

    def parse_kfp_output(
        self, artifacts: dict[str, str], context: ExecutionContext
    ) -> EvaluationResult:
        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="mock",
            benchmark_id="test",
            benchmark_name="test",
            status=EvaluationStatus.COMPLETED,
            metrics={"accuracy": 0.85},
        )

    def validate_config(self, config: dict[str, Any]) -> bool:
        return True


@pytest.fixture
def mock_adapter():
    """Fixture for mock adapter."""
    return MockAdapter()


@pytest.fixture
def execution_context():
    """Fixture for execution context."""
    benchmark_spec = BenchmarkSpec(name="test", tasks=["task1"])
    return ExecutionContext(
        evaluation_id=uuid4(),
        model_url="http://test-model:8000",
        model_name="test-model",
        backend_spec=BackendSpec(
            name="mock-backend",
            type="custom",
            config={},
            benchmarks=[benchmark_spec],
        ),
        benchmark_spec=benchmark_spec,
        timeout_minutes=60,
        retry_attempts=3,
    )


def test_adapter_initialization(mock_adapter):
    """Test adapter initialization."""
    assert mock_adapter.framework_name == "mock"
    assert mock_adapter.version == "1.0"


def test_get_framework_name(mock_adapter):
    """Test get_framework_name method."""
    assert mock_adapter.get_framework_name() == "mock"


def test_get_version(mock_adapter):
    """Test get_version method."""
    assert mock_adapter.get_version() == "1.0"


def test_get_container_image(mock_adapter):
    """Test get_container_image method."""
    assert mock_adapter.get_container_image() == "ghcr.io/eval-hub/mock-kfp:latest"


def test_supports_benchmark(mock_adapter):
    """Test supports_benchmark method."""
    # Default implementation supports all benchmarks
    assert mock_adapter.supports_benchmark("any-benchmark")
    assert mock_adapter.supports_benchmark("test")


def test_get_kfp_component_spec(mock_adapter):
    """Test get_kfp_component_spec method."""
    spec = mock_adapter.get_kfp_component_spec()
    assert spec["name"] == "mock-evaluate"
    assert spec["description"] == "Mock evaluation component"
    assert "inputs" in spec
    assert "outputs" in spec


def test_transform_to_kfp_args(mock_adapter, execution_context):
    """Test transform_to_kfp_args method."""
    args = mock_adapter.transform_to_kfp_args(execution_context, {})
    assert args["model_url"] == "http://test-model:8000"
    assert args["model_name"] == "test-model"


def test_parse_kfp_output(mock_adapter, execution_context):
    """Test parse_kfp_output method."""
    artifacts = {"output_metrics": "/path/to/metrics.json"}
    result = mock_adapter.parse_kfp_output(artifacts, execution_context)

    assert result.evaluation_id == execution_context.evaluation_id
    assert result.provider_id == "mock"
    assert result.status == EvaluationStatus.COMPLETED
    assert result.metrics["accuracy"] == 0.85


def test_validate_config(mock_adapter):
    """Test validate_config method."""
    assert mock_adapter.validate_config({})
    assert mock_adapter.validate_config({"key": "value"})


def test_abstract_methods_raise_not_implemented():
    """Test that abstract methods raise NotImplementedError."""

    class IncompleteAdapter(SchemaAdapter):
        """Incomplete adapter missing abstract methods."""

        def __init__(self):
            super().__init__(framework_name="incomplete", version="1.0")

    # Should not be able to instantiate without implementing abstract methods
    with pytest.raises(TypeError):
        IncompleteAdapter()  # type: ignore
