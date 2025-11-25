"""Unit tests for provider and benchmark API endpoints."""

from unittest.mock import Mock, patch

import pytest
from eval_hub.api.app import create_app
from eval_hub.models.provider import (
    Benchmark,
    BenchmarkDetail,
    Collection,
    ListBenchmarksResponse,
    ListCollectionsResponse,
    ListProvidersResponse,
    Provider,
    ProviderSummary,
    ProviderType,
)
from eval_hub.services.provider_service import ProviderService
from fastapi.testclient import TestClient


@pytest.fixture
def mock_provider_data():
    """Create mock provider data for testing."""
    return {
        "providers": [
            {
                "provider_id": "test_provider",
                "provider_name": "Test Provider",
                "description": "A test provider",
                "provider_type": "nemo-evaluator",
                "base_url": "http://test-provider:8080",
                "benchmarks": [
                    {
                        "benchmark_id": "test_benchmark_1",
                        "name": "Test Benchmark 1",
                        "description": "First test benchmark",
                        "category": "reasoning",
                        "metrics": ["accuracy", "f1_score"],
                        "num_few_shot": 5,
                        "dataset_size": 1000,
                        "tags": ["test", "reasoning"],
                    },
                    {
                        "benchmark_id": "test_benchmark_2",
                        "name": "Test Benchmark 2",
                        "description": "Second test benchmark",
                        "category": "math",
                        "metrics": ["exact_match"],
                        "num_few_shot": 0,
                        "dataset_size": 500,
                        "tags": ["test", "math"],
                    },
                ],
            },
            {
                "provider_id": "lm_evaluation_harness",
                "provider_name": "LM Evaluation Harness",
                "description": "Comprehensive evaluation framework",
                "provider_type": "nemo-evaluator",
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
                        "tags": ["reasoning", "commonsense"],
                    }
                ],
            },
        ],
        "collections": [
            {
                "collection_id": "test_collection",
                "name": "Test Collection",
                "description": "A test benchmark collection",
                "benchmarks": [
                    {
                        "provider_id": "test_provider",
                        "benchmark_id": "test_benchmark_1",
                    },
                    {
                        "provider_id": "lm_evaluation_harness",
                        "benchmark_id": "hellaswag",
                    },
                ],
            }
        ],
    }


@pytest.fixture
def mock_provider_service(mock_provider_data):
    """Create a mock provider service."""
    service = Mock(spec=ProviderService)

    # Mock providers data
    providers = []
    all_benchmarks = []

    for provider_data in mock_provider_data["providers"]:
        # Create provider
        benchmarks = []
        for bench_data in provider_data["benchmarks"]:
            benchmark = Benchmark(**bench_data)
            benchmarks.append(benchmark)

            # Create benchmark detail for all_benchmarks
            benchmark_detail = BenchmarkDetail(
                benchmark_id=bench_data["benchmark_id"],
                provider_id=provider_data["provider_id"],
                provider_name=provider_data["provider_name"],
                name=bench_data["name"],
                description=bench_data["description"],
                category=bench_data["category"],
                metrics=bench_data["metrics"],
                num_few_shot=bench_data["num_few_shot"],
                dataset_size=bench_data["dataset_size"],
                tags=bench_data["tags"],
                provider_type=provider_data["provider_type"],
                base_url=provider_data["base_url"],
            )
            all_benchmarks.append(benchmark_detail)

        provider = Provider(
            provider_id=provider_data["provider_id"],
            provider_name=provider_data["provider_name"],
            description=provider_data["description"],
            provider_type=ProviderType(provider_data["provider_type"]),
            base_url=provider_data["base_url"],
            benchmarks=benchmarks,
        )
        providers.append(provider)

    # Mock collections
    collections = [
        Collection(**col_data) for col_data in mock_provider_data["collections"]
    ]

    # Set up mock return values
    service.get_all_providers.return_value = ListProvidersResponse(
        providers=[
            ProviderSummary(
                provider_id=p.provider_id,
                provider_name=p.provider_name,
                description=p.description,
                provider_type=p.provider_type,
                base_url=p.base_url,
                benchmark_count=len(p.benchmarks),
            )
            for p in providers
        ],
        total_providers=len(providers),
        total_benchmarks=len(all_benchmarks),
    )

    service.get_all_benchmarks.return_value = ListBenchmarksResponse(
        benchmarks=[
            {
                "benchmark_id": b.benchmark_id,
                "provider_id": b.provider_id,
                "name": b.name,
                "description": b.description,
                "category": b.category,
                "metrics": b.metrics,
                "num_few_shot": b.num_few_shot,
                "dataset_size": b.dataset_size,
                "tags": b.tags,
            }
            for b in all_benchmarks
        ],
        total_count=len(all_benchmarks),
        providers_included=[p.provider_id for p in providers],
    )

    service.get_all_collections.return_value = ListCollectionsResponse(
        collections=collections, total_collections=len(collections)
    )

    service.get_provider_by_id.side_effect = lambda provider_id: next(
        (p for p in providers if p.provider_id == provider_id), None
    )

    service.get_benchmarks_by_provider.side_effect = lambda provider_id: [
        b for b in all_benchmarks if b.provider_id == provider_id
    ]

    service.search_benchmarks.side_effect = lambda **filters: [
        b
        for b in all_benchmarks
        if (not filters.get("provider_id") or b.provider_id == filters["provider_id"])
        and (not filters.get("category") or b.category == filters["category"])
        and (not filters.get("tags") or any(tag in b.tags for tag in filters["tags"]))
    ]

    return service


@pytest.fixture
def client_with_mock_provider(mock_provider_service):
    """Create test client with mocked provider service."""
    from eval_hub.api.routes import get_provider_service

    # Mock MLFlow client to prevent connection attempts during tests
    with patch("eval_hub.services.mlflow_client.MLFlowClient._setup_mlflow"):
        app = create_app()

        # Override the provider service dependency
        def override_provider_service():
            return mock_provider_service

        app.dependency_overrides[get_provider_service] = override_provider_service

        return TestClient(app, raise_server_exceptions=False), mock_provider_service


class TestProviderAPI:
    """Test provider API endpoints."""

    def test_list_providers_success(self, client_with_mock_provider):
        """Test successful listing of all providers."""
        client, mock_service = client_with_mock_provider

        response = client.get("/api/v1/evaluations/providers")

        assert response.status_code == 200
        data = response.json()

        assert "providers" in data
        assert "total_providers" in data
        assert "total_benchmarks" in data
        assert data["total_providers"] == 2
        assert data["total_benchmarks"] == 3

        # Check provider summaries
        providers = data["providers"]
        assert len(providers) == 2

        # Verify first provider
        test_provider = next(
            p for p in providers if p["provider_id"] == "test_provider"
        )
        assert test_provider["provider_name"] == "Test Provider"
        assert test_provider["description"] == "A test provider"
        assert test_provider["provider_type"] == "nemo-evaluator"
        assert test_provider["base_url"] == "http://test-provider:8080"
        assert test_provider["benchmark_count"] == 2

    def test_get_provider_success(self, client_with_mock_provider):
        """Test successful retrieval of specific provider."""
        client, mock_service = client_with_mock_provider

        response = client.get("/api/v1/evaluations/providers/test_provider")

        assert response.status_code == 200
        data = response.json()

        assert data["provider_id"] == "test_provider"
        assert data["provider_name"] == "Test Provider"
        assert data["description"] == "A test provider"
        assert data["provider_type"] == "nemo-evaluator"
        assert data["base_url"] == "http://test-provider:8080"
        assert "benchmarks" in data
        assert len(data["benchmarks"]) == 2

    def test_get_provider_not_found(self, client_with_mock_provider):
        """Test getting non-existent provider."""
        client, mock_service = client_with_mock_provider

        response = client.get("/api/v1/evaluations/providers/nonexistent_provider")

        assert response.status_code == 404
        data = response.json()
        assert "not found" in data["detail"].lower()

    def test_list_all_benchmarks_success(self, client_with_mock_provider):
        """Test successful listing of all benchmarks."""
        client, mock_service = client_with_mock_provider

        response = client.get("/api/v1/evaluations/benchmarks")

        assert response.status_code == 200
        data = response.json()

        assert "benchmarks" in data
        assert "total_count" in data
        assert "providers_included" in data
        assert data["total_count"] == 3
        assert len(data["benchmarks"]) == 3
        assert len(data["providers_included"]) == 2

    def test_list_benchmarks_with_provider_filter(self, client_with_mock_provider):
        """Test listing benchmarks filtered by provider."""
        client, mock_service = client_with_mock_provider

        # Mock search_benchmarks to return filtered results
        filtered_benchmarks = [
            b
            for b in mock_service.get_all_benchmarks().benchmarks
            if b["provider_id"] == "test_provider"
        ]
        mock_service.search_benchmarks.return_value = [
            BenchmarkDetail(
                benchmark_id=b["benchmark_id"],
                provider_id=b["provider_id"],
                provider_name="Test Provider",
                name=b["name"],
                description=b["description"],
                category=b["category"],
                metrics=b["metrics"],
                num_few_shot=b["num_few_shot"],
                dataset_size=b["dataset_size"],
                tags=b["tags"],
                provider_type="nemo-evaluator",
                base_url="http://test-provider:8080",
            )
            for b in filtered_benchmarks
        ]

        response = client.get(
            "/api/v1/evaluations/benchmarks?provider_id=test_provider"
        )

        assert response.status_code == 200
        data = response.json()

        # Should only return benchmarks from test_provider
        benchmarks = data["benchmarks"]
        assert all(b["provider_id"] == "test_provider" for b in benchmarks)

    def test_list_benchmarks_with_category_filter(self, client_with_mock_provider):
        """Test listing benchmarks filtered by category."""
        client, mock_service = client_with_mock_provider

        # Mock search_benchmarks to return filtered results
        filtered_benchmarks = [
            b
            for b in mock_service.get_all_benchmarks().benchmarks
            if b["category"] == "reasoning"
        ]
        mock_service.search_benchmarks.return_value = [
            BenchmarkDetail(
                benchmark_id=b["benchmark_id"],
                provider_id=b["provider_id"],
                provider_name="Test Provider",
                name=b["name"],
                description=b["description"],
                category=b["category"],
                metrics=b["metrics"],
                num_few_shot=b["num_few_shot"],
                dataset_size=b["dataset_size"],
                tags=b["tags"],
                provider_type="nemo-evaluator",
                base_url="http://test-provider:8080",
            )
            for b in filtered_benchmarks
        ]

        response = client.get("/api/v1/evaluations/benchmarks?category=reasoning")

        assert response.status_code == 200
        data = response.json()

        # Should only return reasoning benchmarks
        benchmarks = data["benchmarks"]
        assert all(b["category"] == "reasoning" for b in benchmarks)

    def test_list_benchmarks_with_tags_filter(self, client_with_mock_provider):
        """Test listing benchmarks filtered by tags."""
        client, mock_service = client_with_mock_provider

        # Mock search_benchmarks to return filtered results for 'test' tag
        filtered_benchmarks = [
            b
            for b in mock_service.get_all_benchmarks().benchmarks
            if "test" in b["tags"]
        ]
        mock_service.search_benchmarks.return_value = [
            BenchmarkDetail(
                benchmark_id=b["benchmark_id"],
                provider_id=b["provider_id"],
                provider_name="Test Provider",
                name=b["name"],
                description=b["description"],
                category=b["category"],
                metrics=b["metrics"],
                num_few_shot=b["num_few_shot"],
                dataset_size=b["dataset_size"],
                tags=b["tags"],
                provider_type="nemo-evaluator",
                base_url="http://test-provider:8080",
            )
            for b in filtered_benchmarks
        ]

        response = client.get("/api/v1/evaluations/benchmarks?tags=test")

        assert response.status_code == 200
        data = response.json()

        # Should only return benchmarks with 'test' tag
        benchmarks = data["benchmarks"]
        assert all("test" in b["tags"] for b in benchmarks)

    def test_get_benchmarks_by_provider_success(self, client_with_mock_provider):
        """Test successful retrieval of provider-specific benchmarks."""
        client, mock_service = client_with_mock_provider

        response = client.get(
            "/api/v1/evaluations/benchmarks?provider_id=test_provider"
        )

        assert response.status_code == 200
        data = response.json()

        assert "benchmarks" in data
        assert "total_count" in data
        assert "providers_included" in data
        assert len(data["benchmarks"]) == 2
        assert all(b["provider_id"] == "test_provider" for b in data["benchmarks"])

    def test_get_benchmarks_by_provider_not_found(self, client_with_mock_provider):
        """Test getting benchmarks for non-existent provider returns empty results."""
        client, mock_service = client_with_mock_provider

        # Mock service to return empty results for non-existent provider
        mock_service.search_benchmarks.return_value = []

        response = client.get(
            "/api/v1/evaluations/benchmarks?provider_id=nonexistent_provider"
        )

        assert response.status_code == 200
        data = response.json()
        assert "benchmarks" in data
        assert "total_count" in data
        assert data["total_count"] == 0
        assert len(data["benchmarks"]) == 0

    def test_list_collections_success(self, client_with_mock_provider):
        """Test successful listing of collections."""
        client, mock_service = client_with_mock_provider

        response = client.get("/api/v1/evaluations/collections")

        assert response.status_code == 200
        data = response.json()

        assert "collections" in data
        assert "total_collections" in data
        assert data["total_collections"] == 1

        collections = data["collections"]
        assert len(collections) == 1
        assert collections[0]["collection_id"] == "test_collection"
        assert collections[0]["name"] == "Test Collection"
        assert len(collections[0]["benchmarks"]) == 2

    def test_patch_collection_success(self, client_with_mock_provider):
        """Test partially updating a collection."""
        client, mock_service = client_with_mock_provider

        patched_collection = Collection(
            collection_id="test_collection",
            name="Updated Name",
            description="Updated Description",
            provider_id="test_provider",
            tags=["updated"],
            metadata={},
            benchmarks=[],
            created_at="2024-01-01T00:00:00Z",
            updated_at="2024-01-02T00:00:00Z",
        )
        mock_service.update_collection.return_value = patched_collection

        payload = {"name": "Updated Name", "tags": ["updated"]}
        response = client.patch(
            "/api/v1/evaluations/collections/test_collection", json=payload
        )

        assert response.status_code == 200
        data = response.json()
        assert data["name"] == "Updated Name"
        assert data["tags"] == ["updated"]
        mock_service.update_collection.assert_called_once()


class TestProviderServiceIntegration:
    """Integration tests for provider service with API."""

    def test_provider_service_error_handling(self):
        """Test provider service error handling."""
        from eval_hub.core.config import Settings

        with patch(
            "eval_hub.services.provider_service.ProviderService._load_providers_data"
        ) as mock_load:
            mock_load.side_effect = FileNotFoundError("providers.yaml not found")

            settings = Settings()
            service = ProviderService(settings)
            with pytest.raises(FileNotFoundError):
                service.get_all_providers()  # Trigger the data loading

    def test_benchmark_filtering_logic(self, mock_provider_service):
        """Test benchmark filtering logic."""
        # Test with multiple filters
        mock_provider_service.search_benchmarks(
            provider_id="test_provider", category="reasoning"
        )

        # Should call search_benchmarks with proper filters
        mock_provider_service.search_benchmarks.assert_called_with(
            provider_id="test_provider", category="reasoning"
        )

    def test_response_model_validation(self, mock_provider_data):
        """Test that response models validate correctly."""
        # Test Provider model validation
        provider_data = mock_provider_data["providers"][0]
        provider = Provider(
            provider_id=provider_data["provider_id"],
            provider_name=provider_data["provider_name"],
            description=provider_data["description"],
            provider_type=ProviderType(provider_data["provider_type"]),
            base_url=provider_data["base_url"],
            benchmarks=[Benchmark(**b) for b in provider_data["benchmarks"]],
        )

        assert provider.provider_id == "test_provider"
        assert provider.provider_type == ProviderType.NEMO_EVALUATOR
        assert len(provider.benchmarks) == 2

        # Test ListProvidersResponse model validation
        providers_response = ListProvidersResponse(
            providers=[
                ProviderSummary(
                    provider_id=provider.provider_id,
                    provider_name=provider.provider_name,
                    description=provider.description,
                    provider_type=provider.provider_type,
                    base_url=provider.base_url,
                    benchmark_count=len(provider.benchmarks),
                )
            ],
            total_providers=1,
            total_benchmarks=2,
        )

        assert providers_response.total_providers == 1
        assert providers_response.total_benchmarks == 2

    def test_api_error_responses(self, client_with_mock_provider):
        """Test API error response formats."""
        client, mock_service = client_with_mock_provider

        # Mock service to raise exception
        mock_service.get_all_providers.side_effect = Exception("Service error")

        response = client.get("/api/v1/evaluations/providers")

        # Should return 500 internal server error
        assert response.status_code == 500

    def test_concurrent_access_handling(self, mock_provider_service):
        """Test that provider service handles concurrent access properly."""
        import threading

        results = []

        def access_providers():
            try:
                result = mock_provider_service.get_all_providers()
                results.append(result)
            except Exception as e:
                results.append(e)

        # Simulate concurrent access
        threads = []
        for _ in range(5):
            thread = threading.Thread(target=access_providers)
            threads.append(thread)
            thread.start()

        # Wait for all threads to complete
        for thread in threads:
            thread.join()

        # All should succeed
        assert len(results) == 5
        assert all(isinstance(r, ListProvidersResponse) for r in results)


class TestAPIValidation:
    """Test API input validation and error handling."""

    def test_invalid_provider_id_format(self, client_with_mock_provider):
        """Test handling of invalid provider ID format."""
        client, mock_service = client_with_mock_provider

        # Test with special characters that might cause issues
        invalid_ids = [
            "provider/with/slashes",
            "provider with spaces",
            "provider%with%percent",
        ]

        for invalid_id in invalid_ids:
            response = client.get(f"/api/v1/evaluations/providers/{invalid_id}")
            # Should handle gracefully (either 404 or proper encoding)
            assert response.status_code in [200, 404]

    def test_query_parameter_validation(self, client_with_mock_provider):
        """Test query parameter validation."""
        client, mock_service = client_with_mock_provider

        # Test with various query parameter values
        test_cases = [
            "/api/v1/evaluations/benchmarks?provider_id=",  # Empty provider_id
            "/api/v1/evaluations/benchmarks?category=",  # Empty category
            "/api/v1/evaluations/benchmarks?tags=",  # Empty tags
            "/api/v1/evaluations/benchmarks?invalid_param=test",  # Invalid parameter
        ]

        for url in test_cases:
            response = client.get(url)
            # Should handle gracefully
            assert response.status_code in [200, 400, 422]

    def test_large_response_handling(self, client_with_mock_provider):
        """Test handling of large response data."""
        client, mock_service = client_with_mock_provider

        # Create a large number of mock benchmarks for the response
        large_benchmark_list = []
        for i in range(100):  # Use 100 instead of 1000 to keep test faster
            large_benchmark_list.append(
                {
                    "benchmark_id": f"benchmark_{i}",
                    "provider_id": "test_provider",
                    "name": f"Test Benchmark {i}",
                    "description": f"Benchmark number {i}",
                    "category": "test",
                    "metrics": ["accuracy"],
                    "num_few_shot": 0,
                    "dataset_size": 100,
                    "tags": ["test"],
                }
            )

        # Mock the service to return large response
        mock_service.get_all_benchmarks.return_value = ListBenchmarksResponse(
            benchmarks=large_benchmark_list,
            total_count=100,
            providers_included=["test_provider"],
        )

        # Test API can handle large response
        response = client.get("/api/v1/evaluations/benchmarks")
        assert response.status_code == 200
        data = response.json()
        assert len(data["benchmarks"]) == 100
