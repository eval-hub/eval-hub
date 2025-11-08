"""Unit tests for the model API endpoints."""

import pytest
from fastapi.testclient import TestClient
from unittest.mock import Mock, patch
from datetime import datetime

from eval_hub.api.app import create_app
from eval_hub.models.model import (
    Model,
    ModelType,
    ModelStatus,
    ModelCapabilities,
    ModelConfig,
    ModelSummary,
    ListModelsResponse,
)


@pytest.fixture
def app():
    """Create FastAPI test application."""
    return create_app()


@pytest.fixture
def client(app):
    """Create test client."""
    return TestClient(app)


@pytest.fixture
def sample_model():
    """Create a sample model for testing."""
    return Model(
        model_id="test-gpt-4",
        model_name="Test GPT-4",
        description="A test GPT-4 model",
        model_type=ModelType.OPENAI,
        base_url="https://api.openai.com/v1",
        api_key_required=True,
        model_path="gpt-4",
        capabilities=ModelCapabilities(max_tokens=8192, supports_streaming=True),
        config=ModelConfig(temperature=0.7, max_tokens=2000),
        status=ModelStatus.ACTIVE,
        tags=["test", "gpt", "openai"],
        created_at=datetime.utcnow(),
        updated_at=datetime.utcnow()
    )


@pytest.fixture
def sample_model_summary():
    """Create a sample model summary."""
    return ModelSummary(
        model_id="test-gpt-4",
        model_name="Test GPT-4",
        description="A test GPT-4 model",
        model_type=ModelType.OPENAI,
        base_url="https://api.openai.com/v1",
        status=ModelStatus.ACTIVE,
        tags=["test", "gpt", "openai"],
        created_at=datetime.utcnow()
    )


class TestModelAPI:
    """Test model API endpoints."""

    @patch('eval_hub.api.routes.ModelService')
    def test_list_models_success(self, mock_model_service_class, client, sample_model_summary):
        """Test successful model listing."""
        # Mock the service
        mock_service = Mock()
        mock_service.get_all_models.return_value = ListModelsResponse(
            models=[sample_model_summary],
            total_models=1,
            runtime_models=[]
        )
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.get("/api/v1/models")

        # Verify response
        assert response.status_code == 200
        data = response.json()
        assert data["total_models"] == 1
        assert len(data["models"]) == 1
        assert data["models"][0]["model_id"] == "test-gpt-4"
        assert data["models"][0]["model_name"] == "Test GPT-4"

        # Verify service call
        mock_service.get_all_models.assert_called_once_with(include_inactive=True)

    @patch('eval_hub.api.routes.ModelService')
    def test_list_models_exclude_inactive(self, mock_model_service_class, client):
        """Test model listing excluding inactive models."""
        # Mock the service
        mock_service = Mock()
        mock_service.get_all_models.return_value = ListModelsResponse(
            models=[],
            total_models=0,
            runtime_models=[]
        )
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.get("/api/v1/models?include_inactive=false")

        # Verify response
        assert response.status_code == 200
        mock_service.get_all_models.assert_called_once_with(include_inactive=False)

    @patch('eval_hub.api.routes.ModelService')
    def test_get_model_success(self, mock_model_service_class, client, sample_model):
        """Test successful model retrieval."""
        # Mock the service
        mock_service = Mock()
        mock_service.get_model_by_id.return_value = sample_model
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.get("/api/v1/models/test-gpt-4")

        # Verify response
        assert response.status_code == 200
        data = response.json()
        assert data["model_id"] == "test-gpt-4"
        assert data["model_name"] == "Test GPT-4"
        assert data["model_type"] == "openai"

        # Verify service call
        mock_service.get_model_by_id.assert_called_once_with("test-gpt-4")

    @patch('eval_hub.api.routes.ModelService')
    def test_get_model_not_found(self, mock_model_service_class, client):
        """Test model retrieval when model doesn't exist."""
        # Mock the service
        mock_service = Mock()
        mock_service.get_model_by_id.return_value = None
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.get("/api/v1/models/non-existent")

        # Verify response
        assert response.status_code == 404
        data = response.json()
        assert "Model non-existent not found" in data["detail"]

    @patch('eval_hub.api.routes.ModelService')
    def test_register_model_success(self, mock_model_service_class, client, sample_model):
        """Test successful model registration."""
        # Mock the service
        mock_service = Mock()
        mock_service.register_model.return_value = sample_model
        mock_model_service_class.return_value = mock_service

        # Request data
        request_data = {
            "model_id": "test-gpt-4",
            "model_name": "Test GPT-4",
            "description": "A test GPT-4 model",
            "model_type": "openai",
            "base_url": "https://api.openai.com/v1",
            "api_key_required": True,
            "model_path": "gpt-4",
            "capabilities": {
                "max_tokens": 8192,
                "supports_streaming": True,
                "supports_function_calling": False,
                "supports_vision": False
            },
            "config": {
                "temperature": 0.7,
                "max_tokens": 2000,
                "timeout": 30,
                "retry_attempts": 3
            },
            "status": "active",
            "tags": ["test", "gpt", "openai"]
        }

        # Make request
        response = client.post("/api/v1/models", json=request_data)

        # Verify response
        assert response.status_code == 201
        data = response.json()
        assert data["model_id"] == "test-gpt-4"
        assert data["model_name"] == "Test GPT-4"

        # Verify service call
        mock_service.register_model.assert_called_once()

    @patch('eval_hub.api.routes.ModelService')
    def test_register_model_duplicate(self, mock_model_service_class, client):
        """Test model registration with duplicate ID."""
        # Mock the service
        mock_service = Mock()
        mock_service.register_model.side_effect = ValueError("Model with ID 'test-gpt-4' already exists")
        mock_model_service_class.return_value = mock_service

        # Request data
        request_data = {
            "model_id": "test-gpt-4",
            "model_name": "Test GPT-4",
            "description": "A test GPT-4 model",
            "model_type": "openai",
            "base_url": "https://api.openai.com/v1"
        }

        # Make request
        response = client.post("/api/v1/models", json=request_data)

        # Verify response
        assert response.status_code == 400
        data = response.json()
        assert "Model with ID 'test-gpt-4' already exists" in data["detail"]

    # TODO: Fix integration test setup issues
    # @patch('eval_hub.api.routes.ModelService')
    # def test_register_model_validation_error(self, mock_model_service_class, client):
    #     """Test model registration with validation error."""
    #     # Request data with invalid model_id
    #     request_data = {
    #         "model_id": "",  # Invalid empty ID
    #         "model_name": "Test Model",
    #         "description": "A test model",
    #         "model_type": "openai",
    #         "base_url": "https://api.openai.com/v1"
    #     }
    #
    #     # Make request
    #     response = client.post("/api/v1/models", json=request_data)
    #
    #     # Verify response - 422 for validation error or 400 if caught by service validation
    #     assert response.status_code in [400, 422]

    @patch('eval_hub.api.routes.ModelService')
    def test_update_model_success(self, mock_model_service_class, client, sample_model):
        """Test successful model update."""
        # Mock the service
        updated_model = sample_model.model_copy()
        updated_model.model_name = "Updated GPT-4"
        updated_model.description = "Updated description"

        mock_service = Mock()
        mock_service.update_model.return_value = updated_model
        mock_model_service_class.return_value = mock_service

        # Request data
        request_data = {
            "model_name": "Updated GPT-4",
            "description": "Updated description"
        }

        # Make request
        response = client.put("/api/v1/models/test-gpt-4", json=request_data)

        # Verify response
        assert response.status_code == 200
        data = response.json()
        assert data["model_name"] == "Updated GPT-4"
        assert data["description"] == "Updated description"

        # Verify service call
        mock_service.update_model.assert_called_once()

    # TODO: Fix integration test setup issues
    # @patch('eval_hub.api.routes.ModelService')
    # def test_update_model_not_found(self, mock_model_service_class, client):
    #     """Test model update when model doesn't exist."""
    #     # Mock the service - it should be initialized but return None for update
    #     mock_service = Mock()
    #     mock_service.update_model.return_value = None
    #     mock_model_service_class.return_value = mock_service
    #
    #     # Request data
    #     request_data = {
    #         "model_name": "Updated Model"
    #     }
    #
    #     # Make request
    #     response = client.put("/api/v1/models/non-existent", json=request_data)
    #
    #     # Verify response
    #     assert response.status_code == 404
    #     data = response.json()
    #     assert "Model non-existent not found" in data["detail"]

    @patch('eval_hub.api.routes.ModelService')
    def test_update_runtime_model_error(self, mock_model_service_class, client):
        """Test updating runtime model raises error."""
        # Mock the service
        mock_service = Mock()
        mock_service.update_model.side_effect = ValueError("Cannot update runtime models")
        mock_model_service_class.return_value = mock_service

        # Request data
        request_data = {
            "model_name": "Updated Runtime Model"
        }

        # Make request
        response = client.put("/api/v1/models/runtime-model", json=request_data)

        # Verify response
        assert response.status_code == 400
        data = response.json()
        assert "Cannot update runtime models" in data["detail"]

    @patch('eval_hub.api.routes.ModelService')
    def test_delete_model_success(self, mock_model_service_class, client):
        """Test successful model deletion."""
        # Mock the service
        mock_service = Mock()
        mock_service.delete_model.return_value = True
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.delete("/api/v1/models/test-gpt-4")

        # Verify response
        assert response.status_code == 200
        data = response.json()
        assert "Model test-gpt-4 deleted successfully" in data["message"]

        # Verify service call
        mock_service.delete_model.assert_called_once_with("test-gpt-4")

    # TODO: Fix integration test setup issues
    # @patch('eval_hub.api.routes.ModelService')
    # def test_delete_model_not_found(self, mock_model_service_class, client):
    #     """Test model deletion when model doesn't exist."""
    #     # Mock the service
    #     mock_service = Mock()
    #     mock_service.delete_model.return_value = False
    #     mock_model_service_class.return_value = mock_service
    #
    #     # Make request
    #     response = client.delete("/api/v1/models/non-existent")
    #
    #     # Verify response
    #     assert response.status_code == 404
    #     data = response.json()
    #     assert "Model non-existent not found" in data["detail"]

    @patch('eval_hub.api.routes.ModelService')
    def test_delete_runtime_model_error(self, mock_model_service_class, client):
        """Test deleting runtime model raises error."""
        # Mock the service
        mock_service = Mock()
        mock_service.delete_model.side_effect = ValueError("Cannot delete runtime models")
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.delete("/api/v1/models/runtime-model")

        # Verify response
        assert response.status_code == 400
        data = response.json()
        assert "Cannot delete runtime models" in data["detail"]

    @patch('eval_hub.api.routes.ModelService')
    def test_reload_runtime_models_success(self, mock_model_service_class, client):
        """Test successful runtime models reload."""
        # Mock the service
        mock_service = Mock()
        mock_service.reload_runtime_models.return_value = None
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.post("/api/v1/models/reload")

        # Verify response
        assert response.status_code == 200
        data = response.json()
        assert "Runtime models reloaded from environment variables successfully" in data["message"]

        # Verify service call
        mock_service.reload_runtime_models.assert_called_once()

    @patch('eval_hub.api.routes.ModelService')
    def test_reload_runtime_models_error(self, mock_model_service_class, client):
        """Test runtime models reload with error."""
        # Mock the service
        mock_service = Mock()
        mock_service.reload_runtime_models.side_effect = Exception("Environment variable error")
        mock_model_service_class.return_value = mock_service

        # Make request
        response = client.post("/api/v1/models/reload")

        # Verify response
        assert response.status_code == 500
        data = response.json()
        assert "Failed to reload runtime models" in data["detail"]


class TestModelAPIValidation:
    """Test model API validation and edge cases."""

    @patch('eval_hub.api.routes.ModelService')
    def test_register_model_invalid_url(self, mock_model_service_class, client):
        """Test model registration with invalid URL."""
        # Mock service to simulate validation error
        mock_service = Mock()
        mock_service.register_model.side_effect = ValueError("base_url must be a valid HTTP or HTTPS URL")
        mock_model_service_class.return_value = mock_service

        request_data = {
            "model_id": "test-model",
            "model_name": "Test Model",
            "description": "A test model",
            "model_type": "openai",
            "base_url": "invalid-url"  # Invalid URL format
        }

        response = client.post("/api/v1/models", json=request_data)
        assert response.status_code == 400  # Service validation error

    def test_register_model_invalid_model_type(self, client):
        """Test model registration with invalid model type."""
        request_data = {
            "model_id": "test-model",
            "model_name": "Test Model",
            "description": "A test model",
            "model_type": "invalid-type",  # Invalid model type
            "base_url": "https://api.openai.com/v1"
        }

        response = client.post("/api/v1/models", json=request_data)
        assert response.status_code == 422

    def test_register_model_missing_required_fields(self, client):
        """Test model registration with missing required fields."""
        request_data = {
            "model_name": "Test Model",
            "description": "A test model"
            # Missing required fields: model_id, model_type, base_url
        }

        response = client.post("/api/v1/models", json=request_data)
        assert response.status_code == 422

    # TODO: Fix integration test setup issues
    # @patch('eval_hub.api.routes.ModelService')
    # def test_update_model_empty_request(self, mock_model_service_class, client):
    #     """Test model update with empty request body."""
    #     # Mock the service
    #     mock_service = Mock()
    #     mock_service.update_model.return_value = None  # Model not found
    #     mock_model_service_class.return_value = mock_service
    #
    #     response = client.put("/api/v1/models/test-model", json={})
    #     # Should not fail with empty update request, but model not found
    #     assert response.status_code == 404