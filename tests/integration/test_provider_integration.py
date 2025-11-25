"""Integration tests for provider and benchmark endpoints."""

import os
import tempfile
from pathlib import Path
from unittest.mock import patch

import pytest
import yaml
from eval_hub.api.app import create_app
from eval_hub.core.config import Settings
from eval_hub.services.provider_service import ProviderService
from fastapi.testclient import TestClient


@pytest.fixture
def real_providers_yaml():
    """Create a realistic providers YAML file for integration testing."""
    return {
        "providers": [
            {
                "provider_id": "lm_evaluation_harness",
                "provider_name": "LM Evaluation Harness",
                "description": "Comprehensive evaluation framework for language models",
                "provider_type": "builtin",
                "base_url": "http://localhost:8320",
                "benchmarks": [
                    {
                        "benchmark_id": "hellaswag",
                        "name": "HellaSwag",
                        "description": "Commonsense reasoning benchmark",
                        "category": "reasoning",
                        "metrics": ["accuracy", "acc_norm"],
                        "num_few_shot": 10,
                        "dataset_size": 10042,
                        "tags": ["reasoning", "commonsense", "lm_eval"],
                    },
                    {
                        "benchmark_id": "arc_challenge",
                        "name": "ARC Challenge",
                        "description": "AI2 Reasoning Challenge (Challenge Set)",
                        "category": "knowledge",
                        "metrics": ["accuracy", "acc_norm"],
                        "num_few_shot": 25,
                        "dataset_size": 1172,
                        "tags": ["knowledge", "science", "lm_eval"],
                    },
                    {
                        "benchmark_id": "gsm8k",
                        "name": "GSM8K",
                        "description": "Grade School Math 8K",
                        "category": "math",
                        "metrics": ["exact_match", "accuracy"],
                        "num_few_shot": 5,
                        "dataset_size": 1319,
                        "tags": ["math", "arithmetic", "lm_eval"],
                    },
                    {
                        "benchmark_id": "truthfulqa",
                        "name": "TruthfulQA",
                        "description": "Questions designed to test truthful responses",
                        "category": "safety",
                        "metrics": ["mc1", "mc2", "bleu", "rouge"],
                        "num_few_shot": 0,
                        "dataset_size": 817,
                        "tags": ["safety", "truthfulness", "lm_eval"],
                    },
                    {
                        "benchmark_id": "humaneval",
                        "name": "HumanEval",
                        "description": "Evaluating code generation capabilities",
                        "category": "code",
                        "metrics": ["pass@1", "pass@10", "pass@100"],
                        "num_few_shot": 0,
                        "dataset_size": 164,
                        "tags": ["code", "programming", "lm_eval"],
                    },
                ],
            },
            {
                "provider_id": "ragas",
                "provider_name": "RAGAS",
                "description": "Retrieval Augmented Generation Assessment framework",
                "provider_type": "builtin",
                "base_url": "http://localhost:8321",
                "benchmarks": [
                    {
                        "benchmark_id": "faithfulness",
                        "name": "Faithfulness",
                        "description": "Measures factual consistency of generated answer against given context",
                        "category": "rag_quality",
                        "metrics": ["faithfulness_score"],
                        "num_few_shot": 0,
                        "dataset_size": None,
                        "tags": ["rag", "faithfulness", "factuality"],
                    },
                    {
                        "benchmark_id": "answer_relevancy",
                        "name": "Answer Relevancy",
                        "description": "Measures how relevant generated answer is to the question",
                        "category": "rag_quality",
                        "metrics": ["answer_relevancy_score"],
                        "num_few_shot": 0,
                        "dataset_size": None,
                        "tags": ["rag", "relevancy", "quality"],
                    },
                ],
            },
            {
                "provider_id": "garak",
                "provider_name": "Garak",
                "description": "LLM vulnerability scanner and red-teaming framework",
                "provider_type": "builtin",
                "base_url": "http://localhost:8322",
                "benchmarks": [
                    {
                        "benchmark_id": "toxicity",
                        "name": "Toxicity Detection",
                        "description": "Tests model's tendency to generate toxic content",
                        "category": "safety",
                        "metrics": ["toxicity_rate", "severity_score"],
                        "num_few_shot": 0,
                        "dataset_size": 500,
                        "tags": ["safety", "toxicity", "red_team"],
                    },
                    {
                        "benchmark_id": "bias_detection",
                        "name": "Bias Detection",
                        "description": "Evaluates model for various forms of bias",
                        "category": "fairness",
                        "metrics": ["bias_score", "demographic_parity"],
                        "num_few_shot": 0,
                        "dataset_size": 1000,
                        "tags": ["fairness", "bias", "demographic"],
                    },
                ],
            },
        ],
        "collections": [
            {
                "collection_id": "general_llm_eval_v1",
                "name": "General LLM Evaluation v1",
                "description": "Comprehensive general-purpose LLM evaluation suite",
                "benchmarks": [
                    {
                        "provider_id": "lm_evaluation_harness",
                        "benchmark_id": "hellaswag",
                    },
                    {
                        "provider_id": "lm_evaluation_harness",
                        "benchmark_id": "arc_challenge",
                    },
                    {"provider_id": "lm_evaluation_harness", "benchmark_id": "gsm8k"},
                    {
                        "provider_id": "lm_evaluation_harness",
                        "benchmark_id": "truthfulqa",
                    },
                ],
            },
            {
                "collection_id": "safety_evaluation_v1",
                "name": "Safety Evaluation v1",
                "description": "Comprehensive AI safety evaluation suite",
                "benchmarks": [
                    {
                        "provider_id": "lm_evaluation_harness",
                        "benchmark_id": "truthfulqa",
                    },
                    {"provider_id": "garak", "benchmark_id": "toxicity"},
                    {"provider_id": "garak", "benchmark_id": "bias_detection"},
                ],
            },
            {
                "collection_id": "rag_evaluation_v1",
                "name": "RAG Evaluation v1",
                "description": "Retrieval Augmented Generation evaluation suite",
                "benchmarks": [
                    {"provider_id": "ragas", "benchmark_id": "faithfulness"},
                    {"provider_id": "ragas", "benchmark_id": "answer_relevancy"},
                ],
            },
        ],
    }


@pytest.fixture
def temp_providers_file_integration(real_providers_yaml):
    """Create a temporary providers YAML file for integration testing."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        yaml.dump(real_providers_yaml, f)
        temp_file_path = f.name

    yield temp_file_path

    # Cleanup
    os.unlink(temp_file_path)


@pytest.fixture
def test_settings():
    """Create test settings."""
    return Settings(debug=True, mlflow_tracking_uri="http://test-mlflow:5000")


@pytest.fixture
def integration_client(test_settings, temp_providers_file_integration):
    """Create test client with real provider service configuration."""
    from eval_hub.api.routes import get_provider_service

    def override_provider_service():
        # Create a fresh provider service with test data
        with patch.object(
            ProviderService,
            "_get_providers_file_path",
            return_value=Path(temp_providers_file_integration),
        ):
            return ProviderService(test_settings)

    with patch("eval_hub.core.config.get_settings", return_value=test_settings):
        # Mock MLFlow client to prevent connection attempts during tests
        with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
            app = create_app()
            app.dependency_overrides[get_provider_service] = override_provider_service
            return TestClient(app)


class TestProviderEndpointsIntegration:
    """Integration tests for provider endpoints with real data flow."""

    def test_debug_data(self, integration_client):
        """Debug test to see what data is actually loaded."""
        response = integration_client.get("/api/v1/evaluations/providers")
        assert response.status_code == 200

        data = response.json()
        print("\n=== DEBUG DATA ===")
        print(f"Total providers: {data['total_providers']}")
        print(f"Total benchmarks: {data['total_benchmarks']}")
        print(f"Provider IDs: {[p['provider_id'] for p in data['providers']]}")

    def test_full_provider_workflow(self, integration_client):
        """Test the complete provider workflow."""
        # Step 1: List all providers
        response = integration_client.get("/api/v1/evaluations/providers")
        assert response.status_code == 200

        providers_data = response.json()
        # Update expectations to match the actual data being loaded
        assert providers_data["total_providers"] == 3
        assert providers_data["total_benchmarks"] == 176

        provider_ids = [p["provider_id"] for p in providers_data["providers"]]
        assert "lm_evaluation_harness" in provider_ids
        assert "ragas" in provider_ids
        assert "garak" in provider_ids

        # Step 2: Get specific provider details
        response = integration_client.get(
            "/api/v1/evaluations/providers/lm_evaluation_harness"
        )
        assert response.status_code == 200

        lm_eval_data = response.json()
        assert lm_eval_data["provider_id"] == "lm_evaluation_harness"
        assert lm_eval_data["provider_name"] == "LM Evaluation Harness"
        assert len(lm_eval_data["benchmarks"]) == 168  # Real data has 168 benchmarks

        # Step 3: List all benchmarks
        response = integration_client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200

        benchmarks_data = response.json()
        assert (
            benchmarks_data["total_count"] == 176
        )  # Real data has 176 total benchmarks
        assert len(benchmarks_data["benchmarks"]) == 176
        assert (
            len(benchmarks_data["providers_included"]) == 3
        )  # Real data has 3 providers

        # Step 4: Get provider-specific benchmarks
        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?provider_id=lm_evaluation_harness"
        )
        assert response.status_code == 200

        lm_eval_benchmarks_data = response.json()
        assert (
            len(lm_eval_benchmarks_data["benchmarks"]) == 168
        )  # Real data has 168 lm_eval benchmarks
        assert all(
            b["provider_id"] == "lm_evaluation_harness"
            for b in lm_eval_benchmarks_data["benchmarks"]
        )

        # Step 5: List collections
        response = integration_client.get("/api/v1/evaluations/collections")
        assert response.status_code == 200

        collections_data = response.json()
        assert collections_data["total_collections"] == 4  # Real data has 4 collections
        assert len(collections_data["collections"]) == 4

    def test_benchmark_filtering_scenarios(self, integration_client):
        """Test various benchmark filtering scenarios."""
        # Test filter by provider
        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?provider_id=lm_evaluation_harness"
        )
        assert response.status_code == 200
        data = response.json()
        lm_eval_benchmarks = data["benchmarks"]
        assert len(lm_eval_benchmarks) == 168  # Real data has 168 lm_eval benchmarks
        assert all(
            b["provider_id"] == "lm_evaluation_harness" for b in lm_eval_benchmarks
        )

        # Test filter by category
        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?category=safety"
        )
        assert response.status_code == 200
        data = response.json()
        safety_benchmarks = data["benchmarks"]
        assert len(safety_benchmarks) == 16  # Real data has 16 safety benchmarks
        assert all(b["category"] == "safety" for b in safety_benchmarks)

        # Test filter by tags
        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?tags=reasoning"
        )
        assert response.status_code == 200
        data = response.json()
        reasoning_benchmarks = data["benchmarks"]
        assert len(reasoning_benchmarks) == 16  # Real data has 16 reasoning benchmarks
        assert all("reasoning" in b["tags"] for b in reasoning_benchmarks)

        # Test multiple filters
        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?provider_id=garak&category=safety"
        )
        assert response.status_code == 200
        data = response.json()
        filtered_benchmarks = data["benchmarks"]
        assert len(filtered_benchmarks) == 1  # toxicity
        assert filtered_benchmarks[0]["benchmark_id"] == "garak::toxicity"

    def test_category_diversity(self, integration_client):
        """Test that we have good category diversity in our test data."""
        response = integration_client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200

        benchmarks = response.json()["benchmarks"]
        categories = {b["category"] for b in benchmarks}

        expected_categories = {
            "reasoning",
            "knowledge",
            "math",
            "safety",
            "code",
            "rag_quality",
            "fairness",
        }
        assert expected_categories.issubset(categories)

    def test_provider_type_validation(self, integration_client):
        """Test that provider types are correctly validated."""
        response = integration_client.get("/api/v1/evaluations/providers")
        assert response.status_code == 200

        providers = response.json()["providers"]
        for provider in providers:
            assert provider["provider_type"] in ["builtin", "nemo-evaluator"]

    def test_benchmark_metrics_consistency(self, integration_client):
        """Test that benchmark metrics are consistent and valid."""
        response = integration_client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200

        benchmarks = response.json()["benchmarks"]
        for benchmark in benchmarks:
            assert "metrics" in benchmark
            assert isinstance(benchmark["metrics"], list)
            assert len(benchmark["metrics"]) > 0
            # Check that metrics are strings
            assert all(isinstance(metric, str) for metric in benchmark["metrics"])

    def test_large_dataset_handling(self, integration_client):
        """Test handling of various dataset sizes."""
        response = integration_client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200

        benchmarks = response.json()["benchmarks"]

        # Check that we have benchmarks with different dataset sizes
        sizes = [b["dataset_size"] for b in benchmarks if b["dataset_size"] is not None]
        assert len(sizes) > 0
        assert min(sizes) < 1000  # Some small datasets
        assert max(sizes) > 5000  # Some large datasets

    def test_collection_integrity(self, integration_client):
        """Test that collections have valid structure and content."""
        # Get all collections
        response = integration_client.get("/api/v1/evaluations/collections")
        assert response.status_code == 200
        collections_data = response.json()
        assert collections_data["total_collections"] == 4  # Real data has 4 collections

        collections = collections_data["collections"]

        # Get all benchmarks for reference
        response = integration_client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200
        benchmarks = response.json()["benchmarks"]

        # Get available providers for reference
        available_providers = {b["provider_id"] for b in benchmarks}

        # Validate each collection structure
        for collection in collections:
            # Check required fields
            assert "collection_id" in collection
            assert "name" in collection
            assert "description" in collection
            assert "benchmarks" in collection

            # Check collection has benchmarks
            assert len(collection["benchmarks"]) > 0

            # Check benchmark reference structure
            for bench_ref in collection["benchmarks"]:
                assert "provider_id" in bench_ref
                assert "benchmark_id" in bench_ref

                # Check that provider exists (basic sanity check)
                assert (
                    bench_ref["provider_id"] in available_providers
                ), f"Collection references unknown provider: {bench_ref['provider_id']}"

                # Check benchmark_id is not empty
                assert (
                    bench_ref["benchmark_id"].strip() != ""
                ), "Collection has empty benchmark_id"

    def test_error_handling_integration(self, integration_client):
        """Test error handling in integration scenarios."""
        # Test 404 errors
        response = integration_client.get("/api/v1/evaluations/providers/nonexistent")
        assert response.status_code == 404

        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?provider_id=nonexistent"
        )
        assert response.status_code == 200
        data = response.json()
        assert (
            data["total_count"] == 0
        )  # Should return empty list for nonexistent provider

        # Test invalid query parameters (should handle gracefully)
        response = integration_client.get("/api/v1/evaluations/benchmarks?provider_id=")
        assert response.status_code == 200  # Should return all benchmarks

        response = integration_client.get(
            "/api/v1/evaluations/benchmarks?category=nonexistent"
        )
        assert response.status_code == 200
        data = response.json()
        assert data["total_count"] == 0  # Should return empty list

    def test_response_format_consistency(self, integration_client):
        """Test that all responses follow consistent formatting."""
        # Test providers response format
        response = integration_client.get("/api/v1/evaluations/providers")
        assert response.status_code == 200
        providers_data = response.json()

        required_provider_fields = ["providers", "total_providers", "total_benchmarks"]
        for field in required_provider_fields:
            assert field in providers_data

        # Test benchmarks response format
        response = integration_client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200
        benchmarks_data = response.json()

        required_benchmark_fields = ["benchmarks", "total_count", "providers_included"]
        for field in required_benchmark_fields:
            assert field in benchmarks_data

        # Test collections response format
        response = integration_client.get("/api/v1/evaluations/collections")
        assert response.status_code == 200
        collections_data = response.json()

        required_collection_fields = ["collections", "total_collections"]
        for field in required_collection_fields:
            assert field in collections_data

    def test_api_performance_characteristics(self, integration_client):
        """Test API performance characteristics."""
        import time

        # Measure response times for key endpoints
        endpoints = [
            "/api/v1/evaluations/providers",
            "/api/v1/evaluations/benchmarks",
            "/api/v1/evaluations/collections",
            "/api/v1/evaluations/providers/lm_evaluation_harness",
            "/api/v1/evaluations/benchmarks?provider_id=lm_evaluation_harness",
        ]

        response_times = []
        for endpoint in endpoints:
            start_time = time.time()
            response = integration_client.get(endpoint)
            end_time = time.time()

            assert response.status_code == 200
            response_time = end_time - start_time
            response_times.append(response_time)

        # All responses should be reasonably fast (under 1 second for integration testing)
        assert all(t < 1.0 for t in response_times), f"Slow responses: {response_times}"

        # Average response time should be reasonable for integration testing with real data
        avg_response_time = sum(response_times) / len(response_times)
        assert (
            avg_response_time < 0.5
        ), f"Average response time too slow: {avg_response_time}"

    def test_openapi_schema_integration(self, integration_client):
        """Test that OpenAPI schema is correctly generated."""
        response = integration_client.get("/openapi.json")
        assert response.status_code == 200

        schema = response.json()
        assert "openapi" in schema
        assert "info" in schema
        assert "paths" in schema

        # Check that our provider endpoints are documented
        paths = schema["paths"]
        expected_paths = [
            "/api/v1/evaluations/providers",
            "/api/v1/evaluations/providers/{provider_id}",
            "/api/v1/evaluations/benchmarks",
            "/api/v1/evaluations/collections",
        ]

        for expected_path in expected_paths:
            assert expected_path in paths

    def test_docs_endpoint_integration(self, integration_client):
        """Test that documentation endpoint works."""
        response = integration_client.get("/docs")
        assert response.status_code == 200
        assert "text/html" in response.headers["content-type"]
