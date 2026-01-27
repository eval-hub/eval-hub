"""Garak security scanning framework schema adapter.

This adapter integrates Garak with eval-hub via Kubeflow Pipelines,
using KFP's native artifact system for result storage (no explicit S3 required).
"""

import json
from typing import Any

from ...core.logging import get_logger
from ...executors.base import ExecutionContext
from ...models.evaluation import EvaluationResult, EvaluationStatus
from ...utils.datetime_utils import safe_duration_seconds, utcnow
from ..base import SchemaAdapter
from ..transformers.model import ModelConfigTransformer


class GarakAdapter(SchemaAdapter):
    """Adapter for Garak security scanning framework (KFP-based).

    Garak is an LLM vulnerability scanner that tests language models for
    security vulnerabilities, prompt injection, jailbreaks, toxicity, and
    other safety issues. This adapter integrates Garak with eval-hub via
    Kubeflow Pipelines.

    The adapter supports:
    - Probe-based scans: specific vulnerability probes (e.g., "dan", "encoding")
    - Taxonomy-based scans: OWASP LLM Top 10, AVID taxonomy filters
    - Predefined benchmarks: quick, standard, comprehensive scans
    - Custom benchmark configurations via config parameters

    Key Features:
    - Uses KFP's native artifact system (no explicit S3 handling needed)
    - AVID taxonomy support for vulnerability classification
    - Configurable parallelism and generation counts
    - Comprehensive result parsing with attack success rates
    """

    def __init__(self) -> None:
        """Initialize Garak adapter."""
        super().__init__(framework_name="garak", version="1.0")
        self.model_transformer = ModelConfigTransformer()
        self.logger = get_logger(__name__)

    def get_kfp_component_spec(self) -> dict[str, Any]:
        """Generate KFP component specification for Garak.

        Returns:
            KFP component spec with inputs, outputs, and container implementation.
            Uses KFP's native artifact system for result storage.
        """
        return {
            "name": "garak-security-scan",
            "description": "LLM security vulnerability scan using Garak framework",
            "inputs": [
                {
                    "name": "model_url",
                    "type": "String",
                    "description": "Model endpoint URL (OpenAI-compatible API)",
                },
                {
                    "name": "model_name",
                    "type": "String",
                    "description": "Model identifier/name",
                },
                {
                    "name": "api_key",
                    "type": "String",
                    "optional": True,
                    "description": "API key for model endpoint (optional)",
                },
                {
                    "name": "benchmark_id",
                    "type": "String",
                    "description": "Benchmark identifier (e.g., 'quick', 'owasp_llm_top10')",
                },
                {
                    "name": "probes",
                    "type": "JsonArray",
                    "optional": True,
                    "description": "List of Garak probes to run (overrides benchmark)",
                },
                {
                    "name": "taxonomy_filters",
                    "type": "JsonArray",
                    "optional": True,
                    "description": "Taxonomy filters for probe selection (e.g., 'owasp:llm')",
                },
                {
                    "name": "eval_threshold",
                    "type": "Float",
                    "default": 0.5,
                    "description": "Vulnerability threshold (0-1)",
                },
                {
                    "name": "timeout_seconds",
                    "type": "Integer",
                    "default": 3600,
                    "description": "Maximum execution time in seconds",
                },
                {
                    "name": "parallel_attempts",
                    "type": "Integer",
                    "default": 8,
                    "description": "Number of parallel probe attempts (1-32)",
                },
                {
                    "name": "generations",
                    "type": "Integer",
                    "default": 1,
                    "description": "Number of generations per probe (1-10)",
                },
                {
                    "name": "s3_bucket",
                    "type": "String",
                    "description": "S3 bucket for storing results",
                },
                {
                    "name": "s3_prefix",
                    "type": "String",
                    "default": "",
                    "optional": True,
                    "description": "S3 prefix for storing results",
                },
                {
                    "name": "job_id",
                    "type": "String",
                    "description": "Unique job identifier for S3 storage",
                },
            ],
            "outputs": [
                {
                    "name": "output_metrics",
                    "type": "Metrics",
                    "description": "Aggregated security metrics",
                },
                {
                    "name": "output_results",
                    "type": "Dataset",
                    "description": "Detailed scan results with vulnerability info",
                },
            ],
            "implementation": {
                "container": {
                    "image": self.get_container_image(),
                    "command": ["python", "/app/garak_component.py"],
                    "args": [
                        "--model_url",
                        {"inputValue": "model_url"},
                        "--model_name",
                        {"inputValue": "model_name"},
                        "--api_key",
                        {"inputValue": "api_key"},
                        "--benchmark_id",
                        {"inputValue": "benchmark_id"},
                        "--probes",
                        {"inputValue": "probes"},
                        "--taxonomy_filters",
                        {"inputValue": "taxonomy_filters"},
                        "--eval_threshold",
                        {"inputValue": "eval_threshold"},
                        "--timeout_seconds",
                        {"inputValue": "timeout_seconds"},
                        "--parallel_attempts",
                        {"inputValue": "parallel_attempts"},
                        "--generations",
                        {"inputValue": "generations"},
                        "--s3_bucket",
                        {"inputValue": "s3_bucket"},
                        "--s3_prefix",
                        {"inputValue": "s3_prefix"},
                        "--job_id",
                        {"inputValue": "job_id"},
                        "--output_metrics",
                        {"outputPath": "output_metrics"},
                        "--output_results",
                        {"outputPath": "output_results"},
                    ],
                }
            },
        }

    def transform_to_kfp_args(
        self, context: ExecutionContext, backend_config: dict[str, Any]
    ) -> dict[str, Any]:
        """Transform eval-hub context to KFP component arguments.

        Args:
            context: Eval-hub execution context
            backend_config: Backend configuration

        Returns:
            Dictionary of arguments for Garak KFP component
        """
        # Extract model configuration
        model_config = self.model_transformer.extract(context, backend_config)

        # Get benchmark configuration
        benchmark_id = context.benchmark_spec.name
        benchmark_config = self._get_benchmark_config(benchmark_id, context)

        # Extract probe/taxonomy configuration
        probes = None
        taxonomy_filters = None

        if benchmark_config.get("type") == "taxonomy":
            taxonomy_filters = benchmark_config.get("taxonomy_filters", [])
        else:
            probes = benchmark_config.get("probes", [])

        # Override from benchmark spec config if provided
        spec_config = context.benchmark_spec.config or {}
        if "probes" in spec_config:
            probes = spec_config["probes"]
            taxonomy_filters = None
        elif "taxonomy_filters" in spec_config:
            taxonomy_filters = spec_config["taxonomy_filters"]
            probes = None

        # Get execution parameters
        eval_threshold = spec_config.get(
            "eval_threshold", benchmark_config.get("eval_threshold", 0.5)
        )
        timeout_seconds = spec_config.get(
            "timeout", benchmark_config.get("timeout", 3600)
        )
        parallel_attempts = spec_config.get("parallel_attempts", 8)
        generations = spec_config.get("generations", 1)

        # API key from config or context metadata
        api_key = spec_config.get("api_key") or context.metadata.get("api_key")

        # S3 configuration for result storage
        s3_bucket = backend_config.get("s3_bucket", "")
        s3_prefix = backend_config.get("s3_prefix", "")
        job_id = str(context.evaluation_id)

        return {
            "model_url": model_config.url,
            "model_name": model_config.name,
            "api_key": api_key or "",
            "benchmark_id": benchmark_id,
            "probes": probes or [],
            "taxonomy_filters": taxonomy_filters or [],
            "eval_threshold": eval_threshold,
            "timeout_seconds": timeout_seconds,
            "parallel_attempts": parallel_attempts,
            "generations": generations,
            "s3_bucket": s3_bucket,
            "s3_prefix": s3_prefix,
            "job_id": job_id,
        }

    def parse_kfp_output(
        self, artifacts: dict[str, str], context: ExecutionContext
    ) -> EvaluationResult:
        """Parse KFP component outputs to eval-hub result.

        Args:
            artifacts: Dictionary mapping artifact names to file paths
            context: Original execution context

        Returns:
            EvaluationResult with parsed security metrics and metadata
        """
        metrics: dict[str, float | int | str] = {}
        artifact_paths: dict[str, str] = {}

        # Parse metrics artifact (primary source)
        if "output_metrics" in artifacts:
            metrics_path = artifacts["output_metrics"]
            artifact_paths["metrics"] = metrics_path

            try:
                with open(metrics_path) as f:
                    raw_metrics = json.load(f)
                    metrics = self._normalize_metrics(raw_metrics)
            except (FileNotFoundError, json.JSONDecodeError) as e:
                print(f"Warning: Could not load metrics from {metrics_path}: {e}")

        # Parse detailed results for additional metrics
        if "output_results" in artifacts:
            results_path = artifacts["output_results"]
            artifact_paths["results"] = results_path

            try:
                with open(results_path) as f:
                    scan_result = json.load(f)
                    # Merge scan result metrics with existing metrics
                    scan_metrics = self._extract_metrics_from_scan_result(scan_result)
                    # Only add metrics not already present
                    for key, value in scan_metrics.items():
                        if key not in metrics:
                            metrics[key] = value
            except (FileNotFoundError, json.JSONDecodeError) as e:
                print(f"Warning: Could not load results from {results_path}: {e}")

        completed_at = utcnow()
        duration = (
            safe_duration_seconds(completed_at, context.started_at)
            if context.started_at
            else 0.0
        )

        # Determine status based on metrics
        status = EvaluationStatus.COMPLETED
        error_message = None

        if not metrics:
            status = EvaluationStatus.FAILED
            error_message = "No metrics extracted from scan results"

        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="garak",
            benchmark_id=context.benchmark_spec.name,
            benchmark_name=context.benchmark_spec.name,
            status=status,
            metrics=metrics,
            artifacts=artifact_paths,
            error_message=error_message,
            started_at=context.started_at,
            completed_at=completed_at,
            duration_seconds=duration,
            mlflow_run_id=None,
        )

    def validate_config(self, config: dict[str, Any]) -> bool:
        """Validate configuration for Garak.

        Args:
            config: Backend configuration dictionary

        Returns:
            True if configuration is valid

        Raises:
            ValueError: If configuration is invalid
        """
        # Validate framework is correctly specified
        if "framework" in config and config["framework"] != "garak":
            raise ValueError(
                f"Invalid framework '{config['framework']}' for GarakAdapter. "
                "Expected 'garak'"
            )

        return True

    def get_container_image(self) -> str:
        """Get default container image for Garak KFP component.

        Returns:
            Container image URL for Garak scanner
        """
        import os

        return os.getenv("GARAK_KFP_IMAGE", "quay.io/evalhub/garak-kfp:latest")

    def supports_benchmark(self, benchmark_name: str) -> bool:
        """Check if this adapter supports a specific benchmark.

        Args:
            benchmark_name: Name of the benchmark to check

        Returns:
            True - Garak adapter supports any benchmark defined in providers.yaml
        """
        # Garak adapter is flexible and supports any benchmark configuration
        # The actual validation happens in providers.yaml
        return True

    def _get_benchmark_config(
        self, benchmark_id: str, context: ExecutionContext
    ) -> dict[str, Any]:
        """Get benchmark configuration from context (loaded from providers.yaml).

        Args:
            benchmark_id: Benchmark identifier
            context: Execution context with benchmark configuration

        Returns:
            Benchmark configuration dictionary
        """
        # Get config from context (already loaded from providers.yaml by API)
        spec_config = context.benchmark_spec.config or {}

        if "probes" in spec_config:
            return {
                "type": "probes",
                "probes": spec_config["probes"],
                "timeout": spec_config.get("timeout", 3600),
                "eval_threshold": spec_config.get("eval_threshold", 0.5),
            }
        elif "taxonomy_filters" in spec_config:
            return {
                "type": "taxonomy",
                "taxonomy_filters": spec_config["taxonomy_filters"],
                "timeout": spec_config.get("timeout", 3600),
                "eval_threshold": spec_config.get("eval_threshold", 0.5),
            }

        # Fallback if no config (shouldn't happen with proper providers.yaml loading)
        self.logger.warning(
            f"No configuration found for benchmark '{benchmark_id}', using default probe"
        )
        return {
            "type": "probes",
            "probes": ["dan.Dan_11_0"],  # Safe default probe
            "timeout": 3600,
            "eval_threshold": 0.5,
        }

    def _extract_metrics_from_scan_result(
        self, scan_result: dict[str, Any]
    ) -> dict[str, float | int | str]:
        """Extract detailed per-probe metrics from Garak's aggregated results.

        Uses Garak's official aggregated counts and detector scores as the
        authoritative source for probe-specific metrics.

        Args:
            scan_result: Parsed scan result JSON

        Returns:
            Dictionary of per-probe metrics from Garak
        """
        metrics: dict[str, float | int | str] = {}

        # Extract from scores section (Garak's official aggregated results)
        scores = scan_result.get("scores", {})
        for probe_name, probe_data in scores.items():
            if isinstance(probe_data, dict):
                aggregated = probe_data.get("aggregated_results", {})

                # Garak's official probe metrics
                if "attack_success_rate" in aggregated:
                    metrics[f"{probe_name}_attack_success_rate"] = aggregated[
                        "attack_success_rate"
                    ]
                if "total_attempts" in aggregated:
                    metrics[f"{probe_name}_total_attempts"] = aggregated[
                        "total_attempts"
                    ]
                if "vulnerable_responses" in aggregated:
                    metrics[f"{probe_name}_vulnerable_responses"] = aggregated[
                        "vulnerable_responses"
                    ]

                # Extract detector scores (avoid double "mean" suffix)
                detector_scores = aggregated.get("detector_scores", {})
                for detector_name, score in detector_scores.items():
                    # detector_name already has format like "dan.DAN_mean"
                    # so just append it directly
                    metrics[f"{probe_name}_{detector_name}"] = score

                # Extract AVID taxonomy
                metadata = aggregated.get("metadata", {})
                avid_taxonomy = metadata.get("avid_taxonomy", {})
                if avid_taxonomy:
                    risk_domains = avid_taxonomy.get("risk_domain", [])
                    if risk_domains:
                        metrics[f"{probe_name}_risk_domains"] = ",".join(risk_domains)

        return metrics

    def _normalize_metrics(
        self, raw_metrics: dict[str, Any]
    ) -> dict[str, float | int | str]:
        """Normalize raw metrics to consistent format.

        Args:
            raw_metrics: Raw metrics from KFP artifact

        Returns:
            Normalized metrics dictionary
        """
        normalized: dict[str, float | int | str] = {}

        for key, value in raw_metrics.items():
            if isinstance(value, (int, float, str)):
                normalized[key] = value
            elif isinstance(value, bool):
                normalized[key] = 1 if value else 0
            elif isinstance(value, list):
                normalized[key] = ",".join(str(v) for v in value)
            elif isinstance(value, dict):
                # Flatten nested dicts with dot notation
                for sub_key, sub_value in value.items():
                    if isinstance(sub_value, (int, float, str)):
                        normalized[f"{key}.{sub_key}"] = sub_value

        return normalized
