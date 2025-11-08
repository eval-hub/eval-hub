"""Unit tests for the model service."""

import os
import pytest
from unittest.mock import patch, Mock
from datetime import datetime

from eval_hub.services.model_service import ModelService
from eval_hub.models.model import (
    Model,
    ModelType,
    ModelStatus,
    ModelCapabilities,
    ModelConfig,
    ModelRegistrationRequest,
    ModelUpdateRequest,
    ListModelsResponse,
)


@pytest.fixture
def model_service():
    """Create a ModelService instance for testing."""
    from eval_hub.core.config import Settings

    settings = Settings()
    return ModelService(settings)


@pytest.fixture
def sample_model_registration():
    """Create a sample model registration request."""
    return ModelRegistrationRequest(
        model_id="test-gpt-4",
        model_name="Test GPT-4",
        description="A test GPT-4 model for unit testing",
        model_type=ModelType.OPENAI,
        base_url="https://api.openai.com/v1",
        api_key_required=True,
        model_path="gpt-4",
        capabilities=ModelCapabilities(max_tokens=8192, supports_streaming=True),
        config=ModelConfig(temperature=0.7, max_tokens=2000),
        status=ModelStatus.ACTIVE,
        tags=["test", "gpt", "openai"]
    )


class TestModelService:
    """Test ModelService functionality."""

    def test_model_service_initialization(self, model_service):
        """Test ModelService initialization."""
        assert model_service._registered_models == {}
        assert model_service._runtime_models == {}
        assert model_service._initialized is False

    def test_register_model_success(self, model_service, sample_model_registration):
        """Test successful model registration."""
        model = model_service.register_model(sample_model_registration)

        assert model.model_id == "test-gpt-4"
        assert model.model_name == "Test GPT-4"
        assert model.model_type == ModelType.OPENAI
        assert model.status == ModelStatus.ACTIVE
        assert isinstance(model.created_at, datetime)
        assert isinstance(model.updated_at, datetime)

        # Check that model is stored
        stored_model = model_service.get_model_by_id("test-gpt-4")
        assert stored_model is not None
        assert stored_model.model_id == "test-gpt-4"

    def test_register_model_duplicate_id(self, model_service, sample_model_registration):
        """Test registering model with duplicate ID."""
        # Register first model
        model_service.register_model(sample_model_registration)

        # Try to register with same ID
        with pytest.raises(ValueError, match="Model with ID 'test-gpt-4' already exists"):
            model_service.register_model(sample_model_registration)

    def test_get_model_by_id_not_found(self, model_service):
        """Test getting model by ID when model doesn't exist."""
        model = model_service.get_model_by_id("non-existent-model")
        assert model is None

    def test_update_model_success(self, model_service, sample_model_registration):
        """Test successful model update."""
        # Register model first
        model_service.register_model(sample_model_registration)

        # Update the model
        update_request = ModelUpdateRequest(
            model_name="Updated GPT-4",
            description="Updated description",
            status=ModelStatus.INACTIVE,
            tags=["updated", "test"]
        )

        updated_model = model_service.update_model("test-gpt-4", update_request)

        assert updated_model is not None
        assert updated_model.model_name == "Updated GPT-4"
        assert updated_model.description == "Updated description"
        assert updated_model.status == ModelStatus.INACTIVE
        assert updated_model.tags == ["updated", "test"]
        assert updated_model.model_type == ModelType.OPENAI  # Unchanged

    def test_update_model_not_found(self, model_service):
        """Test updating model that doesn't exist."""
        update_request = ModelUpdateRequest(model_name="Updated Name")

        updated_model = model_service.update_model("non-existent-model", update_request)
        assert updated_model is None

    def test_delete_model_success(self, model_service, sample_model_registration):
        """Test successful model deletion."""
        # Register model first
        model_service.register_model(sample_model_registration)

        # Delete the model
        success = model_service.delete_model("test-gpt-4")
        assert success is True

        # Verify model is deleted
        model = model_service.get_model_by_id("test-gpt-4")
        assert model is None

    def test_delete_model_not_found(self, model_service):
        """Test deleting model that doesn't exist."""
        success = model_service.delete_model("non-existent-model")
        assert success is False

    def test_get_all_models_empty(self, model_service):
        """Test getting all models when none are registered."""
        response = model_service.get_all_models()

        assert isinstance(response, ListModelsResponse)
        assert response.models == []
        assert response.total_models == 0
        assert response.runtime_models == []

    def test_get_all_models_with_registered(self, model_service, sample_model_registration):
        """Test getting all models with registered models."""
        # Register a model
        model_service.register_model(sample_model_registration)

        response = model_service.get_all_models()

        assert len(response.models) == 1
        assert response.total_models == 1
        assert response.models[0].model_id == "test-gpt-4"

    def test_get_all_models_exclude_inactive(self, model_service, sample_model_registration):
        """Test getting all models excluding inactive ones."""
        # Register model and make it inactive
        model_service.register_model(sample_model_registration)
        update_request = ModelUpdateRequest(status=ModelStatus.INACTIVE)
        model_service.update_model("test-gpt-4", update_request)

        # Get models excluding inactive
        response = model_service.get_all_models(include_inactive=False)
        assert response.total_models == 0

        # Get models including inactive
        response = model_service.get_all_models(include_inactive=True)
        assert response.total_models == 1

    def test_search_models_by_type(self, model_service, sample_model_registration):
        """Test searching models by type."""
        # Register models with different types
        model_service.register_model(sample_model_registration)

        anthropic_request = ModelRegistrationRequest(
            model_id="test-claude",
            model_name="Test Claude",
            description="A test Claude model",
            model_type=ModelType.ANTHROPIC,
            base_url="https://api.anthropic.com/v1"
        )
        model_service.register_model(anthropic_request)

        # Search by OpenAI type
        openai_models = model_service.search_models(model_type=ModelType.OPENAI)
        assert len(openai_models) == 1
        assert openai_models[0].model_id == "test-gpt-4"

        # Search by Anthropic type
        anthropic_models = model_service.search_models(model_type=ModelType.ANTHROPIC)
        assert len(anthropic_models) == 1
        assert anthropic_models[0].model_id == "test-claude"

    def test_search_models_by_status(self, model_service, sample_model_registration):
        """Test searching models by status."""
        # Register model and create another with different status
        model_service.register_model(sample_model_registration)

        inactive_request = ModelRegistrationRequest(
            model_id="inactive-model",
            model_name="Inactive Model",
            description="An inactive model",
            model_type=ModelType.OPENAI,
            base_url="https://api.openai.com/v1",
            status=ModelStatus.INACTIVE
        )
        model_service.register_model(inactive_request)

        # Search by active status
        active_models = model_service.search_models(status=ModelStatus.ACTIVE)
        assert len(active_models) == 1
        assert active_models[0].model_id == "test-gpt-4"

        # Search by inactive status
        inactive_models = model_service.search_models(status=ModelStatus.INACTIVE)
        assert len(inactive_models) == 1
        assert inactive_models[0].model_id == "inactive-model"

    def test_search_models_by_tags(self, model_service, sample_model_registration):
        """Test searching models by tags."""
        # Register model with tags
        model_service.register_model(sample_model_registration)

        # Register another model with different tags
        other_request = ModelRegistrationRequest(
            model_id="other-model",
            model_name="Other Model",
            description="Another model",
            model_type=ModelType.HUGGINGFACE,
            base_url="https://huggingface.co/models/test",
            tags=["huggingface", "custom"]
        )
        model_service.register_model(other_request)

        # Search by tags
        gpt_models = model_service.search_models(tags=["gpt"])
        assert len(gpt_models) == 1
        assert gpt_models[0].model_id == "test-gpt-4"

        # Search by multiple tags (any match)
        test_models = model_service.search_models(tags=["test"])
        assert len(test_models) == 1  # Only first model has "test" tag

        # Search for all models with huggingface tag
        huggingface_models = model_service.search_models(tags=["huggingface"])
        assert len(huggingface_models) == 1
        assert huggingface_models[0].model_id == "other-model"

    def test_get_active_models(self, model_service, sample_model_registration):
        """Test getting only active models."""
        # Register active model
        model_service.register_model(sample_model_registration)

        # Register inactive model
        inactive_request = ModelRegistrationRequest(
            model_id="inactive-model",
            model_name="Inactive Model",
            description="An inactive model",
            model_type=ModelType.OPENAI,
            base_url="https://api.openai.com/v1",
            status=ModelStatus.INACTIVE
        )
        model_service.register_model(inactive_request)

        active_models = model_service.get_active_models()
        assert len(active_models) == 1
        assert active_models[0].model_id == "test-gpt-4"
        assert active_models[0].status == ModelStatus.ACTIVE


class TestModelServiceRuntimeModels:
    """Test ModelService runtime model functionality."""

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_GPT4_URL": "https://api.openai.com/v1",
        "EVAL_HUB_MODEL_GPT4_NAME": "Runtime GPT-4",
        "EVAL_HUB_MODEL_GPT4_TYPE": "openai",
        "EVAL_HUB_MODEL_GPT4_PATH": "gpt-4"
    })
    def test_load_runtime_models_from_env(self, model_service):
        """Test loading runtime models from environment variables."""
        # Trigger initialization
        model_service._initialize()

        # Check runtime model was loaded
        runtime_model = model_service.get_model_by_id("gpt4")
        assert runtime_model is not None
        assert runtime_model.model_name == "Runtime GPT-4"
        assert runtime_model.model_type == ModelType.OPENAI
        assert runtime_model.base_url == "https://api.openai.com/v1"
        assert runtime_model.model_path == "gpt-4"
        assert "runtime" in runtime_model.tags

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_LOCAL_URL": "http://localhost:8080",
        # No name, type, or path - should use defaults
    })
    def test_load_runtime_models_defaults(self, model_service):
        """Test loading runtime models with default values."""
        model_service._initialize()

        runtime_model = model_service.get_model_by_id("local")
        assert runtime_model is not None
        assert runtime_model.model_name == "Runtime Model LOCAL"
        assert runtime_model.model_type == ModelType.OPENAI_COMPATIBLE  # Default
        assert runtime_model.base_url == "http://localhost:8080"
        assert runtime_model.model_path is None

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_INVALID_URL": "",  # Empty URL should be skipped
    })
    def test_load_runtime_models_empty_url(self, model_service):
        """Test that runtime models with empty URLs are skipped."""
        model_service._initialize()

        runtime_model = model_service.get_model_by_id("invalid")
        assert runtime_model is None

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_BADTYPE_URL": "http://localhost:8080",
        "EVAL_HUB_MODEL_BADTYPE_TYPE": "invalid-type"
    })
    def test_load_runtime_models_invalid_type(self, model_service):
        """Test runtime models with invalid type use default."""
        model_service._initialize()

        runtime_model = model_service.get_model_by_id("badtype")
        assert runtime_model is not None
        assert runtime_model.model_type == ModelType.OPENAI_COMPATIBLE  # Default fallback

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_RUNTIME1_URL": "http://localhost:8001"
    })
    def test_cannot_register_model_with_runtime_id(self, model_service):
        """Test that registered models cannot have same ID as runtime models."""
        # Initialize to load runtime models
        model_service._initialize()

        # Try to register model with same ID as runtime model
        request = ModelRegistrationRequest(
            model_id="runtime1",
            model_name="Conflicting Model",
            description="This should fail",
            model_type=ModelType.OPENAI,
            base_url="https://api.openai.com/v1"
        )

        with pytest.raises(ValueError, match="Model with ID 'runtime1' is specified as runtime model"):
            model_service.register_model(request)

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_RUNTIME2_URL": "http://localhost:8002"
    })
    def test_cannot_update_runtime_model(self, model_service):
        """Test that runtime models cannot be updated."""
        model_service._initialize()

        update_request = ModelUpdateRequest(model_name="Updated Runtime")

        with pytest.raises(ValueError, match="Cannot update runtime models"):
            model_service.update_model("runtime2", update_request)

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_RUNTIME3_URL": "http://localhost:8003"
    })
    def test_cannot_delete_runtime_model(self, model_service):
        """Test that runtime models cannot be deleted."""
        model_service._initialize()

        with pytest.raises(ValueError, match="Cannot delete runtime models"):
            model_service.delete_model("runtime3")

    @patch.dict(os.environ, {
        "EVAL_HUB_MODEL_COMBINED_URL": "http://localhost:8004"
    })
    def test_get_all_models_includes_runtime(self, model_service, sample_model_registration):
        """Test that get_all_models includes both registered and runtime models."""
        # Register a model
        model_service.register_model(sample_model_registration)

        # Get all models (this will trigger initialization)
        response = model_service.get_all_models()

        assert response.total_models == 2  # 1 registered + 1 runtime
        assert len(response.runtime_models) == 1
        assert response.runtime_models[0].model_id == "combined"

        # Check that both types are in the models list
        model_ids = [model.model_id for model in response.models]
        assert "test-gpt-4" in model_ids  # Registered model
        assert "combined" in model_ids   # Runtime model

    def test_reload_runtime_models(self, model_service):
        """Test reloading runtime models."""
        # Initialize first
        model_service._initialize()
        initial_count = len(model_service._runtime_models)

        # Reload runtime models
        with patch.dict(os.environ, {
            "EVAL_HUB_MODEL_NEW_RUNTIME_URL": "http://localhost:9000"
        }):
            model_service.reload_runtime_models()

        # Check that new runtime model was loaded
        new_model = model_service.get_model_by_id("new_runtime")
        assert new_model is not None
        assert new_model.base_url == "http://localhost:9000"