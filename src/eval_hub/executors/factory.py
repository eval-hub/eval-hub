"""Factory for creating backend executors."""

from typing import Any

from ..core.exceptions import BackendError
from ..models.evaluation import BackendType
from .base import Executor
from .kfp import KFPExecutor
from .lmeval import LMEvalExecutor
from .nemo import NemoEvaluatorExecutor


class ExecutorFactory:
    """Factory for creating backend executors based on configuration."""

    _EXECUTOR_REGISTRY: dict[str, type[Executor]] = {
        BackendType.NEMO_EVALUATOR.value: NemoEvaluatorExecutor,
        BackendType.LMEVAL.value: LMEvalExecutor,
        BackendType.KFP.value: KFPExecutor,
    }

    @classmethod
    def register_executor(
        cls, backend_type: str, executor_class: type[Executor]
    ) -> None:
        """Register a new executor class for a backend type.

        Args:
            backend_type: Backend type identifier
            executor_class: Executor class to register
        """
        cls._EXECUTOR_REGISTRY[backend_type] = executor_class

    @classmethod
    def create_executor(
        cls, backend_type: str, backend_config: dict[str, Any]
    ) -> Executor:
        """Create an executor instance for the specified backend type.

        Args:
            backend_type: Type of backend to create executor for
            backend_config: Configuration for the backend

        Returns:
            Executor: Configured executor instance

        Raises:
            BackendError: If backend type is not supported or configuration is invalid
        """
        if backend_type not in cls._EXECUTOR_REGISTRY:
            raise BackendError(f"Unsupported backend type: {backend_type}")

        executor_class = cls._EXECUTOR_REGISTRY[backend_type]

        try:
            return executor_class(backend_config)
        except Exception as e:
            raise BackendError(f"Failed to create {backend_type} executor: {e}") from e

    @classmethod
    def validate_backend_config(
        cls, backend_type: str, backend_config: dict[str, Any]
    ) -> bool:
        """Validate configuration for a specific backend type.

        Args:
            backend_type: Backend type to validate for
            backend_config: Configuration to validate

        Returns:
            bool: True if configuration is valid

        Raises:
            BackendError: If backend type is not supported
        """
        if backend_type not in cls._EXECUTOR_REGISTRY:
            raise BackendError(f"Unsupported backend type: {backend_type}")

        executor_class = cls._EXECUTOR_REGISTRY[backend_type]
        return executor_class.validate_backend_config(backend_config)

    @classmethod
    def get_supported_backend_types(cls) -> list[str]:
        """Get list of supported backend types.

        Returns:
            list[str]: List of supported backend type identifiers
        """
        return list(cls._EXECUTOR_REGISTRY.keys())

    @classmethod
    def is_backend_supported(cls, backend_type: str) -> bool:
        """Check if a backend type is supported.

        Args:
            backend_type: Backend type to check

        Returns:
            bool: True if backend is supported
        """
        return backend_type in cls._EXECUTOR_REGISTRY


# Convenience function for creating executors
def create_executor(backend_type: str, backend_config: dict[str, Any]) -> Executor:
    """Convenience function to create an executor.

    Args:
        backend_type: Type of backend to create executor for
        backend_config: Configuration for the backend

    Returns:
        Executor: Configured executor instance
    """
    return ExecutorFactory.create_executor(backend_type, backend_config)
