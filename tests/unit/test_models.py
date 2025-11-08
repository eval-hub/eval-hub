"""Unit tests for data models."""

import pytest
from datetime import datetime
from uuid import uuid4

from eval_hub.models.evaluation import (
    EvaluationRequest,
    EvaluationSpec,
    BackendSpec,
    BenchmarkSpec,
    RiskCategory,
    BackendType,
    EvaluationResult,
    EvaluationStatus,
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
            config={"custom_param": "value"}
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
        benchmark = BenchmarkSpec(
            name="arc_easy",
            tasks=["arc_easy"],
            num_fewshot=5
        )

        backend = BackendSpec(
            name="lm-evaluation-harness",
            type=BackendType.LMEVAL,
            endpoint="http://localhost:8080",
            config={"timeout": 3600},
            benchmarks=[benchmark]
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
            name="test-backend",
            type=BackendType.CUSTOM,
            benchmarks=[benchmark]
        )

        eval_spec = EvaluationSpec(
            name="Test Evaluation",
            model_name="test-model",
            backends=[backend],
            risk_category=RiskCategory.MEDIUM,
            priority=1,
            timeout_minutes=30,
            retry_attempts=2
        )

        assert eval_spec.name == "Test Evaluation"
        assert eval_spec.model_name == "test-model"
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
            name="test-backend",
            type=BackendType.CUSTOM,
            benchmarks=[benchmark]
        )
        eval_spec = EvaluationSpec(
            name="Test Evaluation",
            model_name="test-model",
            backends=[backend]
        )

        request = EvaluationRequest(
            evaluations=[eval_spec],
            experiment_name="test-experiment",
            tags={"team": "ai", "project": "eval"},
            async_mode=True
        )

        assert len(request.evaluations) == 1
        assert request.experiment_name == "test-experiment"
        assert request.tags == {"team": "ai", "project": "eval"}
        assert request.async_mode is True
        assert isinstance(request.request_id, type(uuid4()))
        assert isinstance(request.created_at, datetime)

    def test_evaluation_result_creation(self):
        """Test EvaluationResult model creation."""
        eval_id = uuid4()

        result = EvaluationResult(
            evaluation_id=eval_id,
            backend_name="test-backend",
            benchmark_name="test-benchmark",
            status=EvaluationStatus.COMPLETED,
            metrics={"accuracy": 0.85, "f1_score": 0.78},
            artifacts={"results": "/path/to/results.json"},
            started_at=datetime.utcnow(),
            completed_at=datetime.utcnow(),
            duration_seconds=120.5,
            mlflow_run_id="test-run-123"
        )

        assert result.evaluation_id == eval_id
        assert result.backend_name == "test-backend"
        assert result.benchmark_name == "test-benchmark"
        assert result.status == EvaluationStatus.COMPLETED
        assert result.metrics["accuracy"] == 0.85
        assert result.metrics["f1_score"] == 0.78
        assert result.artifacts["results"] == "/path/to/results.json"
        assert result.duration_seconds == 120.5
        assert result.mlflow_run_id == "test-run-123"

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

        eval_spec = EvaluationSpec(
            name="Test",
            model_name="test-model",
            backends=[]
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
            extra_field="extra_value"  # This should be allowed
        )
        assert hasattr(benchmark, 'extra_field')
        assert benchmark.extra_field == "extra_value"


class TestModelDataStructures:
    """Test model data structures."""

    def test_model_type_values(self):
        """Test ModelType enum values."""
        from eval_hub.models.model import ModelType

        assert ModelType.OPENAI.value == "openai"
        assert ModelType.ANTHROPIC.value == "anthropic"
        assert ModelType.HUGGINGFACE.value == "huggingface"
        assert ModelType.OLLAMA.value == "ollama"
        assert ModelType.VLLM.value == "vllm"
        assert ModelType.OPENAI_COMPATIBLE.value == "openai-compatible"
        assert ModelType.CUSTOM.value == "custom"

    def test_model_status_values(self):
        """Test ModelStatus enum values."""
        from eval_hub.models.model import ModelStatus

        assert ModelStatus.ACTIVE.value == "active"
        assert ModelStatus.INACTIVE.value == "inactive"
        assert ModelStatus.TESTING.value == "testing"
        assert ModelStatus.DEPRECATED.value == "deprecated"

    def test_model_capabilities_creation(self):
        """Test ModelCapabilities model creation."""
        from eval_hub.models.model import ModelCapabilities

        capabilities = ModelCapabilities(
            max_tokens=8192,
            supports_streaming=True,
            supports_function_calling=False,
            supports_vision=True,
            context_window=4096
        )

        assert capabilities.max_tokens == 8192
        assert capabilities.supports_streaming is True
        assert capabilities.supports_function_calling is False
        assert capabilities.supports_vision is True
        assert capabilities.context_window == 4096

    def test_model_capabilities_defaults(self):
        """Test ModelCapabilities default values."""
        from eval_hub.models.model import ModelCapabilities

        capabilities = ModelCapabilities()

        assert capabilities.max_tokens is None
        assert capabilities.supports_streaming is False
        assert capabilities.supports_function_calling is False
        assert capabilities.supports_vision is False
        assert capabilities.context_window is None

    def test_model_config_creation(self):
        """Test ModelConfig model creation."""
        from eval_hub.models.model import ModelConfig

        config = ModelConfig(
            temperature=0.7,
            max_tokens=1000,
            top_p=0.9,
            frequency_penalty=0.1,
            presence_penalty=-0.1,
            timeout=60,
            retry_attempts=5
        )

        assert config.temperature == 0.7
        assert config.max_tokens == 1000
        assert config.top_p == 0.9
        assert config.frequency_penalty == 0.1
        assert config.presence_penalty == -0.1
        assert config.timeout == 60
        assert config.retry_attempts == 5

    def test_model_config_defaults(self):
        """Test ModelConfig default values."""
        from eval_hub.models.model import ModelConfig

        config = ModelConfig()

        assert config.temperature is None
        assert config.max_tokens is None
        assert config.top_p is None
        assert config.frequency_penalty is None
        assert config.presence_penalty is None
        assert config.timeout == 30
        assert config.retry_attempts == 3

    def test_model_creation(self):
        """Test Model creation with all fields."""
        from eval_hub.models.model import Model, ModelType, ModelStatus, ModelCapabilities, ModelConfig
        from datetime import datetime

        capabilities = ModelCapabilities(max_tokens=4096, supports_streaming=True)
        config = ModelConfig(temperature=0.5, max_tokens=2000)

        model = Model(
            model_id="test-gpt-4",
            model_name="Test GPT-4",
            description="A test GPT-4 model",
            model_type=ModelType.OPENAI,
            base_url="https://api.openai.com/v1",
            api_key_required=True,
            model_path="gpt-4",
            capabilities=capabilities,
            config=config,
            status=ModelStatus.ACTIVE,
            tags=["test", "gpt"]
        )

        assert model.model_id == "test-gpt-4"
        assert model.model_name == "Test GPT-4"
        assert model.description == "A test GPT-4 model"
        assert model.model_type == ModelType.OPENAI
        assert model.base_url == "https://api.openai.com/v1"
        assert model.api_key_required is True
        assert model.model_path == "gpt-4"
        assert model.capabilities.max_tokens == 4096
        assert model.config.temperature == 0.5
        assert model.status == ModelStatus.ACTIVE
        assert model.tags == ["test", "gpt"]
        assert isinstance(model.created_at, datetime)
        assert isinstance(model.updated_at, datetime)

    def test_model_validation_model_id(self):
        """Test Model validation for model_id."""
        from eval_hub.models.model import Model, ModelType
        import pytest

        # Test empty model_id
        with pytest.raises(ValueError, match="model_id cannot be empty"):
            Model(
                model_id="",
                model_name="Test",
                description="Test",
                model_type=ModelType.OPENAI,
                base_url="https://api.openai.com/v1"
            )

        # Test invalid characters in model_id
        with pytest.raises(ValueError, match="model_id can only contain"):
            Model(
                model_id="test@model!",
                model_name="Test",
                description="Test",
                model_type=ModelType.OPENAI,
                base_url="https://api.openai.com/v1"
            )

    def test_model_validation_base_url(self):
        """Test Model validation for base_url."""
        from eval_hub.models.model import Model, ModelType
        import pytest

        # Test invalid base_url
        with pytest.raises(ValueError, match="base_url must be a valid HTTP or HTTPS URL"):
            Model(
                model_id="test-model",
                model_name="Test",
                description="Test",
                model_type=ModelType.OPENAI,
                base_url="invalid-url"
            )

    def test_model_summary_creation(self):
        """Test ModelSummary creation."""
        from eval_hub.models.model import ModelSummary, ModelType, ModelStatus
        from datetime import datetime

        summary = ModelSummary(
            model_id="test-model",
            model_name="Test Model",
            description="A test model",
            model_type=ModelType.ANTHROPIC,
            base_url="https://api.anthropic.com",
            status=ModelStatus.ACTIVE,
            tags=["test"],
            created_at=datetime.utcnow()
        )

        assert summary.model_id == "test-model"
        assert summary.model_name == "Test Model"
        assert summary.model_type == ModelType.ANTHROPIC
        assert summary.status == ModelStatus.ACTIVE

    def test_model_registration_request(self):
        """Test ModelRegistrationRequest creation."""
        from eval_hub.models.model import ModelRegistrationRequest, ModelType, ModelCapabilities

        capabilities = ModelCapabilities(supports_streaming=True)

        request = ModelRegistrationRequest(
            model_id="new-model",
            model_name="New Model",
            description="A new model to register",
            model_type=ModelType.HUGGINGFACE,
            base_url="https://huggingface.co/models/test",
            capabilities=capabilities,
            tags=["new", "test"]
        )

        assert request.model_id == "new-model"
        assert request.model_name == "New Model"
        assert request.model_type == ModelType.HUGGINGFACE
        assert request.capabilities.supports_streaming is True
        assert request.tags == ["new", "test"]

    def test_model_update_request(self):
        """Test ModelUpdateRequest with optional fields."""
        from eval_hub.models.model import ModelUpdateRequest, ModelStatus

        request = ModelUpdateRequest(
            model_name="Updated Model Name",
            status=ModelStatus.INACTIVE
        )

        assert request.model_name == "Updated Model Name"
        assert request.status == ModelStatus.INACTIVE
        assert request.description is None  # Optional field not set

    def test_list_models_response(self):
        """Test ListModelsResponse creation."""
        from eval_hub.models.model import ListModelsResponse, ModelSummary, ModelType, ModelStatus
        from datetime import datetime

        summary1 = ModelSummary(
            model_id="model1",
            model_name="Model 1",
            description="First model",
            model_type=ModelType.OPENAI,
            base_url="https://api.openai.com/v1",
            status=ModelStatus.ACTIVE,
            created_at=datetime.utcnow()
        )

        summary2 = ModelSummary(
            model_id="model2",
            model_name="Model 2",
            description="Second model",
            model_type=ModelType.ANTHROPIC,
            base_url="https://api.anthropic.com",
            status=ModelStatus.ACTIVE,
            created_at=datetime.utcnow()
        )

        response = ListModelsResponse(
            models=[summary1, summary2],
            total_models=2,
            runtime_models=[summary2]
        )

        assert len(response.models) == 2
        assert response.total_models == 2
        assert len(response.runtime_models) == 1
        assert response.runtime_models[0].model_id == "model2"

    def test_runtime_model_config(self):
        """Test RuntimeModelConfig creation."""
        from eval_hub.models.model import RuntimeModelConfig, ModelType

        runtime_config = RuntimeModelConfig(
            model_id="runtime-model",
            model_name="Runtime Model",
            base_url="https://localhost:8080",
            model_type=ModelType.VLLM,
            model_path="/models/test"
        )

        assert runtime_config.model_id == "runtime-model"
        assert runtime_config.model_type == ModelType.VLLM
        assert runtime_config.base_url == "https://localhost:8080"
        assert runtime_config.model_path == "/models/test"

    def test_model_config_validation_ranges(self):
        """Test ModelConfig validation for parameter ranges."""
        from eval_hub.models.model import ModelConfig
        import pytest

        # Test valid values
        config = ModelConfig(
            temperature=0.5,
            top_p=0.9,
            frequency_penalty=0.0,
            presence_penalty=-1.0
        )
        assert config.temperature == 0.5

        # Test temperature out of range
        with pytest.raises(ValueError):
            ModelConfig(temperature=3.0)  # Above 2.0

        # Test top_p out of range
        with pytest.raises(ValueError):
            ModelConfig(top_p=1.5)  # Above 1.0

        # Test frequency_penalty out of range
        with pytest.raises(ValueError):
            ModelConfig(frequency_penalty=-3.0)  # Below -2.0

        # Test presence_penalty out of range
        with pytest.raises(ValueError):
            ModelConfig(presence_penalty=3.0)  # Above 2.0