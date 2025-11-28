"""Integration tests for the API endpoints."""

from unittest.mock import patch

import pytest
from fastapi.testclient import TestClient

from eval_hub.api.app import create_app
from eval_hub.core.config import Settings


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
        risk_category_benchmarks={
            "low": {"benchmarks": ["hellaswag"], "num_fewshot": 5, "limit": 100}
        },
    )


@pytest.fixture
def client(test_settings):
    """Create test client."""
    with patch("eval_hub.core.config.get_settings", return_value=test_settings):
        # Mock MLFlow client to prevent connection attempts during tests
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            app = create_app()
            return TestClient(app)


class TestAPIEndpoints:
    """Test API endpoints."""

    def test_health_check(self, client):
        """Test health check endpoint."""
        response = client.get("/api/v1/health")

        assert response.status_code == 200
        data = response.json()

        assert data["status"] == "healthy"
        assert "version" in data
        assert "timestamp" in data
        assert "uptime_seconds" in data
        assert "components" in data

    def test_create_evaluation_with_risk_category(self, client):
        """Test creating evaluation with basic benchmarks."""
        request_data = {
            "model": {"url": "http://test-server:8000", "name": "test-model"},
            "benchmarks": [
                {
                    "benchmark_id": "arc_easy",
                    "provider_id": "lm_evaluation_harness",
                    "config": {"num_fewshot": 0, "limit": 100},
                }
            ],
            "experiment": {"name": "Test Evaluation"},
        }

        # Mock MLFlow client initialization to prevent hanging during dependency injection
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            with patch(
                "eval_hub.services.mlflow_client.MLFlowClient.create_experiment",
                return_value="test-exp-1",
            ):
                with patch(
                    "eval_hub.services.mlflow_client.MLFlowClient.get_experiment_url",
                    return_value="http://test-mlflow:5000/#/experiments/1",
                ):
                    response = client.post(
                        "/api/v1/evaluations/jobs", json=request_data
                    )

        assert response.status_code == 202
        data = response.json()

        assert "id" in data
        assert data["status"] in ["pending", "running"]
        assert data["total_evaluations"] > 0
        assert "experiment_url" in data

    def test_create_evaluation_with_explicit_backends(self, client):
        """Test creating evaluation with multiple benchmarks."""
        request_data = {
            "model": {"url": "http://test-server:8000", "name": "test-model"},
            "benchmarks": [
                {
                    "benchmark_id": "arc_easy",
                    "provider_id": "lm_evaluation_harness",
                    "config": {"num_fewshot": 0, "limit": 100},
                },
                {
                    "benchmark_id": "blimp",
                    "provider_id": "lm_evaluation_harness",
                    "config": {"num_fewshot": 0, "limit": 50},
                },
            ],
            "experiment": {"name": "Explicit Backend Test"},
        }

        # Mock MLFlow client initialization to prevent hanging during dependency injection
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            with patch(
                "eval_hub.services.mlflow_client.MLFlowClient.create_experiment",
                return_value="test-exp-2",
            ):
                with patch(
                    "eval_hub.services.mlflow_client.MLFlowClient.get_experiment_url",
                    return_value="http://test-mlflow:5000/#/experiments/2",
                ):
                    response = client.post(
                        "/api/v1/evaluations/jobs", json=request_data
                    )

        assert response.status_code == 202
        data = response.json()

        assert "id" in data
        assert data["total_evaluations"] == 2  # Updated to match 2 benchmarks

    def test_create_evaluation_validation_error(self, client):
        """Test validation error for invalid evaluation request."""
        request_data = {
            "model": {
                "url": "http://test-server:8000",
                "name": "",  # Empty model name should fail validation
            },
            "benchmarks": [
                {
                    "benchmark_id": "arc_easy",
                    "provider_id": "lm_evaluation_harness",
                    "config": {},
                }
            ],
            "experiment": {"name": "Validation Test"},
        }

        # Mock MLFlow client initialization to prevent hanging during dependency injection
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            response = client.post("/api/v1/evaluations/jobs", json=request_data)

        assert response.status_code == 400
        data = response.json()
        assert "detail" in data
        assert "validation" in data["detail"].lower()

    def test_get_evaluation_status_not_found(self, client):
        """Test getting status for non-existent evaluation."""
        fake_id = "550e8400-e29b-41d4-a716-446655440000"

        response = client.get(f"/api/v1/evaluations/jobs/{fake_id}")

        assert response.status_code == 404
        data = response.json()
        assert "not found" in data["detail"].lower()

    def test_list_evaluations_empty(self, client):
        """Test listing evaluations when none exist."""
        # Clear any evaluations from previous tests
        from eval_hub.api.routes import active_evaluations

        active_evaluations.clear()

        response = client.get("/api/v1/evaluations/jobs")

        assert response.status_code == 200
        data = response.json()
        assert isinstance(data, list)
        assert len(data) == 0

    def test_list_evaluations_with_filter(self, client):
        """Test listing evaluations with status filter."""
        response = client.get("/api/v1/evaluations/jobs?status_filter=completed")

        assert response.status_code == 200
        data = response.json()
        assert isinstance(data, list)

    def test_cancel_evaluation_not_found(self, client):
        """Test canceling non-existent evaluation."""
        fake_id = "550e8400-e29b-41d4-a716-446655440000"

        response = client.delete(f"/api/v1/evaluations/jobs/{fake_id}")

        assert response.status_code == 404
        data = response.json()
        assert "not found" in data["detail"].lower()

    def test_get_evaluation_summary_not_found(self, client):
        """Test getting summary for non-existent evaluation."""
        fake_id = "550e8400-e29b-41d4-a716-446655440000"

        response = client.get(f"/api/v1/evaluations/jobs/{fake_id}/summary")

        assert response.status_code == 404
        data = response.json()
        assert "not found" in data["detail"].lower()

    def test_get_system_metrics(self, client):
        """Test getting system metrics."""
        response = client.get("/api/v1/metrics/system")

        assert response.status_code == 200
        data = response.json()

        assert "active_evaluations" in data
        assert "running_tasks" in data
        assert "total_requests" in data
        assert "memory_usage" in data
        assert "status_breakdown" in data

    def test_metrics_endpoint(self, client):
        """Test Prometheus metrics endpoint."""
        response = client.get("/metrics")

        assert response.status_code == 200
        # Prometheus metrics should be in text format
        assert response.headers["content-type"].startswith("text/plain")

    def test_openapi_schema(self, client):
        """Test OpenAPI schema endpoint."""
        response = client.get("/openapi.json")

        assert response.status_code == 200
        data = response.json()

        assert "openapi" in data
        assert "info" in data
        assert "paths" in data

    def test_docs_endpoint(self, client):
        """Test Swagger UI endpoint."""
        response = client.get("/docs")

        assert response.status_code == 200
        # Should return HTML content
        assert "text/html" in response.headers["content-type"]

    def test_async_mode_parameter(self, client):
        """Test async mode parameter in evaluation creation."""
        request_data = {
            "model": {"url": "http://test-server:8000", "name": "test-model"},
            "benchmarks": [
                {
                    "benchmark_id": "arc_easy",
                    "provider_id": "lm_evaluation_harness",
                    "config": {"num_fewshot": 0, "limit": 100},
                }
            ],
            "experiment": {"name": "Sync Test"},
            "async_mode": False,
        }

        # Mock MLFlow client initialization to prevent hanging during dependency injection
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            with patch(
                "eval_hub.services.mlflow_client.MLFlowClient.create_experiment",
                return_value="test-exp-sync",
            ):
                with patch(
                    "eval_hub.services.mlflow_client.MLFlowClient.get_experiment_url",
                    return_value="http://test-mlflow:5000/#/experiments/sync",
                ):
                    with patch(
                        "eval_hub.services.executor.EvaluationExecutor.execute_evaluation_request",
                        return_value=[],
                    ):
                        # Test synchronous mode
                        response = client.post(
                            "/api/v1/evaluations/jobs", json=request_data
                        )

        assert response.status_code == 202

    def test_request_limit_parameter(self, client):
        """Test limit parameter in list evaluations."""
        response = client.get("/api/v1/evaluations/jobs?limit=10")

        assert response.status_code == 200
        data = response.json()
        assert isinstance(data, list)
        assert len(data) <= 10

    def test_invalid_request_format(self, client):
        """Test handling of invalid JSON request."""
        response = client.post(
            "/api/v1/evaluations/jobs",
            data="invalid json",
            headers={"Content-Type": "application/json"},
        )

        assert response.status_code == 422  # Unprocessable Entity

    def test_missing_required_fields(self, client):
        """Test handling of missing required fields."""
        request_data = {
            "evaluations": [
                {
                    "name": "Incomplete Test",
                    # Missing model_name and backends/risk_category
                }
            ]
        }

        # Mock MLFlow client initialization to prevent hanging during dependency injection
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            response = client.post("/api/v1/evaluations/jobs", json=request_data)

        assert response.status_code == 422  # Validation error
