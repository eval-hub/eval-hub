"""Metric extraction and normalization for schema adapters."""

from enum import Enum
from typing import Any


class MetricNamingStrategy(str, Enum):
    """Strategies for naming metrics when extracting from frameworks.

    Different frameworks use different naming conventions. This enum
    defines strategies for normalizing metric names.
    """

    FLAT = "flat"  # metrics["accuracy"] = 0.85
    HIERARCHICAL = "hierarchical"  # metrics["mmlu.accuracy"] = 0.85
    NESTED = "nested"  # metrics["mmlu"]["accuracy"] = 0.85


class MetricExtractor:
    """Extractor for normalizing metrics from evaluation frameworks.

    Provides reusable logic for extracting and normalizing metrics from
    various framework output formats into eval-hub's standardized format.
    """

    def extract(
        self,
        raw_metrics: dict[str, Any],
        framework: str,
        naming_strategy: str = "hierarchical",
    ) -> dict[str, float]:
        """Extract and normalize metrics from framework output.

        Args:
            raw_metrics: Raw metrics from framework evaluation
            framework: Name of the framework (for framework-specific logic)
            naming_strategy: Strategy for metric naming ("flat", "hierarchical", "nested")

        Returns:
            Dictionary of normalized metrics with consistent naming
        """
        strategy = MetricNamingStrategy(naming_strategy)

        if strategy == MetricNamingStrategy.FLAT:
            return self._flatten_metrics(raw_metrics)
        elif strategy == MetricNamingStrategy.HIERARCHICAL:
            return self._flatten_metrics(raw_metrics, separator=".")
        elif strategy == MetricNamingStrategy.NESTED:
            # Keep nested structure but ensure all values are numeric
            return self._normalize_nested_metrics(raw_metrics)
        else:
            return self._flatten_metrics(raw_metrics)

    def _flatten_metrics(
        self,
        metrics: dict[str, Any],
        parent_key: str = "",
        separator: str = ".",
    ) -> dict[str, float]:
        """Flatten nested metrics dictionary.

        Args:
            metrics: Nested metrics dictionary
            parent_key: Parent key for recursion
            separator: Separator for hierarchical keys

        Returns:
            Flattened metrics dictionary
        """
        flattened: dict[str, float] = {}

        for key, value in metrics.items():
            new_key = f"{parent_key}{separator}{key}" if parent_key else key

            if isinstance(value, dict):
                # Recursively flatten nested dictionaries
                flattened.update(self._flatten_metrics(value, new_key, separator))
            elif isinstance(value, int | float):
                # Store numeric values
                flattened[new_key] = float(value)
            elif isinstance(value, bool):
                # Convert boolean to numeric
                flattened[new_key] = float(value)
            elif isinstance(value, str):
                # Try to parse string as number
                try:
                    flattened[new_key] = float(value)
                except ValueError:
                    # Skip non-numeric strings
                    pass
            # Skip other types (lists, None, etc.)

        return flattened

    def _normalize_nested_metrics(self, metrics: dict[str, Any]) -> dict[str, float]:
        """Normalize nested metrics but keep structure.

        For nested strategy, we still return a flat dict for now
        but could be extended to support nested dicts in the future.

        Args:
            metrics: Nested metrics dictionary

        Returns:
            Normalized flat metrics dictionary
        """
        # For now, just flatten with underscore separator
        # This can be extended to support truly nested structures
        return self._flatten_metrics(metrics, separator="_")

    def extract_from_file(
        self,
        file_path: str,
        framework: str,
        naming_strategy: str = "hierarchical",
    ) -> dict[str, float]:
        """Extract metrics from a JSON file.

        Args:
            file_path: Path to JSON file containing metrics
            framework: Name of the framework
            naming_strategy: Strategy for metric naming

        Returns:
            Dictionary of normalized metrics
        """
        import json

        with open(file_path) as f:
            raw_metrics = json.load(f)

        return self.extract(raw_metrics, framework, naming_strategy)

    def filter_metrics(
        self,
        metrics: dict[str, float],
        include_patterns: list[str] | None = None,
        exclude_patterns: list[str] | None = None,
    ) -> dict[str, float]:
        """Filter metrics based on patterns.

        Args:
            metrics: Metrics dictionary to filter
            include_patterns: Patterns to include (glob-style)
            exclude_patterns: Patterns to exclude (glob-style)

        Returns:
            Filtered metrics dictionary
        """
        import fnmatch

        filtered = metrics.copy()

        # Apply include patterns (if specified, only keep matching)
        if include_patterns:
            filtered = {
                key: value
                for key, value in filtered.items()
                if any(fnmatch.fnmatch(key, pattern) for pattern in include_patterns)
            }

        # Apply exclude patterns (remove matching)
        if exclude_patterns:
            filtered = {
                key: value
                for key, value in filtered.items()
                if not any(
                    fnmatch.fnmatch(key, pattern) for pattern in exclude_patterns
                )
            }

        return filtered

    def aggregate_metrics(
        self,
        metrics: dict[str, float],
        aggregation: str = "mean",
    ) -> float:
        """Aggregate metrics using specified strategy.

        Args:
            metrics: Metrics dictionary to aggregate
            aggregation: Aggregation strategy ("mean", "median", "min", "max")

        Returns:
            Aggregated value
        """
        if not metrics:
            return 0.0

        values = list(metrics.values())

        if aggregation == "mean":
            return sum(values) / len(values)
        elif aggregation == "median":
            sorted_values = sorted(values)
            n = len(sorted_values)
            if n % 2 == 0:
                return (sorted_values[n // 2 - 1] + sorted_values[n // 2]) / 2
            else:
                return sorted_values[n // 2]
        elif aggregation == "min":
            return min(values)
        elif aggregation == "max":
            return max(values)
        else:
            raise ValueError(f"Unknown aggregation strategy: {aggregation}")
