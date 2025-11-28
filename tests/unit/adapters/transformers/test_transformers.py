"""Unit tests for adapter transformers."""

import tempfile
from pathlib import Path
from uuid import uuid4

import pytest

from eval_hub.adapters.transformers import (
    BenchmarkConfigTransformer,
    MetricExtractor,
    ModelConfigTransformer,
)
from eval_hub.executors.base import ExecutionContext
from eval_hub.models.evaluation import BackendSpec, BenchmarkSpec

# ModelConfigTransformer Tests


@pytest.fixture
def model_transformer():
    """Fixture for ModelConfigTransformer."""
    return ModelConfigTransformer()


@pytest.fixture
def execution_context():
    """Fixture for ExecutionContext."""
    benchmark_spec = BenchmarkSpec(name="test", tasks=["task1"])
    return ExecutionContext(
        evaluation_id=uuid4(),
        model_url="http://test-model:8000",
        model_name="test-model",
        backend_spec=BackendSpec(
            name="test-backend",
            type="custom",
            config={},
            benchmarks=[benchmark_spec],
        ),
        benchmark_spec=benchmark_spec,
        timeout_minutes=60,
        retry_attempts=3,
    )


def test_model_transformer_extract_basic(model_transformer, execution_context):
    """Test basic model config extraction."""
    backend_config = {}
    model_config = model_transformer.extract(execution_context, backend_config)

    assert model_config.url == "http://test-model:8000"
    assert model_config.name == "test-model"
    assert model_config.configuration == {}


def test_model_transformer_extract_with_config(model_transformer, execution_context):
    """Test model config extraction with additional configuration."""
    backend_config = {
        "model_configuration": {
            "temperature": 0.7,
            "max_tokens": 100,
        }
    }
    model_config = model_transformer.extract(execution_context, backend_config)

    assert model_config.configuration == {"temperature": 0.7, "max_tokens": 100}


def test_model_transformer_extract_with_overrides(model_transformer, execution_context):
    """Test model config extraction with URL/name overrides."""
    backend_config = {
        "model_url_override": "http://override:9000",
        "model_name_override": "override-model",
    }
    model_config = model_transformer.extract(execution_context, backend_config)

    assert model_config.url == "http://override:9000"
    assert model_config.name == "override-model"


def test_model_transformer_validate_valid(model_transformer):
    """Test validation of valid model config."""
    from eval_hub.adapters.transformers.model import ModelConfig

    model_config = ModelConfig(url="http://test:8000", name="test-model")
    assert model_transformer.validate_model_config(model_config)


def test_model_transformer_validate_missing_url(model_transformer):
    """Test validation fails with missing URL."""
    from eval_hub.adapters.transformers.model import ModelConfig

    model_config = ModelConfig(url="", name="test-model")

    with pytest.raises(ValueError, match="Model URL is required"):
        model_transformer.validate_model_config(model_config)


def test_model_transformer_validate_missing_name(model_transformer):
    """Test validation fails with missing name."""
    from eval_hub.adapters.transformers.model import ModelConfig

    model_config = ModelConfig(url="http://test:8000", name="")

    with pytest.raises(ValueError, match="Model name is required"):
        model_transformer.validate_model_config(model_config)


# BenchmarkConfigTransformer Tests


@pytest.fixture
def benchmark_transformer():
    """Fixture for BenchmarkConfigTransformer."""
    return BenchmarkConfigTransformer()


@pytest.fixture
def benchmark_spec():
    """Fixture for BenchmarkSpec."""
    return BenchmarkSpec(
        name="mmlu",
        tasks=["mmlu_anatomy", "mmlu_algebra"],
        num_fewshot=5,
        batch_size=32,
        limit=100,
        device="cuda",
    )


def test_benchmark_transformer_extract_basic(benchmark_transformer, benchmark_spec):
    """Test basic benchmark config extraction."""
    backend_config = {}
    config = benchmark_transformer.extract(benchmark_spec, backend_config)

    assert config["num_fewshot"] == 5
    assert config["batch_size"] == 32
    assert config["limit"] == 100
    assert config["device"] == "cuda"


def test_benchmark_transformer_extract_with_overrides(
    benchmark_transformer, benchmark_spec
):
    """Test benchmark config extraction with backend overrides."""
    backend_config = {
        "benchmark_config": {
            "num_fewshot": 10,
            "custom_param": "value",
        }
    }
    config = benchmark_transformer.extract(benchmark_spec, backend_config)

    # Backend config should override
    assert config["num_fewshot"] == 10
    assert config["custom_param"] == "value"
    # Other values should remain
    assert config["batch_size"] == 32


def test_benchmark_transformer_get_tasks(benchmark_transformer, benchmark_spec):
    """Test getting tasks from benchmark spec."""
    backend_config = {}
    tasks = benchmark_transformer.get_tasks(benchmark_spec, backend_config)

    assert tasks == ["mmlu_anatomy", "mmlu_algebra"]


def test_benchmark_transformer_get_tasks_override(
    benchmark_transformer, benchmark_spec
):
    """Test getting tasks with backend override."""
    backend_config = {"tasks": ["custom_task1", "custom_task2"]}
    tasks = benchmark_transformer.get_tasks(benchmark_spec, backend_config)

    assert tasks == ["custom_task1", "custom_task2"]


def test_benchmark_transformer_get_tasks_string(benchmark_transformer):
    """Test getting tasks when specified as string."""
    benchmark_spec = BenchmarkSpec(name="test", tasks=["task1"])
    backend_config = {"tasks": "single_task"}

    tasks = benchmark_transformer.get_tasks(benchmark_spec, backend_config)
    assert tasks == ["single_task"]


def test_benchmark_transformer_get_num_fewshot(benchmark_transformer, benchmark_spec):
    """Test getting num_fewshot parameter."""
    backend_config = {}
    num_fewshot = benchmark_transformer.get_num_fewshot(benchmark_spec, backend_config)

    assert num_fewshot == 5


def test_benchmark_transformer_get_num_fewshot_default(benchmark_transformer):
    """Test getting num_fewshot with default."""
    benchmark_spec = BenchmarkSpec(name="test", tasks=["task1"])
    backend_config = {}

    num_fewshot = benchmark_transformer.get_num_fewshot(
        benchmark_spec, backend_config, default=3
    )
    assert num_fewshot == 3


def test_benchmark_transformer_get_batch_size(benchmark_transformer, benchmark_spec):
    """Test getting batch_size parameter."""
    backend_config = {}
    batch_size = benchmark_transformer.get_batch_size(benchmark_spec, backend_config)

    assert batch_size == 32


def test_benchmark_transformer_get_limit(benchmark_transformer, benchmark_spec):
    """Test getting limit parameter."""
    backend_config = {}
    limit = benchmark_transformer.get_limit(benchmark_spec, backend_config)

    assert limit == 100


# MetricExtractor Tests


@pytest.fixture
def metric_extractor():
    """Fixture for MetricExtractor."""
    return MetricExtractor()


def test_metric_extractor_flat_metrics(metric_extractor):
    """Test extracting flat metrics."""
    raw_metrics = {
        "accuracy": 0.85,
        "f1_score": 0.82,
        "precision": 0.88,
    }

    metrics = metric_extractor.extract(raw_metrics, "test", "flat")

    assert metrics == {
        "accuracy": 0.85,
        "f1_score": 0.82,
        "precision": 0.88,
    }


def test_metric_extractor_hierarchical_metrics(metric_extractor):
    """Test extracting hierarchical metrics."""
    raw_metrics = {
        "mmlu": {
            "accuracy": 0.85,
            "f1": 0.82,
        },
        "hellaswag": {
            "accuracy": 0.78,
        },
    }

    metrics = metric_extractor.extract(raw_metrics, "test", "hierarchical")

    assert metrics == {
        "mmlu.accuracy": 0.85,
        "mmlu.f1": 0.82,
        "hellaswag.accuracy": 0.78,
    }


def test_metric_extractor_nested_strategy(metric_extractor):
    """Test extracting with nested strategy."""
    raw_metrics = {
        "task1": {"accuracy": 0.85},
        "task2": {"accuracy": 0.78},
    }

    metrics = metric_extractor.extract(raw_metrics, "test", "nested")

    # Nested strategy uses underscore separator
    assert "task1_accuracy" in metrics
    assert "task2_accuracy" in metrics


def test_metric_extractor_type_conversion(metric_extractor):
    """Test metric type conversion."""
    raw_metrics = {
        "int_value": 42,
        "float_value": 0.85,
        "bool_value": True,
        "string_number": "0.95",
        "string_text": "not_a_number",
    }

    metrics = metric_extractor.extract(raw_metrics, "test", "flat")

    assert metrics["int_value"] == 42.0
    assert metrics["float_value"] == 0.85
    assert metrics["bool_value"] == 1.0
    assert metrics["string_number"] == 0.95
    # Non-numeric strings should be skipped
    assert "string_text" not in metrics


def test_metric_extractor_deeply_nested(metric_extractor):
    """Test extracting deeply nested metrics."""
    raw_metrics = {
        "level1": {
            "level2": {
                "level3": {
                    "accuracy": 0.85,
                }
            }
        }
    }

    metrics = metric_extractor.extract(raw_metrics, "test", "hierarchical")
    assert metrics["level1.level2.level3.accuracy"] == 0.85


def test_metric_extractor_from_file(metric_extractor):
    """Test extracting metrics from JSON file."""
    raw_metrics = {
        "accuracy": 0.85,
        "f1_score": 0.82,
    }

    # Create temporary JSON file
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        import json

        json.dump(raw_metrics, f)
        temp_path = f.name

    try:
        metrics = metric_extractor.extract_from_file(temp_path, "test", "flat")
        assert metrics == {"accuracy": 0.85, "f1_score": 0.82}
    finally:
        Path(temp_path).unlink()


def test_metric_extractor_filter_include(metric_extractor):
    """Test filtering metrics with include patterns."""
    metrics = {
        "mmlu.accuracy": 0.85,
        "mmlu.f1": 0.82,
        "hellaswag.accuracy": 0.78,
    }

    filtered = metric_extractor.filter_metrics(metrics, include_patterns=["mmlu.*"])

    assert "mmlu.accuracy" in filtered
    assert "mmlu.f1" in filtered
    assert "hellaswag.accuracy" not in filtered


def test_metric_extractor_filter_exclude(metric_extractor):
    """Test filtering metrics with exclude patterns."""
    metrics = {
        "mmlu.accuracy": 0.85,
        "mmlu.f1": 0.82,
        "hellaswag.accuracy": 0.78,
    }

    filtered = metric_extractor.filter_metrics(metrics, exclude_patterns=["*.f1"])

    assert "mmlu.accuracy" in filtered
    assert "mmlu.f1" not in filtered
    assert "hellaswag.accuracy" in filtered


def test_metric_extractor_aggregate_mean(metric_extractor):
    """Test aggregating metrics with mean."""
    metrics = {"metric1": 0.8, "metric2": 0.9, "metric3": 0.7}

    avg = metric_extractor.aggregate_metrics(metrics, "mean")
    assert abs(avg - 0.8) < 1e-10  # (0.8 + 0.9 + 0.7) / 3


def test_metric_extractor_aggregate_median(metric_extractor):
    """Test aggregating metrics with median."""
    metrics = {"metric1": 0.7, "metric2": 0.8, "metric3": 0.9}

    median = metric_extractor.aggregate_metrics(metrics, "median")
    assert median == 0.8


def test_metric_extractor_aggregate_min(metric_extractor):
    """Test aggregating metrics with min."""
    metrics = {"metric1": 0.7, "metric2": 0.9, "metric3": 0.8}

    min_val = metric_extractor.aggregate_metrics(metrics, "min")
    assert min_val == 0.7


def test_metric_extractor_aggregate_max(metric_extractor):
    """Test aggregating metrics with max."""
    metrics = {"metric1": 0.7, "metric2": 0.9, "metric3": 0.8}

    max_val = metric_extractor.aggregate_metrics(metrics, "max")
    assert max_val == 0.9


def test_metric_extractor_aggregate_empty(metric_extractor):
    """Test aggregating empty metrics."""
    metrics = {}
    avg = metric_extractor.aggregate_metrics(metrics, "mean")
    assert avg == 0.0


def test_metric_extractor_aggregate_invalid(metric_extractor):
    """Test aggregating with invalid strategy."""
    metrics = {"metric1": 0.8}

    with pytest.raises(ValueError, match="Unknown aggregation strategy"):
        metric_extractor.aggregate_metrics(metrics, "invalid")
