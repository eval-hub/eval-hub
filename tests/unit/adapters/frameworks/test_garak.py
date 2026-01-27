"""Unit tests for Garak security scanning adapter."""

import json
import tempfile
from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

import pytest

from eval_hub.adapters.frameworks.garak import (
    GarakAdapter,
)
from eval_hub.executors.base import ExecutionContext
from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationStatus,
)


class TestGarakAdapter:
    """Test suite for GarakAdapter."""

    @pytest.fixture
    def adapter(self) -> GarakAdapter:
        """Create a GarakAdapter instance."""
        return GarakAdapter()

    @pytest.fixture
    def execution_context_probes(self) -> ExecutionContext:
        """Create a sample execution context with probe-based benchmark."""
        benchmark_spec = BenchmarkSpec(
            name="prompt_injection",
            tasks=["prompt_injection"],
            config={
                "probes": ["promptinject", "dan", "encoding"],
                "timeout": 3600,
            },
        )

        return ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://api.openai.com/v1",
            model_name="gpt-4",
            backend_spec=BackendSpec(
                name="kfp",
                type=BackendType.KFP,
                config={"framework": "garak"},
                benchmarks=[benchmark_spec],
            ),
            benchmark_spec=benchmark_spec,
            timeout_minutes=60,
            retry_attempts=3,
            started_at=datetime.now(UTC),
            metadata={"api_key": "test-key"},
        )

    @pytest.fixture
    def execution_context_taxonomy(self) -> ExecutionContext:
        """Create a sample execution context with taxonomy-based benchmark."""
        benchmark_spec = BenchmarkSpec(
            name="owasp_llm_top10",
            tasks=["owasp_llm_top10"],
            config={
                "taxonomy_filters": ["owasp:llm"],
                "timeout": 43200,
            },
        )

        return ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://model-server/v1",
            model_name="llama-3.1-8b",
            backend_spec=BackendSpec(
                name="kfp",
                type=BackendType.KFP,
                config={"framework": "garak"},
                benchmarks=[benchmark_spec],
            ),
            benchmark_spec=benchmark_spec,
            timeout_minutes=720,  # 12 hours
            retry_attempts=3,
            started_at=datetime.now(UTC),
        )

    @pytest.fixture
    def backend_config(self) -> dict:
        """Create sample backend configuration."""
        return {
            "type": "kubeflow-pipelines",
            "framework": "garak",
            "s3_bucket": "test-bucket",
            "s3_prefix": "garak-results",
        }

    # Initialization Tests

    def test_adapter_initialization(self, adapter: GarakAdapter) -> None:
        """Test adapter initializes correctly."""
        assert adapter.framework_name == "garak"
        assert adapter.version == "1.0"
        assert hasattr(adapter, "logger")  # Has logger for warnings

    def test_adapter_supports_any_benchmark(self, adapter: GarakAdapter) -> None:
        """Test adapter supports any benchmark (relies on providers.yaml)."""
        # Adapter no longer has hardcoded mappings - all configs come from providers.yaml
        assert adapter.supports_benchmark("quick")
        assert adapter.supports_benchmark("custom_test")  # Any ID supported
        assert adapter.supports_benchmark("my_benchmark")

    # KFP Component Specification Tests

    def test_get_kfp_component_spec(self, adapter: GarakAdapter) -> None:
        """Test KFP component specification is valid."""
        spec = adapter.get_kfp_component_spec()

        # Validate required structure
        assert spec["name"] == "garak-security-scan"
        assert "inputs" in spec
        assert "outputs" in spec
        assert "implementation" in spec

        # Validate required inputs exist (no S3 params needed)
        input_names = {inp["name"] for inp in spec["inputs"]}
        required_inputs = {
            "model_url",
            "model_name",
            "benchmark_id",
            "probes",
            "taxonomy_filters",
            "eval_threshold",
            "timeout_seconds",
            "parallel_attempts",
            "generations",
        }
        assert required_inputs.issubset(input_names)

        # Validate outputs exist (using KFP native artifacts)
        output_names = {out["name"] for out in spec["outputs"]}
        assert {"output_metrics", "output_results"}.issubset(output_names)

    def test_get_container_image(self, adapter: GarakAdapter) -> None:
        """Test container image URL is correct."""
        image = adapter.get_container_image()
        assert image.startswith("quay.io/evalhub/garak-kfp:")
        assert "garak-kfp" in image

    # Transform to KFP Args Tests

    def test_transform_to_kfp_args_probe_based(
        self,
        adapter: GarakAdapter,
        execution_context_probes: ExecutionContext,
        backend_config: dict,
    ) -> None:
        """Test context transformation produces valid KFP arguments for probe-based benchmark."""
        args = adapter.transform_to_kfp_args(execution_context_probes, backend_config)

        # Validate model configuration
        assert args["model_url"] == "https://api.openai.com/v1"
        assert args["model_name"] == "gpt-4"
        assert args["api_key"] == "test-key"

        # Validate benchmark configuration
        assert args["benchmark_id"] == "prompt_injection"
        assert args["probes"] == ["promptinject", "dan", "encoding"]
        assert args["taxonomy_filters"] == []

        # Validate execution parameters
        assert args["eval_threshold"] == 0.5
        assert args["parallel_attempts"] == 8
        assert args["generations"] == 1

        # Validate S3 configuration (for easy evalhub retrieval)
        assert args["s3_bucket"] == "test-bucket"
        assert args["s3_prefix"] == "garak-results"
        assert "job_id" in args  # Should be set to evaluation_id

    def test_transform_to_kfp_args_taxonomy_based(
        self,
        adapter: GarakAdapter,
        execution_context_taxonomy: ExecutionContext,
        backend_config: dict,
    ) -> None:
        """Test context transformation for taxonomy-based benchmark."""
        args = adapter.transform_to_kfp_args(execution_context_taxonomy, backend_config)

        # Validate taxonomy configuration
        assert args["benchmark_id"] == "owasp_llm_top10"
        assert args["probes"] == []
        assert args["taxonomy_filters"] == ["owasp:llm"]

    def test_transform_to_kfp_args_uses_predefined_benchmark(
        self,
        adapter: GarakAdapter,
        backend_config: dict,
    ) -> None:
        """Test transformation uses predefined benchmark when no config specified."""
        # Create context with predefined benchmark ID but no explicit probes
        benchmark_spec = BenchmarkSpec(
            name="quick",
            tasks=["quick"],
            config={},  # No explicit probes
        )

        context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://api.openai.com/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp",
                type=BackendType.KFP,
                config={"framework": "garak"},
                benchmarks=[benchmark_spec],
            ),
            benchmark_spec=benchmark_spec,
            timeout_minutes=30,
            retry_attempts=3,
            started_at=datetime.now(UTC),
        )

        args = adapter.transform_to_kfp_args(context, backend_config)

        # With empty config, uses fallback default
        assert args["probes"] == ["dan.Dan_11_0"]  # Fallback default
        assert args["timeout_seconds"] == 600  # Updated default (10 minutes)

    # Parse KFP Output Tests

    def test_parse_kfp_output_success(
        self, adapter: GarakAdapter, execution_context_probes: ExecutionContext
    ) -> None:
        """Test successful parsing of KFP outputs."""
        with tempfile.TemporaryDirectory() as tmpdir:
            results_path = Path(tmpdir) / "results.json"
            metrics_path = Path(tmpdir) / "metrics.json"

            # Write sample detailed results
            results_data = {
                "generations": [
                    {"probe": "dan.DAN", "vulnerable": True, "prompt": "test"},
                    {"probe": "dan.DAN", "vulnerable": False, "prompt": "test2"},
                    {"probe": "encoding", "vulnerable": True, "prompt": "test3"},
                ],
                "scores": {
                    "dan.DAN": {
                        "aggregated_results": {
                            "total_attempts": 2,
                            "vulnerable_responses": 1,
                            "attack_success_rate": 50.0,
                        }
                    }
                },
            }
            with open(results_path, "w") as f:
                json.dump(results_data, f)

            # Write sample metrics
            metrics_data = {
                "total_attempts": 3,
                "vulnerable_attempts": 2,
                "attack_success_rate": 66.67,
                "security_score": 33.33,
            }
            with open(metrics_path, "w") as f:
                json.dump(metrics_data, f)

            artifacts = {
                "output_results": str(results_path),
                "output_metrics": str(metrics_path),
            }

            result = adapter.parse_kfp_output(artifacts, execution_context_probes)

            # Validate result structure
            assert result.evaluation_id == execution_context_probes.evaluation_id
            assert result.provider_id == "garak"
            assert result.benchmark_id == "prompt_injection"
            assert result.status == EvaluationStatus.COMPLETED
            assert len(result.metrics) > 0
            assert result.metrics["total_attempts"] == 3
            assert result.metrics["attack_success_rate"] == 66.67

    def test_parse_kfp_output_with_avid_taxonomy(
        self, adapter: GarakAdapter, execution_context_probes: ExecutionContext
    ) -> None:
        """Test parsing extracts AVID taxonomy information."""
        with tempfile.TemporaryDirectory() as tmpdir:
            results_path = Path(tmpdir) / "results.json"

            results_data = {
                "generations": [
                    {"probe": "dan.DAN", "vulnerable": True, "prompt": "test"},
                ],
                "scores": {
                    "dan.DAN": {
                        "aggregated_results": {
                            "total_attempts": 1,
                            "vulnerable_responses": 1,
                            "attack_success_rate": 100.0,
                            "metadata": {
                                "avid_taxonomy": {
                                    "risk_domain": ["Security"],
                                    "sep_view": ["S0401"],
                                }
                            },
                        }
                    }
                },
            }
            with open(results_path, "w") as f:
                json.dump(results_data, f)

            artifacts = {"output_results": str(results_path)}

            result = adapter.parse_kfp_output(artifacts, execution_context_probes)

            # Validate AVID taxonomy extraction
            assert "dan.DAN_risk_domains" in result.metrics
            assert result.metrics["dan.DAN_risk_domains"] == "Security"

    def test_parse_kfp_output_missing_files(
        self, adapter: GarakAdapter, execution_context_probes: ExecutionContext
    ) -> None:
        """Test parsing handles missing artifact files gracefully."""
        artifacts = {"output_results": "/nonexistent/results.json"}

        result = adapter.parse_kfp_output(artifacts, execution_context_probes)

        # Should return failed result (no metrics)
        assert result.evaluation_id == execution_context_probes.evaluation_id
        assert result.status == EvaluationStatus.FAILED
        assert result.error_message is not None

    def test_parse_kfp_output_empty_result(
        self, adapter: GarakAdapter, execution_context_probes: ExecutionContext
    ) -> None:
        """Test parsing handles empty scan results."""
        with tempfile.TemporaryDirectory() as tmpdir:
            results_path = Path(tmpdir) / "results.json"

            # Write empty result
            with open(results_path, "w") as f:
                json.dump({}, f)

            artifacts = {"output_results": str(results_path)}

            result = adapter.parse_kfp_output(artifacts, execution_context_probes)

            # Should return failed result due to no metrics
            assert result.status == EvaluationStatus.FAILED

    # Validation Tests

    def test_validate_config(self, adapter: GarakAdapter) -> None:
        """Test config validation accepts valid configs."""
        assert adapter.validate_config({}) is True
        assert adapter.validate_config({"framework": "garak"}) is True

    def test_validate_config_rejects_wrong_framework(
        self, adapter: GarakAdapter
    ) -> None:
        """Test config validation rejects incorrect framework."""
        with pytest.raises(ValueError, match="Invalid framework"):
            adapter.validate_config({"framework": "wrong-framework"})

    # Benchmark Support Tests

    def test_supports_benchmark(self, adapter: GarakAdapter) -> None:
        """Test benchmark support checking."""
        # Known benchmarks
        assert adapter.supports_benchmark("quick") is True
        assert adapter.supports_benchmark("owasp_llm_top10") is True
        assert adapter.supports_benchmark("toxicity") is True

        # Unknown benchmarks (should still return True for custom configs)
        assert adapter.supports_benchmark("custom_benchmark") is True

    # Metrics Extraction Tests

    def test_extract_metrics_from_scan_result(self, adapter: GarakAdapter) -> None:
        """Test metrics extraction from scan result (detailed probe metrics only)."""
        scan_result = {
            "scores": {
                "dan.DAN": {
                    "aggregated_results": {
                        "attack_success_rate": 50.0,
                        "total_attempts": 2,
                        "vulnerable_responses": 1,
                        "detector_scores": {
                            "dan.DAN_mean": 50.0,
                            "mitigation.MitigationBypass_mean": 25.0,
                        },
                        "metadata": {
                            "avid_taxonomy": {
                                "risk_domain": ["Security", "Performance"]
                            }
                        },
                    }
                },
            },
        }

        metrics = adapter._extract_metrics_from_scan_result(scan_result)

        # Verify per-probe metrics from Garak's aggregated results
        assert metrics["dan.DAN_attack_success_rate"] == 50.0
        assert metrics["dan.DAN_total_attempts"] == 2
        assert metrics["dan.DAN_vulnerable_responses"] == 1
        assert metrics["dan.DAN_risk_domains"] == "Security,Performance"

        # Verify detector scores
        assert metrics["dan.DAN_dan.DAN_mean"] == 50.0
        assert metrics["dan.DAN_mitigation.MitigationBypass_mean"] == 25.0

    def test_normalize_metrics(self, adapter: GarakAdapter) -> None:
        """Test metrics normalization."""
        raw_metrics = {
            "score": 0.85,
            "count": 100,
            "name": "test",
            "enabled": True,
            "tags": ["a", "b"],
            "nested": {"key": "value"},
        }

        normalized = adapter._normalize_metrics(raw_metrics)

        assert normalized["score"] == 0.85
        assert normalized["count"] == 100
        assert normalized["name"] == "test"
        assert normalized["enabled"] == 1  # Bool converted to int
        assert normalized["tags"] == "a,b"  # List joined
        assert normalized["nested.key"] == "value"  # Nested flattened


class TestGarakAdapterIntegration:
    """Integration tests for GarakAdapter with AdapterRegistry."""

    @pytest.fixture
    def adapter(self) -> GarakAdapter:
        """Create a GarakAdapter instance."""
        return GarakAdapter()

    def test_adapter_registered_in_registry(self) -> None:
        """Test GarakAdapter is properly registered."""
        from eval_hub.adapters.registry import AdapterRegistry

        assert AdapterRegistry.is_registered("garak")

    def test_adapter_can_be_retrieved(self) -> None:
        """Test GarakAdapter can be retrieved from registry."""
        from eval_hub.adapters.registry import AdapterRegistry

        adapter = AdapterRegistry.get_adapter("garak")

        assert isinstance(adapter, GarakAdapter)
        assert adapter.framework_name == "garak"

    # Custom Benchmark Configuration Tests

    def test_custom_benchmark_with_probes(self, adapter: GarakAdapter) -> None:
        """Test adapter handles custom benchmark IDs with probe config."""
        custom_context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://model/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp",
                type=BackendType.KFP,
                config={"framework": "garak", "s3_bucket": "test", "s3_prefix": "test"},
                benchmarks=[],
            ),
            benchmark_spec=BenchmarkSpec(
                name="my_custom_test",  # Custom benchmark ID
                tasks=["my_custom_test"],
                config={
                    "probes": ["malwaregen.Evasion", "xss.MarkdownExfiltration"],
                    "timeout": 2400,
                    "eval_threshold": 0.7,
                },
            ),
            timeout_minutes=60,
            retry_attempts=3,
            started_at=datetime.now(UTC),
        )

        backend_config = {
            "framework": "garak",
            "s3_bucket": "test-bucket",
            "s3_prefix": "custom-tests",
        }

        args = adapter.transform_to_kfp_args(custom_context, backend_config)

        # Verify custom probes are used
        assert args["benchmark_id"] == "my_custom_test"
        assert args["probes"] == ["malwaregen.Evasion", "xss.MarkdownExfiltration"]
        assert args["taxonomy_filters"] == []
        assert args["timeout_seconds"] == 2400
        assert args["eval_threshold"] == 0.7

    def test_custom_benchmark_with_taxonomy(self, adapter: GarakAdapter) -> None:
        """Test adapter handles custom benchmark with taxonomy filters."""
        custom_context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://model/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp",
                type=BackendType.KFP,
                config={"framework": "garak", "s3_bucket": "test", "s3_prefix": "test"},
                benchmarks=[],
            ),
            benchmark_spec=BenchmarkSpec(
                name="my_avid_scan",
                tasks=["my_avid_scan"],
                config={
                    "taxonomy_filters": ["owasp:llm01", "avid-effect:security"],
                    "timeout": 5400,
                },
            ),
            timeout_minutes=120,
            retry_attempts=3,
            started_at=datetime.now(UTC),
        )

        backend_config = {
            "framework": "garak",
            "s3_bucket": "test",
            "s3_prefix": "test",
        }
        args = adapter.transform_to_kfp_args(custom_context, backend_config)

        # Verify taxonomy filters are used
        assert args["probes"] == []
        assert args["taxonomy_filters"] == ["owasp:llm01", "avid-effect:security"]
        assert args["timeout_seconds"] == 5400

    def test_config_override_uses_request_values(self, adapter: GarakAdapter) -> None:
        """Test that request config overrides benchmark defaults from providers.yaml."""
        # Simulate benchmark with config from providers.yaml + request override
        override_context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://model/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp",
                type=BackendType.KFP,
                config={"framework": "garak", "s3_bucket": "test", "s3_prefix": "test"},
                benchmarks=[],
            ),
            benchmark_spec=BenchmarkSpec(
                name="quick",
                tasks=["quick"],
                # Merged config: providers.yaml defaults + request overrides
                config={
                    "probes": ["dan.Dan_13_0"],  # Override from request
                    "timeout": 1200,  # Override from request
                },
            ),
            timeout_minutes=30,
            retry_attempts=3,
            started_at=datetime.now(UTC),
        )

        backend_config = {
            "framework": "garak",
            "s3_bucket": "test",
            "s3_prefix": "test",
        }
        args = adapter.transform_to_kfp_args(override_context, backend_config)

        # Verify overrides are respected
        assert args["probes"] == ["dan.Dan_13_0"]  # Not default probes
        assert args["timeout_seconds"] == 1200  # Not default timeout
