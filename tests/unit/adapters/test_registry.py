"""Unit tests for adapter registry."""

from typing import Any

import pytest

from eval_hub.adapters.base import SchemaAdapter
from eval_hub.adapters.registry import AdapterRegistry
from eval_hub.core.exceptions import BackendError
from eval_hub.executors.base import ExecutionContext
from eval_hub.models.evaluation import EvaluationResult


class DemoAdapter(SchemaAdapter):
    """Demo adapter for registry testing."""

    def __init__(self, custom_param: str = "default"):
        super().__init__(framework_name="test", version="1.0")
        self.custom_param = custom_param

    def get_kfp_component_spec(self) -> dict[str, Any]:
        return {"name": "test-component"}

    def transform_to_kfp_args(
        self, context: ExecutionContext, backend_config: dict[str, Any]
    ) -> dict[str, Any]:
        return {}

    def parse_kfp_output(
        self, artifacts: dict[str, str], context: ExecutionContext
    ) -> EvaluationResult:
        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="test",
            benchmark_id="test",
            benchmark_name="test",
            status="completed",
            metrics={},
        )

    def validate_config(self, config: dict[str, Any]) -> bool:
        return True


class AnotherDemoAdapter(SchemaAdapter):
    """Another demo adapter."""

    def __init__(self):
        super().__init__(framework_name="another", version="2.0")

    def get_kfp_component_spec(self) -> dict[str, Any]:
        return {"name": "another-component"}

    def transform_to_kfp_args(
        self, context: ExecutionContext, backend_config: dict[str, Any]
    ) -> dict[str, Any]:
        return {}

    def parse_kfp_output(
        self, artifacts: dict[str, str], context: ExecutionContext
    ) -> EvaluationResult:
        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="another",
            benchmark_id="test",
            benchmark_name="test",
            status="completed",
            metrics={},
        )

    def validate_config(self, config: dict[str, Any]) -> bool:
        return True


@pytest.fixture(autouse=True)
def clear_registry():
    """Clear registry before and after each test."""
    AdapterRegistry.clear()
    yield
    AdapterRegistry.clear()


def test_register_adapter():
    """Test registering an adapter."""
    AdapterRegistry.register("test", DemoAdapter)
    assert AdapterRegistry.is_registered("test")


def test_register_multiple_adapters():
    """Test registering multiple adapters."""
    AdapterRegistry.register("test", DemoAdapter)
    AdapterRegistry.register("another", AnotherDemoAdapter)

    assert AdapterRegistry.is_registered("test")
    assert AdapterRegistry.is_registered("another")


def test_register_invalid_adapter():
    """Test registering non-adapter class raises ValueError."""

    class NotAnAdapter:
        pass

    with pytest.raises(ValueError, match="must be a subclass of SchemaAdapter"):
        AdapterRegistry.register("invalid", NotAnAdapter)  # type: ignore


def test_get_adapter():
    """Test retrieving an adapter instance."""
    AdapterRegistry.register("test", DemoAdapter)
    adapter = AdapterRegistry.get_adapter("test")

    assert isinstance(adapter, DemoAdapter)
    assert adapter.framework_name == "test"
    assert adapter.version == "1.0"


def test_get_adapter_with_kwargs():
    """Test retrieving adapter with custom parameters."""
    AdapterRegistry.register("test", DemoAdapter)
    adapter = AdapterRegistry.get_adapter("test", custom_param="custom_value")

    assert isinstance(adapter, DemoAdapter)
    assert adapter.custom_param == "custom_value"


def test_get_unregistered_adapter():
    """Test getting unregistered adapter raises BackendError."""
    with pytest.raises(BackendError, match="No adapter registered"):
        AdapterRegistry.get_adapter("nonexistent")


def test_is_registered():
    """Test is_registered method."""
    assert not AdapterRegistry.is_registered("test")

    AdapterRegistry.register("test", DemoAdapter)
    assert AdapterRegistry.is_registered("test")


def test_list_frameworks():
    """Test listing registered frameworks."""
    assert AdapterRegistry.list_frameworks() == []

    AdapterRegistry.register("test", DemoAdapter)
    assert AdapterRegistry.list_frameworks() == ["test"]

    AdapterRegistry.register("another", AnotherDemoAdapter)
    frameworks = AdapterRegistry.list_frameworks()
    assert len(frameworks) == 2
    assert "test" in frameworks
    assert "another" in frameworks


def test_unregister():
    """Test unregistering an adapter."""
    AdapterRegistry.register("test", DemoAdapter)
    assert AdapterRegistry.is_registered("test")

    AdapterRegistry.unregister("test")
    assert not AdapterRegistry.is_registered("test")


def test_unregister_nonexistent():
    """Test unregistering non-existent adapter raises KeyError."""
    with pytest.raises(KeyError, match="Framework not registered"):
        AdapterRegistry.unregister("nonexistent")


def test_clear():
    """Test clearing all adapters."""
    AdapterRegistry.register("test", DemoAdapter)
    AdapterRegistry.register("another", AnotherDemoAdapter)
    assert len(AdapterRegistry.list_frameworks()) == 2

    AdapterRegistry.clear()
    assert AdapterRegistry.list_frameworks() == []


def test_registry_isolation():
    """Test that registry is shared across calls."""
    # Register in one context
    AdapterRegistry.register("test", DemoAdapter)

    # Should be available in another context
    assert AdapterRegistry.is_registered("test")
    adapter = AdapterRegistry.get_adapter("test")
    assert isinstance(adapter, DemoAdapter)
