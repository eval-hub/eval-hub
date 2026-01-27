"""Integration tests for Garak security scanning framework.

This module contains end-to-end integration tests for the Garak adapter,
KFP executor, and result parsing pipeline.
"""

import json
import tempfile
from datetime import UTC, datetime
from pathlib import Path
from unittest.mock import Mock, patch
from uuid import uuid4

import pytest

from eval_hub.adapters.frameworks.garak import GarakAdapter

# Check if KFP is available (needed for certain integration tests)
try:
    import kfp  # noqa: F401

    KFP_AVAILABLE = True
except ImportError:
    KFP_AVAILABLE = False
from eval_hub.executors.base import ExecutionContext
from eval_hub.executors.kfp import KFPExecutor
from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationStatus,
)


@pytest.fixture
def garak_backend_config():
    """Backend configuration for Garak/KFP executor."""
    return {
        "kfp_endpoint": "http://kfp.example.com",
        "namespace": "kubeflow",
        "experiment_name": "garak-security-scan",
        "framework": "garak",
        "poll_interval_seconds": 1,
        "enable_caching": False,
        "s3_bucket": "garak-results",
        "s3_prefix": "scans",
        "s3_credentials_secret": "aws-credentials",
    }


@pytest.fixture
def garak_execution_context_quick():
    """Create execution context for quick Garak scan."""
    evaluation_id = uuid4()
    benchmark_spec = BenchmarkSpec(
        name="quick",
        tasks=["quick"],
        config={
            "probes": ["dan.Dan_11_0"],
            "timeout": 300,
            "eval_threshold": 0.5,
        },
    )
    backend_spec = BackendSpec(
        name="kfp-garak",
        type=BackendType.KFP,
        config={"framework": "garak"},
        benchmarks=[benchmark_spec],
    )
    return ExecutionContext(
        evaluation_id=evaluation_id,
        model_url="https://api.example.com/v1",
        model_name="test-model",
        backend_spec=backend_spec,
        benchmark_spec=benchmark_spec,
        timeout_minutes=30,
        retry_attempts=2,
        started_at=datetime.now(UTC),
    )


@pytest.fixture
def garak_execution_context_taxonomy():
    """Create execution context for taxonomy-based Garak scan."""
    evaluation_id = uuid4()
    benchmark_spec = BenchmarkSpec(
        name="owasp_llm_top10",
        tasks=["owasp_llm_top10"],
        config={
            "taxonomy_filters": ["owasp:llm"],
            "timeout": 7200,
            "eval_threshold": 0.5,
        },
    )
    backend_spec = BackendSpec(
        name="kfp-garak",
        type=BackendType.KFP,
        config={"framework": "garak"},
        benchmarks=[benchmark_spec],
    )
    return ExecutionContext(
        evaluation_id=evaluation_id,
        model_url="https://api.example.com/v1",
        model_name="test-model-taxonomy",
        backend_spec=backend_spec,
        benchmark_spec=benchmark_spec,
        timeout_minutes=120,
        retry_attempts=2,
        started_at=datetime.now(UTC),
    )


@pytest.fixture
def sample_garak_results():
    """Sample Garak scan results."""
    return {
        "generations": [
            {
                "probe": "dan.Dan_11_0",
                "detector": "dan.DAN_detector",
                "prompt": "Test prompt",
                "response": "Test response",
                "score": 0.2,
                "vulnerable": False,
                "attempt_number": 1,
            },
            {
                "probe": "dan.Dan_11_0",
                "detector": "dan.DAN_detector",
                "prompt": "Jailbreak prompt",
                "response": "Bypassed response",
                "score": 0.8,
                "vulnerable": True,
                "attempt_number": 2,
            },
        ],
        "scores": {
            "dan.Dan_11_0": {
                "aggregated_results": {
                    "total_attempts": 2,
                    "vulnerable_responses": 1,
                    "attack_success_rate": 50.0,
                    "detector_scores": {"dan.DAN_mean": 0.5},
                    "metadata": {
                        "avid_taxonomy": {
                            "risk_domain": ["security", "performance"],
                        }
                    },
                }
            }
        },
        "summary": {
            "total_attempts": 2,
            "vulnerable_attempts": 1,
            "safe_attempts": 1,
            "attack_success_rate": 50.0,
            "security_score": 50.0,
            "eval_threshold": 0.5,
            "probes_tested": ["dan.Dan_11_0"],
        },
    }


@pytest.fixture
def sample_garak_metrics():
    """Sample Garak metrics artifact."""
    return {
        "total_attempts": 2,
        "vulnerable_attempts": 1,
        "attack_success_rate": 50.0,
        "security_score": 50.0,
    }


@pytest.mark.integration
class TestGarakEndToEnd:
    """Integration tests for complete Garak evaluation workflow."""

    def test_garak_adapter_initialization(self):
        """Test Garak adapter can be instantiated."""
        adapter = GarakAdapter()
        assert adapter.framework_name == "garak"
        assert adapter.version == "1.0"

    def test_transform_probe_based_scan(
        self, garak_execution_context_quick, garak_backend_config
    ):
        """Test transformation of probe-based scan configuration."""
        adapter = GarakAdapter()

        args = adapter.transform_to_kfp_args(
            garak_execution_context_quick, garak_backend_config
        )

        assert args["model_url"] == "https://api.example.com/v1"
        assert args["model_name"] == "test-model"
        assert args["benchmark_id"] == "quick"
        assert args["probes"] == ["dan.Dan_11_0"]
        assert args["taxonomy_filters"] == []
        assert args["timeout_seconds"] == 300
        assert args["s3_bucket"] == "garak-results"
        assert args["s3_prefix"] == "scans"

    def test_transform_taxonomy_based_scan(
        self, garak_execution_context_taxonomy, garak_backend_config
    ):
        """Test transformation of taxonomy-based scan configuration."""
        adapter = GarakAdapter()

        args = adapter.transform_to_kfp_args(
            garak_execution_context_taxonomy, garak_backend_config
        )

        assert args["model_name"] == "test-model-taxonomy"
        assert args["benchmark_id"] == "owasp_llm_top10"
        assert args["probes"] == []
        assert args["taxonomy_filters"] == ["owasp:llm"]
        assert args["timeout_seconds"] == 7200

    def test_parse_successful_results(
        self,
        garak_execution_context_quick,
        sample_garak_results,
        sample_garak_metrics,
    ):
        """Test parsing of successful Garak scan results."""
        adapter = GarakAdapter()

        with tempfile.TemporaryDirectory() as tmpdir:
            # Create result artifacts
            metrics_path = Path(tmpdir) / "metrics.json"
            results_path = Path(tmpdir) / "results.json"

            with open(metrics_path, "w") as f:
                json.dump(sample_garak_metrics, f)

            with open(results_path, "w") as f:
                json.dump(sample_garak_results, f)

            # Parse outputs
            artifacts = {
                "output_metrics": str(metrics_path),
                "output_results": str(results_path),
            }

            result = adapter.parse_kfp_output(artifacts, garak_execution_context_quick)

            # Verify result structure
            assert result.status == EvaluationStatus.COMPLETED
            assert result.evaluation_id == garak_execution_context_quick.evaluation_id
            assert result.provider_id == "garak"
            assert result.benchmark_id == "quick"

            # Verify metrics
            assert result.metrics["total_attempts"] == 2
            assert result.metrics["vulnerable_attempts"] == 1
            assert result.metrics["attack_success_rate"] == 50.0
            assert result.metrics["security_score"] == 50.0

            # Verify per-probe metrics
            assert "dan.Dan_11_0_attack_success_rate" in result.metrics
            assert result.metrics["dan.Dan_11_0_attack_success_rate"] == 50.0
            assert "dan.Dan_11_0_total_attempts" in result.metrics
            assert result.metrics["dan.Dan_11_0_total_attempts"] == 2

    def test_parse_results_with_missing_files(self, garak_execution_context_quick):
        """Test parsing with missing artifact files."""
        adapter = GarakAdapter()

        artifacts = {
            "output_metrics": "/nonexistent/metrics.json",
            "output_results": "/nonexistent/results.json",
        }

        result = adapter.parse_kfp_output(artifacts, garak_execution_context_quick)

        # Should return FAILED status when files are missing
        assert result.status == EvaluationStatus.FAILED
        assert result.error_message == "No metrics extracted from scan results"
        assert result.metrics == {}

    def test_parse_results_with_empty_metrics(self, garak_execution_context_quick):
        """Test parsing with empty metrics."""
        adapter = GarakAdapter()

        with tempfile.TemporaryDirectory() as tmpdir:
            metrics_path = Path(tmpdir) / "metrics.json"

            # Create empty metrics
            with open(metrics_path, "w") as f:
                json.dump({}, f)

            artifacts = {"output_metrics": str(metrics_path)}

            result = adapter.parse_kfp_output(artifacts, garak_execution_context_quick)

            assert result.status == EvaluationStatus.FAILED
            assert "No metrics extracted" in result.error_message

    @pytest.mark.asyncio
    @pytest.mark.skipif(not KFP_AVAILABLE, reason="KFP package not installed")
    async def test_kfp_executor_with_garak(
        self, garak_backend_config, garak_execution_context_quick
    ):
        """Test KFP executor execution flow with Garak adapter."""
        with (
            patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class,
            patch("eval_hub.executors.kfp.asyncio.sleep"),
            patch(
                "eval_hub.executors.tracked.TrackedExecutor._track_start"
            ) as mock_track_start,
            patch(
                "eval_hub.executors.tracked.TrackedExecutor._track_complete"
            ) as mock_track_complete,
        ):
            # Setup mocks
            mock_registry = Mock()
            mock_adapter = GarakAdapter()
            mock_registry.get_adapter.return_value = mock_adapter
            mock_registry_class.return_value = mock_registry

            async def mock_track_start_impl(*args, **kwargs):
                return "test-run-id"

            async def mock_track_complete_impl(*args, **kwargs):
                return None

            mock_track_start.side_effect = mock_track_start_impl
            mock_track_complete.side_effect = mock_track_complete_impl

            # Create executor
            executor = KFPExecutor(garak_backend_config)
            executor.adapter_registry = mock_registry

            # Mock KFP client and its methods
            mock_kfp_client = Mock()
            mock_experiment = Mock()
            mock_experiment.experiment_id = "exp-123"
            mock_run = Mock()
            mock_run.run_id = "run-456"
            mock_run_detail = Mock()
            mock_run_detail.state = "Succeeded"

            # mock methods
            mock_kfp_client.create_experiment = Mock(return_value=mock_experiment)
            mock_kfp_client.run_pipeline = Mock(return_value=mock_run)
            mock_kfp_client.get_run = Mock(return_value=mock_run_detail)

            async def mock_to_thread(func, *args, **kwargs):
                return func(*args, **kwargs)

            with (
                patch.object(executor, "_get_kfp_client", return_value=mock_kfp_client),
                patch(
                    "eval_hub.executors.kfp.asyncio.to_thread",
                    side_effect=mock_to_thread,
                ),
                patch("kfp.compiler.Compiler"),
                patch.object(
                    executor,
                    "_download_artifacts_from_s3",
                ) as mock_download,
            ):
                # Setup artifacts
                with tempfile.TemporaryDirectory() as tmpdir:
                    metrics_path = Path(tmpdir) / "metrics.json"
                    results_path = Path(tmpdir) / "results.json"

                    with open(metrics_path, "w") as f:
                        json.dump(
                            {
                                "total_attempts": 10,
                                "vulnerable_attempts": 2,
                                "attack_success_rate": 20.0,
                                "security_score": 80.0,
                            },
                            f,
                        )

                    with open(results_path, "w") as f:
                        json.dump(
                            {
                                "generations": [],
                                "scores": {},
                                "summary": {
                                    "total_attempts": 10,
                                    "vulnerable_attempts": 2,
                                },
                            },
                            f,
                        )

                    async def mock_download_impl(*args, **kwargs):
                        return {
                            "output_metrics": str(metrics_path),
                            "output_results": str(results_path),
                        }

                    mock_download.side_effect = mock_download_impl

                    result = await executor.execute_benchmark(
                        garak_execution_context_quick
                    )

                    assert result.status == EvaluationStatus.COMPLETED
                    assert result.metrics["attack_success_rate"] == 20.0
                    assert result.metrics["security_score"] == 80.0

    @pytest.mark.asyncio
    @pytest.mark.skipif(not KFP_AVAILABLE, reason="KFP package not installed")
    async def test_kfp_executor_handles_pipeline_failure(
        self, garak_backend_config, garak_execution_context_quick
    ):
        """Test KFP executor handles pipeline failures correctly."""
        with (
            patch("eval_hub.executors.kfp.AdapterRegistry") as mock_registry_class,
            patch("eval_hub.executors.kfp.asyncio.sleep"),
            patch(
                "eval_hub.executors.tracked.TrackedExecutor._track_start"
            ) as mock_track_start,
            patch(
                "eval_hub.executors.tracked.TrackedExecutor._track_failure"
            ) as mock_track_failure,
        ):
            mock_registry = Mock()
            mock_adapter = GarakAdapter()
            mock_registry.get_adapter.return_value = mock_adapter
            mock_registry_class.return_value = mock_registry

            # async mock
            async def mock_track_start_impl(*args, **kwargs):
                return "test-run-id"

            async def mock_track_failure_impl(*args, **kwargs):
                return "failed-run-id"

            mock_track_start.side_effect = mock_track_start_impl
            mock_track_failure.side_effect = mock_track_failure_impl

            executor = KFPExecutor(garak_backend_config)
            executor.adapter_registry = mock_registry

            # Mock pipeline failure
            mock_kfp_client = Mock()
            mock_experiment = Mock()
            mock_experiment.experiment_id = "exp-123"
            mock_run = Mock()
            mock_run.run_id = "run-456"
            mock_run_detail = Mock()
            mock_run_detail.state = "Failed"
            mock_run_detail.run = Mock()
            mock_run_detail.run.error = "Pipeline execution failed"

            mock_kfp_client.create_experiment = Mock(return_value=mock_experiment)
            mock_kfp_client.run_pipeline = Mock(return_value=mock_run)
            mock_kfp_client.get_run = Mock(return_value=mock_run_detail)

            async def mock_to_thread(func, *args, **kwargs):
                return func(*args, **kwargs)

            with (
                patch.object(executor, "_get_kfp_client", return_value=mock_kfp_client),
                patch(
                    "eval_hub.executors.kfp.asyncio.to_thread",
                    side_effect=mock_to_thread,
                ),
                patch("kfp.compiler.Compiler"),
            ):
                result = await executor.execute_benchmark(garak_execution_context_quick)

                assert result.status == EvaluationStatus.FAILED
                assert result.error_message is not None
                assert "failed" in result.error_message.lower()

    def test_garak_component_spec_generation(self):
        """Test that Garak generates valid KFP component spec."""
        adapter = GarakAdapter()
        spec = adapter.get_kfp_component_spec()

        # Verify spec structure
        assert spec["name"] == "garak-security-scan"
        assert "description" in spec
        assert "inputs" in spec
        assert "outputs" in spec
        assert "implementation" in spec

        # Verify inputs
        input_names = [inp["name"] for inp in spec["inputs"]]
        assert "model_url" in input_names
        assert "model_name" in input_names
        assert "benchmark_id" in input_names
        assert "probes" in input_names
        assert "taxonomy_filters" in input_names
        assert "timeout_seconds" in input_names
        assert "s3_bucket" in input_names

        # Verify outputs
        output_names = [out["name"] for out in spec["outputs"]]
        assert "output_metrics" in output_names
        assert "output_results" in output_names

        # Verify container implementation
        container = spec["implementation"]["container"]
        assert "image" in container
        assert "command" in container
        assert "args" in container

    def test_timeout_configuration_respected(self, garak_backend_config):
        """Test that timeout configuration is properly propagated."""
        adapter = GarakAdapter()

        # Test with quick scan (short timeout)
        quick_context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://api.example.com/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp-garak",
                type=BackendType.KFP,
                config={"framework": "garak"},
                benchmarks=[
                    BenchmarkSpec(
                        name="quick",
                        tasks=["quick"],
                        config={"probes": ["dan.Dan_11_0"], "timeout": 300},
                    )
                ],
            ),
            benchmark_spec=BenchmarkSpec(
                name="quick",
                tasks=["quick"],
                config={"probes": ["dan.Dan_11_0"], "timeout": 300},
            ),
            timeout_minutes=30,
            retry_attempts=2,
            started_at=datetime.now(UTC),
        )

        args = adapter.transform_to_kfp_args(quick_context, garak_backend_config)
        assert args["timeout_seconds"] == 300

        comprehensive_context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://api.example.com/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp-garak",
                type=BackendType.KFP,
                config={"framework": "garak"},
                benchmarks=[
                    BenchmarkSpec(
                        name="owasp_llm_top10",
                        tasks=["owasp_llm_top10"],
                        config={
                            "taxonomy_filters": ["owasp:llm"],
                            "timeout": 43200,
                        },
                    )
                ],
            ),
            benchmark_spec=BenchmarkSpec(
                name="owasp_llm_top10",
                tasks=["owasp_llm_top10"],
                config={"taxonomy_filters": ["owasp:llm"], "timeout": 43200},
            ),
            timeout_minutes=720,
            retry_attempts=2,
            started_at=datetime.now(UTC),
        )

        args = adapter.transform_to_kfp_args(
            comprehensive_context, garak_backend_config
        )
        assert args["timeout_seconds"] == 43200

    def test_avid_taxonomy_parsing(self, garak_execution_context_quick):
        """Test parsing of AVID taxonomy metadata from results."""
        adapter = GarakAdapter()

        results_with_avid = {
            "generations": [],
            "scores": {
                "dan.Dan_11_0": {
                    "aggregated_results": {
                        "total_attempts": 5,
                        "vulnerable_responses": 2,
                        "attack_success_rate": 40.0,
                        "metadata": {
                            "avid_taxonomy": {
                                "risk_domain": ["security", "ethics"],
                                "sep_view": ["E0101", "S0201"],
                            }
                        },
                    }
                }
            },
            "summary": {
                "total_attempts": 5,
                "vulnerable_attempts": 2,
                "attack_success_rate": 40.0,
            },
        }

        with tempfile.TemporaryDirectory() as tmpdir:
            metrics_path = Path(tmpdir) / "metrics.json"
            results_path = Path(tmpdir) / "results.json"

            with open(metrics_path, "w") as f:
                json.dump({"attack_success_rate": 40.0}, f)

            with open(results_path, "w") as f:
                json.dump(results_with_avid, f)

            artifacts = {
                "output_metrics": str(metrics_path),
                "output_results": str(results_path),
            }

            result = adapter.parse_kfp_output(artifacts, garak_execution_context_quick)

            # Verify AVID taxonomy is extracted
            assert "dan.Dan_11_0_risk_domains" in result.metrics
            assert "security,ethics" in result.metrics["dan.Dan_11_0_risk_domains"]


@pytest.mark.integration
class TestGarakErrorScenarios:
    """Integration tests for Garak error handling."""

    def test_invalid_framework_config(self):
        """Test that invalid framework configuration is rejected."""
        adapter = GarakAdapter()

        with pytest.raises(ValueError, match="Invalid framework"):
            adapter.validate_config({"framework": "not-garak"})

    def test_empty_benchmark_config_uses_fallback(self, garak_backend_config):
        """Test that empty benchmark config uses fallback defaults."""
        adapter = GarakAdapter()

        context = ExecutionContext(
            evaluation_id=uuid4(),
            model_url="https://api.example.com/v1",
            model_name="test-model",
            backend_spec=BackendSpec(
                name="kfp-garak",
                type=BackendType.KFP,
                config={"framework": "garak"},
                benchmarks=[
                    BenchmarkSpec(name="unknown", tasks=["unknown"], config={})
                ],
            ),
            benchmark_spec=BenchmarkSpec(name="unknown", tasks=["unknown"], config={}),
            timeout_minutes=30,
            retry_attempts=2,
            started_at=datetime.now(UTC),
        )

        args = adapter.transform_to_kfp_args(context, garak_backend_config)

        # Should use fallback defaults
        assert args["probes"] == ["dan.Dan_11_0"]
        assert args["timeout_seconds"] == 600  # Updated default

    def test_malformed_json_in_results(self, garak_execution_context_quick):
        """Test handling of malformed JSON in result files."""
        adapter = GarakAdapter()

        with tempfile.TemporaryDirectory() as tmpdir:
            results_path = Path(tmpdir) / "results.json"

            with open(results_path, "w") as f:
                f.write("{invalid json content")

            artifacts = {"output_results": str(results_path)}

            # Should handle gracefully
            result = adapter.parse_kfp_output(artifacts, garak_execution_context_quick)

            assert result.status == EvaluationStatus.FAILED
