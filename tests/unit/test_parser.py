"""Unit tests for request parser service."""

import pytest

from eval_hub.core.config import Settings
from eval_hub.core.exceptions import ValidationError
from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationRequest,
    EvaluationSpec,
    ExperimentConfig,
    RiskCategory,
)
from eval_hub.services.parser import RequestParser


@pytest.fixture
def settings():
    """Create test settings."""
    return Settings(
        backend_configs={
            "lm-evaluation-harness": {
                "image": "eval-harness:latest",
                "resources": {"cpu": "2", "memory": "4Gi"},
                "timeout": 3600,
            },
            "guidellm": {
                "image": "guidellm:latest",
                "resources": {"cpu": "1", "memory": "2Gi"},
                "timeout": 1800,
            },
        },
        risk_category_benchmarks={
            "low": {
                "benchmarks": ["hellaswag", "arc_easy"],
                "num_fewshot": 5,
                "limit": 100,
            },
            "medium": {
                "benchmarks": ["hellaswag", "arc_easy", "arc_challenge", "winogrande"],
                "num_fewshot": 5,
                "limit": 500,
            },
            "high": {
                "benchmarks": [
                    "hellaswag",
                    "arc_easy",
                    "arc_challenge",
                    "winogrande",
                    "mmlu",
                ],
                "num_fewshot": 5,
                "limit": 1000,
            },
            "critical": {
                "benchmarks": [
                    "hellaswag",
                    "arc_easy",
                    "arc_challenge",
                    "winogrande",
                    "mmlu",
                    "gsm8k",
                ],
                "num_fewshot": 5,
                "limit": None,
            },
        },
    )


@pytest.fixture
def parser(settings):
    """Create request parser instance."""
    return RequestParser(settings)


class TestRequestParser:
    """Test request parser service."""

    @pytest.mark.asyncio
    async def test_parse_valid_request_with_backends(self, parser):
        """Test parsing a valid request with explicit backends."""
        benchmark = BenchmarkSpec(
            name="hellaswag", tasks=["hellaswag"], num_fewshot=5, limit=1000
        )
        backend = BackendSpec(
            name="lm-evaluation-harness",
            type=BackendType.LMEVAL,
            benchmarks=[benchmark],
        )
        eval_spec = EvaluationSpec(
            name="Test Evaluation",
            model={"url": "http://test-server:8000", "name": "test-model"},
            backends=[backend],
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        parsed_request = await parser.parse_evaluation_request(request)

        assert len(parsed_request.evaluations) == 1
        assert parsed_request.evaluations[0].name == "Test Evaluation"
        assert parsed_request.evaluations[0].model_name == "test-model"
        assert len(parsed_request.evaluations[0].backends) == 1
        assert parsed_request.evaluations[0].backends[0].name == "lm-evaluation-harness"

    @pytest.mark.asyncio
    async def test_parse_request_with_risk_category(self, parser):
        """Test parsing a request with risk category."""
        eval_spec = EvaluationSpec(
            name="Risk Category Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            risk_category=RiskCategory.MEDIUM,
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        parsed_request = await parser.parse_evaluation_request(request)

        assert len(parsed_request.evaluations) == 1
        evaluation = parsed_request.evaluations[0]

        # Should have generated backends based on risk category
        assert len(evaluation.backends) > 0

        # Check that backends were generated
        backend_names = [b.name for b in evaluation.backends]
        assert "lm-evaluation-harness" in backend_names
        assert "guidellm" in backend_names

        # Check benchmarks for medium risk category
        for backend in evaluation.backends:
            benchmark_names = [b.name for b in backend.benchmarks]
            expected_benchmarks = [
                "hellaswag",
                "arc_easy",
                "arc_challenge",
                "winogrande",
            ]
            for expected in expected_benchmarks:
                assert expected in benchmark_names

    @pytest.mark.asyncio
    async def test_parse_request_with_low_risk_category(self, parser):
        """Test parsing a request with low risk category."""
        eval_spec = EvaluationSpec(
            name="Low Risk Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            risk_category=RiskCategory.LOW,
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        parsed_request = await parser.parse_evaluation_request(request)

        evaluation = parsed_request.evaluations[0]

        # Check benchmarks for low risk category
        for backend in evaluation.backends:
            benchmark_names = [b.name for b in backend.benchmarks]
            expected_benchmarks = ["hellaswag", "arc_easy"]
            assert len(benchmark_names) == len(expected_benchmarks)
            for expected in expected_benchmarks:
                assert expected in benchmark_names

    @pytest.mark.asyncio
    async def test_parse_request_with_critical_risk_category(self, parser):
        """Test parsing a request with critical risk category."""
        eval_spec = EvaluationSpec(
            name="Critical Risk Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            risk_category=RiskCategory.CRITICAL,
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        parsed_request = await parser.parse_evaluation_request(request)

        evaluation = parsed_request.evaluations[0]

        # Check benchmarks for critical risk category
        for backend in evaluation.backends:
            benchmark_names = [b.name for b in backend.benchmarks]
            expected_benchmarks = [
                "hellaswag",
                "arc_easy",
                "arc_challenge",
                "winogrande",
                "mmlu",
                "gsm8k",
            ]
            assert len(benchmark_names) == len(expected_benchmarks)
            for expected in expected_benchmarks:
                assert expected in benchmark_names

    @pytest.mark.asyncio
    async def test_validation_empty_evaluations(self, parser):
        """Test validation fails for empty evaluations list."""
        request = EvaluationRequest(
            evaluations=[], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "must contain at least one evaluation" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_too_many_evaluations(self, parser):
        """Test validation fails for too many evaluations."""
        evaluations = []
        for i in range(101):  # More than the limit of 100
            eval_spec = EvaluationSpec(
                name=f"Test {i}",
                model={"url": "http://test-server:8000", "name": "test-model"},
                risk_category=RiskCategory.LOW,
            )
            evaluations.append(eval_spec)

        request = EvaluationRequest(
            evaluations=evaluations, experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "cannot contain more than 100 evaluations" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_missing_model_name(self, parser):
        """Test validation fails for missing model name."""
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": ""},  # Empty model name
            risk_category=RiskCategory.LOW,
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "model_name is required" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_no_backends_or_risk_category(self, parser):
        """Test validation fails when neither backends nor risk category specified."""
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            # No backends or risk_category
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "must specify one of" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_negative_timeout(self, parser):
        """Test validation fails for negative timeout."""
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            risk_category=RiskCategory.LOW,
            timeout_minutes=-1,
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "timeout_minutes must be positive" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_negative_retry_attempts(self, parser):
        """Test validation fails for negative retry attempts."""
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            risk_category=RiskCategory.LOW,
            retry_attempts=-1,
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "retry_attempts cannot be negative" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_empty_backend_name(self, parser):
        """Test validation fails for empty backend name."""
        benchmark = BenchmarkSpec(name="test", tasks=["test"])
        backend = BackendSpec(
            name="",
            type=BackendType.CUSTOM,
            benchmarks=[benchmark],  # Empty name
        )
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            backends=[backend],
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "name is required" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_validation_empty_benchmarks(self, parser):
        """Test validation fails for empty benchmarks list."""
        backend = BackendSpec(
            name="test-backend",
            type=BackendType.CUSTOM,
            benchmarks=[],  # Empty benchmarks
        )
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            backends=[backend],
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        with pytest.raises(ValidationError) as exc_info:
            await parser.parse_evaluation_request(request)

        assert "must specify at least one benchmark" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_apply_backend_defaults(self, parser):
        """Test that backend defaults are applied correctly."""
        benchmark = BenchmarkSpec(name="hellaswag", tasks=["hellaswag"])
        backend = BackendSpec(
            name="lm-evaluation-harness",
            type=BackendType.LMEVAL,
            benchmarks=[benchmark],
        )
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            backends=[backend],
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        parsed_request = await parser.parse_evaluation_request(request)

        # Check that backend config was merged with defaults
        backend_config = parsed_request.evaluations[0].backends[0].config
        assert "image" in backend_config
        assert "resources" in backend_config
        assert "timeout" in backend_config

        # Check that benchmark defaults were applied
        benchmark_spec = parsed_request.evaluations[0].backends[0].benchmarks[0]
        assert benchmark_spec.batch_size == 1  # Default batch size
        assert benchmark_spec.device == "auto"  # Default device
        assert benchmark_spec.num_fewshot == 5  # Default for lm-eval

    def test_get_total_benchmark_count(self, parser):
        """Test counting total benchmarks in a request."""
        benchmark1 = BenchmarkSpec(name="test1", tasks=["test1"])
        benchmark2 = BenchmarkSpec(name="test2", tasks=["test2"])
        benchmark3 = BenchmarkSpec(name="test3", tasks=["test3"])

        backend1 = BackendSpec(
            name="backend1",
            type=BackendType.CUSTOM,
            benchmarks=[benchmark1, benchmark2],
        )
        backend2 = BackendSpec(
            name="backend2", type=BackendType.CUSTOM, benchmarks=[benchmark3]
        )

        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            backends=[backend1, backend2],
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        total_count = parser.get_total_benchmark_count(request)
        assert total_count == 3  # 2 + 1 benchmarks

    def test_estimate_completion_time(self, parser):
        """Test completion time estimation."""
        benchmark = BenchmarkSpec(name="test", tasks=["test"])
        backend = BackendSpec(
            name="test-backend", type=BackendType.CUSTOM, benchmarks=[benchmark]
        )
        eval_spec = EvaluationSpec(
            name="Test",
            model={"url": "http://test-server:8000", "name": "test-model"},
            backends=[backend],
        )
        request = EvaluationRequest(
            evaluations=[eval_spec], experiment=ExperimentConfig(name="test-evaluation")
        )

        estimated_time = parser.estimate_completion_time(request)

        # Should be at least the minimum time
        assert estimated_time >= 10
        # Should be reasonable for 1 benchmark
        assert estimated_time <= 60
