"""Unit tests for API routes error handling paths to improve coverage."""

from unittest.mock import MagicMock, patch

import pytest
from fastapi.testclient import TestClient

from eval_hub.api.app import create_app
from eval_hub.core.config import Settings
from eval_hub.models.provider import ProviderRecord, ProviderType


@pytest.fixture
def test_settings():
    """Create test settings."""
    return Settings(
        debug=True,
        mlflow_tracking_uri="http://test-mlflow:5000",
        backend_configs={
            "lm-evaluation-harness": {
                "image": "eval-harness:test",
                "resources": {"cpu": "1", "memory": "2Gi"},
                "timeout": 1800,
            }
        },
    )


@pytest.fixture
def client(test_settings):
    """Create test client."""
    with patch("eval_hub.core.config.get_settings", return_value=test_settings):
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            app = create_app()
            return TestClient(app)


class TestRoutesErrorHandling:
    """Test error handling paths in API routes."""

    @pytest.mark.skip(
        reason="Benchmark validation removed - any benchmark ID now allowed"
    )
    def test_create_evaluation_benchmark_not_found(self, client):
        """Test creating evaluation with non-existent benchmark - DISABLED: validation removed."""
        request_data = {
            "model": {"url": "http://test-server:8000", "name": "test-model"},
            "benchmarks": [
                {
                    "name": "Nonexistent Test",
                    "id": "nonexistent_benchmark",
                    "provider_id": "lm_evaluation_harness",
                    "config": {"num_fewshot": 0, "limit": 100},
                }
            ],
            "experiment": {"name": "Test Error Handling"},
        }

        # Create a mock provider service
        mock_service = MagicMock()
        mock_service.get_benchmark_by_id.return_value = None

        # Override the dependency
        from eval_hub.api.routes import get_provider_service

        client.app.dependency_overrides[get_provider_service] = lambda: mock_service

        try:
            # Mock MLFlow client to prevent hanging on connection
            with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
                with patch(
                    "eval_hub.services.mlflow_client.MLFlowClient.create_experiment",
                    return_value="test-exp",
                ):
                    response = client.post(
                        "/api/v1/evaluations/jobs", json=request_data
                    )

                    # HTTPException gets caught by generic handler and converted to 500
                    assert response.status_code == 500
                    data = response.json()
                    assert (
                        "Benchmark lm_evaluation_harness::nonexistent_benchmark not found"
                        in data["detail"]
                    )
        finally:
            # Clean up the override
            client.app.dependency_overrides.clear()

    def test_create_evaluation_provider_not_found(self, client):
        """Test creating evaluation with existing benchmark but non-existent provider."""
        # Use existing benchmark from real provider service that will pass validation
        request_data = {
            "model": {"url": "http://test-server:8000", "name": "test-model"},
            "benchmarks": [
                {
                    "name": "ARC Easy Test",
                    "id": "arc_easy",  # Use existing benchmark
                    "provider_id": "nonexistent_provider",  # Use non-existent provider
                    "config": {"num_fewshot": 0, "limit": 100},
                }
            ],
            "experiment": {"name": "Test Provider Error"},
        }

        # Create a mock provider service
        mock_service = MagicMock()
        # First call passes benchmark validation with the original provider_id
        mock_service.get_benchmark_by_id.return_value = MagicMock(
            benchmark_id="arc_easy", provider_id="nonexistent_provider"
        )
        # Second call fails provider validation - provider doesn't exist
        mock_service.get_provider_by_id.return_value = None

        # Override the dependency
        from eval_hub.api.routes import get_provider_service

        client.app.dependency_overrides[get_provider_service] = lambda: mock_service

        try:
            # Mock MLFlow client to prevent hanging on connection
            with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
                with patch(
                    "eval_hub.services.mlflow_client.MLFlowClient.create_experiment",
                    return_value="test-exp",
                ):
                    response = client.post(
                        "/api/v1/evaluations/jobs", json=request_data
                    )

                    # HTTPException gets caught by generic handler and converted to 500
                    assert response.status_code == 500
                    data = response.json()
                    assert "Provider nonexistent_provider not found" in data["detail"]
        finally:
            # Clean up the override
            client.app.dependency_overrides.clear()

    def test_create_evaluation_unsupported_provider_type(self, client):
        """Test creating evaluation with unsupported provider type."""
        # Use existing benchmark to pass initial validation
        request_data = {
            "model": {"url": "http://test-server:8000", "name": "test-model"},
            "benchmarks": [
                {
                    "name": "ARC Easy Test",
                    "id": "arc_easy",  # Use existing benchmark
                    "provider_id": "unsupported_provider",
                    "config": {"num_fewshot": 0, "limit": 100},
                }
            ],
            "experiment": {"name": "Test Unsupported Provider"},
        }

        # Create a mock provider service
        mock_service = MagicMock()
        # First call passes benchmark validation
        mock_service.get_benchmark_by_id.return_value = MagicMock(
            benchmark_id="arc_easy", provider_id="unsupported_provider"
        )

        # Second call returns provider with unsupported type (BUILTIN but not lm_evaluation_harness)
        mock_provider = ProviderRecord(
            provider_id="unsupported_provider",
            provider_name="Unsupported Provider",
            description="An unsupported provider",
            provider_type=ProviderType.BUILTIN,
            base_url=None,
            benchmarks=[],
        )
        mock_service.get_provider_by_id.return_value = mock_provider

        # Override the dependency
        from eval_hub.api.routes import get_provider_service

        client.app.dependency_overrides[get_provider_service] = lambda: mock_service

        try:
            # Mock MLFlow client to prevent hanging on connection
            with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
                with patch(
                    "eval_hub.services.mlflow_client.MLFlowClient.create_experiment",
                    return_value="test-exp",
                ):
                    response = client.post(
                        "/api/v1/evaluations/jobs", json=request_data
                    )

                    # HTTPException gets caught by generic handler and converted to 500
                    assert response.status_code == 500
                    data = response.json()
                    assert "Unsupported provider type" in data["detail"]
                    assert "unsupported_provider" in data["detail"]
        finally:
            # Clean up the override
            client.app.dependency_overrides.clear()
