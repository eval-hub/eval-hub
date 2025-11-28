"""Benchmark configuration transformer for schema adapters."""

from typing import Any

from ...models.evaluation import BenchmarkSpec


class BenchmarkConfigTransformer:
    """Transformer for extracting benchmark configuration.

    Provides reusable logic for extracting and normalizing benchmark
    configuration from benchmark specs and backend configs.
    """

    def extract(
        self,
        benchmark_spec: BenchmarkSpec,
        backend_config: dict[str, Any],
    ) -> dict[str, Any]:
        """Extract benchmark configuration from spec and backend config.

        Merges benchmark spec parameters with backend config, with backend
        config taking precedence for overlapping keys.

        Args:
            benchmark_spec: Benchmark specification from execution context
            backend_config: Backend-specific configuration

        Returns:
            Dictionary containing merged benchmark configuration
        """
        # Start with benchmark spec config
        config = benchmark_spec.config.copy() if benchmark_spec.config else {}

        # Add standard benchmark parameters
        if benchmark_spec.num_fewshot is not None:
            config.setdefault("num_fewshot", benchmark_spec.num_fewshot)

        if benchmark_spec.batch_size is not None:
            config.setdefault("batch_size", benchmark_spec.batch_size)

        if benchmark_spec.limit is not None:
            config.setdefault("limit", benchmark_spec.limit)

        if benchmark_spec.device is not None:
            config.setdefault("device", benchmark_spec.device)

        # Merge with backend config (backend config takes precedence)
        benchmark_overrides = backend_config.get("benchmark_config", {})
        config.update(benchmark_overrides)

        return config

    def get_tasks(
        self,
        benchmark_spec: BenchmarkSpec,
        backend_config: dict[str, Any],
    ) -> list[str]:
        """Extract task list from benchmark spec.

        Args:
            benchmark_spec: Benchmark specification
            backend_config: Backend-specific configuration

        Returns:
            List of tasks to evaluate
        """
        # Backend config can override tasks
        if "tasks" in backend_config:
            tasks = backend_config["tasks"]
        else:
            tasks = benchmark_spec.tasks

        # Ensure tasks is a list
        if isinstance(tasks, str):
            return [tasks]

        return list(tasks)

    def get_num_fewshot(
        self,
        benchmark_spec: BenchmarkSpec,
        backend_config: dict[str, Any],
        default: int = 0,
    ) -> int:
        """Extract num_fewshot parameter.

        Args:
            benchmark_spec: Benchmark specification
            backend_config: Backend-specific configuration
            default: Default value if not specified

        Returns:
            Number of few-shot examples
        """
        # Backend config takes precedence
        if "num_fewshot" in backend_config:
            return int(backend_config["num_fewshot"])

        # Then benchmark spec
        if benchmark_spec.num_fewshot is not None:
            return benchmark_spec.num_fewshot

        # Then benchmark spec config
        config = benchmark_spec.config or {}
        if "num_fewshot" in config:
            return int(config["num_fewshot"])

        return default

    def get_batch_size(
        self,
        benchmark_spec: BenchmarkSpec,
        backend_config: dict[str, Any],
        default: int | None = None,
    ) -> int | None:
        """Extract batch_size parameter.

        Args:
            benchmark_spec: Benchmark specification
            backend_config: Backend-specific configuration
            default: Default value if not specified

        Returns:
            Batch size or None if not specified
        """
        # Backend config takes precedence
        if "batch_size" in backend_config:
            value = backend_config["batch_size"]
            return int(value) if value is not None else None

        # Then benchmark spec
        if benchmark_spec.batch_size is not None:
            return benchmark_spec.batch_size

        # Then benchmark spec config
        config = benchmark_spec.config or {}
        if "batch_size" in config:
            value = config["batch_size"]
            return int(value) if value is not None else None

        return default

    def get_limit(
        self,
        benchmark_spec: BenchmarkSpec,
        backend_config: dict[str, Any],
        default: int | None = None,
    ) -> int | None:
        """Extract limit parameter.

        Args:
            benchmark_spec: Benchmark specification
            backend_config: Backend-specific configuration
            default: Default value if not specified

        Returns:
            Sample limit or None if not specified
        """
        # Backend config takes precedence
        if "limit" in backend_config:
            value = backend_config["limit"]
            return int(value) if value is not None else None

        # Then benchmark spec
        if benchmark_spec.limit is not None:
            return benchmark_spec.limit

        # Then benchmark spec config
        config = benchmark_spec.config or {}
        if "limit" in config:
            value = config["limit"]
            return int(value) if value is not None else None

        return default
