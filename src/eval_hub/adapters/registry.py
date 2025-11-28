"""Registry for schema adapters."""

from typing import Any

from ..core.exceptions import BackendError
from .base import SchemaAdapter


class AdapterRegistry:
    """Registry for managing schema adapters.

    Provides centralized registration and retrieval of schema adapters
    for different evaluation frameworks. Supports dynamic registration
    at runtime for extensibility.
    """

    _adapters: dict[str, type[SchemaAdapter]] = {}

    @classmethod
    def register(cls, framework_name: str, adapter_class: type[SchemaAdapter]) -> None:
        """Register a schema adapter for a framework.

        Args:
            framework_name: Name of the evaluation framework (e.g., "lighteval")
            adapter_class: SchemaAdapter subclass to register

        Raises:
            ValueError: If adapter_class is not a SchemaAdapter subclass
        """
        if not issubclass(adapter_class, SchemaAdapter):
            raise ValueError(
                f"Adapter class must be a subclass of SchemaAdapter, "
                f"got {adapter_class.__name__}"
            )

        cls._adapters[framework_name] = adapter_class

    @classmethod
    def get_adapter(cls, framework_name: str, **kwargs: Any) -> SchemaAdapter:
        """Get an adapter instance for a framework.

        Args:
            framework_name: Name of the evaluation framework
            **kwargs: Additional arguments to pass to adapter constructor

        Returns:
            Initialized SchemaAdapter instance

        Raises:
            BackendError: If no adapter is registered for the framework
        """
        if framework_name not in cls._adapters:
            raise BackendError(
                f"No adapter registered for framework: {framework_name}. "
                f"Available adapters: {list(cls._adapters.keys())}"
            )

        adapter_class = cls._adapters[framework_name]
        return adapter_class(**kwargs)

    @classmethod
    def is_registered(cls, framework_name: str) -> bool:
        """Check if an adapter is registered for a framework.

        Args:
            framework_name: Name of the evaluation framework

        Returns:
            True if adapter is registered, False otherwise
        """
        return framework_name in cls._adapters

    @classmethod
    def list_frameworks(cls) -> list[str]:
        """List all registered framework names.

        Returns:
            List of registered framework names
        """
        return list(cls._adapters.keys())

    @classmethod
    def unregister(cls, framework_name: str) -> None:
        """Unregister an adapter for a framework.

        Primarily used for testing and cleanup.

        Args:
            framework_name: Name of the evaluation framework

        Raises:
            KeyError: If framework is not registered
        """
        if framework_name not in cls._adapters:
            raise KeyError(f"Framework not registered: {framework_name}")

        del cls._adapters[framework_name]

    @classmethod
    def clear(cls) -> None:
        """Clear all registered adapters.

        Primarily used for testing and cleanup.
        """
        cls._adapters.clear()
