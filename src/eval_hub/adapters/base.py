"""Base schema adapter for evaluation frameworks."""

from abc import ABC, abstractmethod
from typing import Any

from ..executors.base import ExecutionContext
from ..models.evaluation import EvaluationResult


class SchemaAdapter(ABC):
    """Base class for framework schema adapters (KFP-focused).

    Schema adapters provide bidirectional transformation between eval-hub's
    internal representation and framework-specific formats. For KFP-based
    execution, adapters generate KFP component specifications and transform
    execution contexts to component arguments.

    Attributes:
        framework_name: Name of the evaluation framework (e.g., "lighteval")
        version: Version of the adapter implementation
    """

    def __init__(self, framework_name: str, version: str):
        """Initialize the schema adapter.

        Args:
            framework_name: Name of the evaluation framework
            version: Version of the adapter implementation
        """
        self.framework_name = framework_name
        self.version = version

    @abstractmethod
    def get_kfp_component_spec(self) -> dict[str, Any]:
        """Get KFP component specification for this framework.

        Returns a KFP component specification (YAML-compatible dict) that
        defines the component's inputs, outputs, and implementation details.

        Returns:
            KFP component specification dictionary containing:
                - name: Component name
                - description: Component description
                - inputs: List of input parameters
                - outputs: List of output artifacts
                - implementation: Container/execution specification
        """
        pass

    @abstractmethod
    def transform_to_kfp_args(
        self, context: ExecutionContext, backend_config: dict[str, Any]
    ) -> dict[str, Any]:
        """Transform eval-hub context to KFP component arguments.

        Converts the eval-hub execution context and backend configuration
        into a dictionary of arguments suitable for the KFP component.

        Args:
            context: Eval-hub execution context containing model, benchmark,
                and evaluation parameters
            backend_config: Backend-specific configuration dictionary

        Returns:
            Dictionary of arguments for the KFP component, with keys matching
            the component's input parameter names
        """
        pass

    @abstractmethod
    def parse_kfp_output(
        self, artifacts: dict[str, str], context: ExecutionContext
    ) -> EvaluationResult:
        """Parse KFP component outputs to eval-hub result.

        Transforms the artifacts produced by the KFP component execution
        into an eval-hub EvaluationResult object.

        Args:
            artifacts: Dictionary mapping artifact names to file paths.
                Typically includes "output_metrics" and "output_results"
            context: Original execution context for reference

        Returns:
            EvaluationResult containing parsed metrics, status, and metadata
        """
        pass

    @abstractmethod
    def validate_config(self, config: dict[str, Any]) -> bool:
        """Validate backend configuration for this framework.

        Args:
            config: Backend configuration dictionary to validate

        Returns:
            True if configuration is valid, False otherwise

        Raises:
            ValueError: If configuration is invalid with details
        """
        pass

    def get_container_image(self) -> str:
        """Get default container image for this framework's KFP component.

        Returns:
            Container image URL. Override in subclass for custom images.
        """
        return f"ghcr.io/eval-hub/{self.framework_name}-kfp:latest"

    def get_framework_name(self) -> str:
        """Get the framework name.

        Returns:
            Framework name string
        """
        return self.framework_name

    def get_version(self) -> str:
        """Get the adapter version.

        Returns:
            Adapter version string
        """
        return self.version

    def supports_benchmark(self, benchmark_name: str) -> bool:
        """Check if this adapter supports a specific benchmark.

        Override in subclass to provide benchmark-specific validation.

        Args:
            benchmark_name: Name of the benchmark to check

        Returns:
            True if benchmark is supported, False otherwise
        """
        return True  # Default: support all benchmarks
