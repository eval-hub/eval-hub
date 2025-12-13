"""Unit tests for data models."""

from datetime import datetime
from uuid import uuid4

import pytest

from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkConfig,
    BenchmarkSpec,
    EvaluationJobBenchmarkConfig,
    EvaluationJobRequest,
    EvaluationRequest,
    EvaluationResult,
    EvaluationSpec,
    EvaluationStatus,
    ExperimentConfig,
    Model,
    RiskCategory,
)


class TestEvaluationModels:
    """Test evaluation data models."""

    def test_benchmark_spec_creation(self):
        """Test BenchmarkSpec model creation."""
        benchmark = BenchmarkSpec(
            name="hellaswag",
            tasks=["hellaswag"],
            num_fewshot=5,
            batch_size=8,
            limit=1000,
            device="cuda",
            config={"custom_param": "value"},
        )

        assert benchmark.name == "hellaswag"
        assert benchmark.tasks == ["hellaswag"]
        assert benchmark.num_fewshot == 5
        assert benchmark.batch_size == 8
        assert benchmark.limit == 1000
        assert benchmark.device == "cuda"
        assert benchmark.config == {"custom_param": "value"}

    def test_backend_spec_creation(self):
        """Test BackendSpec model creation."""
        benchmark = BenchmarkSpec(name="arc_easy", tasks=["arc_easy"], num_fewshot=5)

        backend = BackendSpec(
            name="lm-evaluation-harness",
            type=BackendType.LMEVAL,
            endpoint="http://localhost:8080",
            config={"timeout": 3600},
            benchmarks=[benchmark],
        )

        assert backend.name == "lm-evaluation-harness"
        assert backend.type == BackendType.LMEVAL
        assert backend.endpoint == "http://localhost:8080"
        assert backend.config == {"timeout": 3600}
        assert len(backend.benchmarks) == 1
        assert backend.benchmarks[0].name == "arc_easy"

    def test_evaluation_spec_creation(self):
        """Test EvaluationSpec model creation."""
        benchmark = BenchmarkSpec(name="test", tasks=["test"])
        backend = BackendSpec(
            name="test-backend", type=BackendType.CUSTOM, benchmarks=[benchmark]
        )

        model = Model(url="http://test-server:8000", name="test-model")
        eval_spec = EvaluationSpec(
            name="Test Evaluation",
            model=model,
            backends=[backend],
            risk_category=RiskCategory.MEDIUM,
            priority=1,
            timeout_minutes=30,
            retry_attempts=2,
        )

        assert eval_spec.name == "Test Evaluation"
        assert eval_spec.model_name == "test-model"
        assert eval_spec.model_url == "http://test-server:8000"
        assert len(eval_spec.backends) == 1
        assert eval_spec.risk_category == RiskCategory.MEDIUM
        assert eval_spec.priority == 1
        assert eval_spec.timeout_minutes == 30
        assert eval_spec.retry_attempts == 2
        assert isinstance(eval_spec.id, type(uuid4()))

    def test_evaluation_request_creation(self):
        """Test EvaluationRequest model creation."""
        benchmark = BenchmarkSpec(name="test", tasks=["test"])
        backend = BackendSpec(
            name="test-backend", type=BackendType.CUSTOM, benchmarks=[benchmark]
        )
        model = Model(url="http://test-server:8000", name="test-model")
        eval_spec = EvaluationSpec(
            name="Test Evaluation",
            model=model,
            backends=[backend],
        )

        request = EvaluationRequest(
            evaluations=[eval_spec],
            experiment=ExperimentConfig(
                name="test-experiment", tags={"team": "ai", "project": "eval"}
            ),
            async_mode=True,
        )

        assert len(request.evaluations) == 1
        assert request.experiment.name == "test-experiment"
        assert request.experiment.tags == {"team": "ai", "project": "eval"}
        assert request.async_mode is True
        assert isinstance(request.request_id, type(uuid4()))
        assert isinstance(request.created_at, datetime)

    def test_evaluation_result_creation(self):
        """Test EvaluationResult model creation."""
        eval_id = uuid4()

        result = EvaluationResult(
            evaluation_id=eval_id,
            provider_id="lm_evaluation_harness",
            benchmark_id="test-benchmark",
            benchmark_name="Test Benchmark",
            status=EvaluationStatus.COMPLETED,
            metrics={"accuracy": 0.85, "f1_score": 0.78},
            artifacts={"results": "/path/to/results.json"},
            started_at=datetime.utcnow(),
            completed_at=datetime.utcnow(),
            duration_seconds=120.5,
            mlflow_run_id="test-run-123",
        )

        assert result.evaluation_id == eval_id
        assert result.provider_id == "lm_evaluation_harness"
        assert result.benchmark_id == "test-benchmark"
        assert result.benchmark_name == "Test Benchmark"
        assert result.status == EvaluationStatus.COMPLETED
        assert result.metrics["accuracy"] == 0.85
        assert result.metrics["f1_score"] == 0.78
        assert result.artifacts["results"] == "/path/to/results.json"
        assert result.duration_seconds == 120.5
        assert result.mlflow_run_id == "test-run-123"

    def test_evaluation_result_serialization_excludes_id(self):
        """EvaluationResult should hide evaluation_id in serialized output."""
        eval_id = uuid4()

        result = EvaluationResult(
            evaluation_id=eval_id,
            provider_id="lm_evaluation_harness",
            benchmark_id="test-benchmark",
            status=EvaluationStatus.PENDING,
        )

        serialized = result.model_dump()

        assert "evaluation_id" not in serialized
        assert result.evaluation_id == eval_id

    def test_risk_category_values(self):
        """Test RiskCategory enum values."""
        assert RiskCategory.LOW.value == "low"
        assert RiskCategory.MEDIUM.value == "medium"
        assert RiskCategory.HIGH.value == "high"
        assert RiskCategory.CRITICAL.value == "critical"

    def test_backend_type_values(self):
        """Test BackendType enum values."""
        assert BackendType.LMEVAL.value == "lm-evaluation-harness"
        assert BackendType.GUIDELLM.value == "guidellm"
        assert BackendType.CUSTOM.value == "custom"

    def test_evaluation_status_values(self):
        """Test EvaluationStatus enum values."""
        assert EvaluationStatus.PENDING.value == "pending"
        assert EvaluationStatus.RUNNING.value == "running"
        assert EvaluationStatus.COMPLETED.value == "completed"
        assert EvaluationStatus.FAILED.value == "failed"
        assert EvaluationStatus.CANCELLED.value == "cancelled"

    def test_model_validation(self):
        """Test model validation."""
        # Test required fields
        with pytest.raises(ValueError):
            BenchmarkSpec(tasks=["test"])  # Missing name

        with pytest.raises(ValueError):
            BenchmarkSpec(name="test")  # Missing tasks

        # Test empty tasks list is allowed
        benchmark = BenchmarkSpec(name="test", tasks=[])
        assert benchmark.name == "test"
        assert benchmark.tasks == []

        # Test negative values are allowed (no validation currently)
        benchmark_negative = BenchmarkSpec(name="test", tasks=["test"], num_fewshot=-1)
        assert benchmark_negative.num_fewshot == -1

        benchmark_zero = BenchmarkSpec(name="test", tasks=["test"], batch_size=0)
        assert benchmark_zero.batch_size == 0

    def test_model_defaults(self):
        """Test model default values."""
        benchmark = BenchmarkSpec(name="test", tasks=["test"])
        assert benchmark.num_fewshot is None
        assert benchmark.batch_size is None
        assert benchmark.limit is None
        assert benchmark.device is None
        assert benchmark.config == {}

        model = Model(url="http://test-server:8000", name="test-model")
        eval_spec = EvaluationSpec(
            name="Test",
            model=model,
            backends=[],
        )
        assert eval_spec.risk_category is None
        assert eval_spec.priority == 0
        assert eval_spec.timeout_minutes == 60
        assert eval_spec.retry_attempts == 3
        assert eval_spec.metadata == {}

    def test_model_extra_fields(self):
        """Test that models allow extra fields."""
        benchmark = BenchmarkSpec(
            name="test",
            tasks=["test"],
            extra_field="extra_value",  # This should be allowed
        )
        assert hasattr(benchmark, "extra_field")
        assert benchmark.extra_field == "extra_value"

    def test_model_creation(self):
        """Test Model model creation."""
        model = Model(
            url="http://test-server:8000",
            name="test-model",
        )

        assert model.url == "http://test-server:8000"
        assert model.name == "test-model"

    def test_model_model_defaults(self):
        """Test Model model default values."""
        model = Model(url="http://test-server:8000", name="test-model")
        assert model.url == "http://test-server:8000"
        assert model.name == "test-model"

    def test_benchmark_config_creation(self):
        """Test BenchmarkConfig model creation."""
        config = BenchmarkConfig(
            benchmark_id="mmlu",
            provider_id="lm_evaluation_harness",
            config={
                "num_fewshot": 5,
                "limit": None,
                "batch_size": 16,
                "include_path": "./custom_prompts/mmlu_cot.yaml",
                "fewshot_as_multiturn": False,
                "trust_remote_code": False,
            },
        )

        assert config.benchmark_id == "mmlu"
        assert config.provider_id == "lm_evaluation_harness"
        assert config.config["num_fewshot"] == 5
        assert config.config["limit"] is None
        assert config.config["batch_size"] == 16
        assert config.config["include_path"] == "./custom_prompts/mmlu_cot.yaml"
        assert config.config["fewshot_as_multiturn"] is False
        assert config.config["trust_remote_code"] is False

    def test_benchmark_config_minimal(self):
        """Test BenchmarkConfig with minimal required fields."""
        config = BenchmarkConfig(
            benchmark_id="arc_easy", provider_id="lm_evaluation_harness"
        )

        assert config.benchmark_id == "arc_easy"
        assert config.provider_id == "lm_evaluation_harness"
        assert config.config == {}

    def test_evaluation_job_request_creation(self):
        """Test EvaluationJobRequest model creation."""
        model = Model(
            url="http://test-server:8000",
            name="meta-llama/llama-3.1-8b",
        )

        benchmarks = [
            EvaluationJobBenchmarkConfig(
                name="ARC Easy Benchmark",
                id="arc_easy",
                provider_id="lm_evaluation_harness",
                config={
                    "num_fewshot": 0,
                    "limit": 1000,
                    "batch_size": 16,
                    "include_path": "./custom_prompts/arc_easy.yaml",
                },
            ),
            EvaluationJobBenchmarkConfig(
                name="MMLU Benchmark",
                id="mmlu",
                provider_id="lm_evaluation_harness",
                config={
                    "num_fewshot": 5,
                    "limit": None,
                    "batch_size": 16,
                    "include_path": "./custom_prompts/mmlu_cot.yaml",
                    "fewshot_as_multiturn": False,
                    "trust_remote_code": False,
                },
            ),
        ]

        request = EvaluationJobRequest(
            model=model.model_dump(),
            benchmarks=[bench.model_dump() for bench in benchmarks],
            experiment={
                "name": "llama-3.1-8b-reasoning-eval",
                "tags": {
                    "environment": "production",
                    "model_family": "llama-3.1",
                    "evaluation_type": "reasoning",
                },
            },
        )

        assert request.model.url == "http://test-server:8000"
        assert request.model.name == "meta-llama/llama-3.1-8b"
        assert len(request.benchmarks) == 2
        assert request.benchmarks[0].id == "arc_easy"
        assert request.benchmarks[1].id == "mmlu"
        assert request.experiment.name == "llama-3.1-8b-reasoning-eval"
        assert request.experiment.tags["environment"] == "production"
        assert request.experiment.tags["model_family"] == "llama-3.1"
        assert request.experiment.tags["evaluation_type"] == "reasoning"

    def test_evaluation_job_request_defaults(self):
        """Test EvaluationJobRequest model default values."""
        model = Model(url="http://test-server:8000", name="test-model")
        benchmarks = [
            EvaluationJobBenchmarkConfig(
                name="Test Benchmark", id="test", provider_id="test_provider"
            )
        ]

        request = EvaluationJobRequest(
            model=model.model_dump(),
            benchmarks=[bench.model_dump() for bench in benchmarks],
            experiment={"name": "test-experiment"},
        )

        assert request.experiment.name == "test-experiment"
        assert getattr(request.experiment, "tags", {}) == {}
        assert request.timeout_minutes == 60
        assert request.retry_attempts == 3
        assert request.callback_url is None
