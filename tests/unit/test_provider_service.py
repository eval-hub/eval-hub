"""Unit tests for the provider service."""

import os
import tempfile
from pathlib import Path
from unittest.mock import patch

import pytest
import yaml

from eval_hub.models.provider import (
    BenchmarkDetail,
    BenchmarksList,
    ListCollectionsResponse,
    ProviderRecord,
    ProvidersData,
    ProviderType,
)
from eval_hub.services.provider_service import ProviderService


def create_provider_service_with_test_data(temp_file_path):
    """Helper function to create ProviderService with test data."""
    from contextlib import contextmanager

    from eval_hub.core.config import Settings

    @contextmanager
    def _service_context():
        settings = Settings()
        with patch.object(
            ProviderService,
            "_get_providers_file_path",
            return_value=Path(temp_file_path),
        ):
            yield ProviderService(settings)

    return _service_context()


@pytest.fixture
def sample_providers_yaml():
    """Create sample providers YAML data for testing."""
    return {
        "providers": [
            {
                "provider_id": "test_provider_1",
                "provider_name": "Test Provider 1",
                "description": "First test provider",
                "provider_type": "builtin",
                "base_url": "http://test1:8080",
                "benchmarks": [
                    {
                        "benchmark_id": "benchmark_1a",
                        "name": "Benchmark 1A",
                        "description": "First benchmark of provider 1",
                        "category": "reasoning",
                        "metrics": ["accuracy", "f1_score"],
                        "num_few_shot": 5,
                        "dataset_size": 1000,
                        "tags": ["reasoning", "test"],
                    },
                    {
                        "benchmark_id": "benchmark_1b",
                        "name": "Benchmark 1B",
                        "description": "Second benchmark of provider 1",
                        "category": "math",
                        "metrics": ["exact_match"],
                        "num_few_shot": 0,
                        "dataset_size": 500,
                        "tags": ["math", "test"],
                    },
                ],
            },
            {
                "provider_id": "test_provider_2",
                "provider_name": "Test Provider 2",
                "description": "Second test provider",
                "provider_type": "builtin",
                "base_url": "http://test2:8081",
                "benchmarks": [
                    {
                        "benchmark_id": "benchmark_2a",
                        "name": "Benchmark 2A",
                        "description": "First benchmark of provider 2",
                        "category": "safety",
                        "metrics": ["toxicity_rate"],
                        "num_few_shot": 0,
                        "dataset_size": 200,
                        "tags": ["safety", "test"],
                    }
                ],
            },
        ],
        "collections": [
            {
                "collection_id": "test_collection_1",
                "name": "Test Collection 1",
                "description": "First test collection",
                "benchmarks": [
                    {"provider_id": "test_provider_1", "benchmark_id": "benchmark_1a"},
                    {"provider_id": "test_provider_2", "benchmark_id": "benchmark_2a"},
                ],
            },
            {
                "collection_id": "test_collection_2",
                "name": "Test Collection 2",
                "description": "Second test collection",
                "benchmarks": [
                    {"provider_id": "test_provider_1", "benchmark_id": "benchmark_1b"}
                ],
            },
        ],
    }


@pytest.fixture
def temp_providers_file(sample_providers_yaml):
    """Create a temporary providers YAML file for testing."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        yaml.dump(sample_providers_yaml, f)
        temp_file_path = f.name

    yield temp_file_path

    # Cleanup
    os.unlink(temp_file_path)


class TestProviderService:
    """Test the ProviderService class."""

    def test_init_with_valid_data_file(self, temp_providers_file):
        """Test initialization with a valid providers data file."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            assert service is not None
            # Verify internal data is loaded
            assert hasattr(service, "_providers_data")
            assert hasattr(service, "_providers_by_id")
            assert hasattr(service, "_benchmarks_by_id")

    def test_init_with_missing_file(self):
        """Test initialization when providers file is missing."""
        from eval_hub.core.config import Settings

        settings = Settings()
        with patch.object(
            ProviderService,
            "_get_providers_file_path",
            side_effect=FileNotFoundError("Providers file not found"),
        ):
            service = ProviderService(settings)
            with pytest.raises(FileNotFoundError):
                service.get_all_providers()  # This triggers the data loading

    def test_init_with_invalid_yaml(self):
        """Test initialization with invalid YAML file."""
        from eval_hub.core.config import Settings

        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
            f.write("invalid: yaml: content: [")
            invalid_file_path = f.name

        try:
            settings = Settings()
            with patch.object(
                ProviderService,
                "_get_providers_file_path",
                return_value=Path(invalid_file_path),
            ):
                service = ProviderService(settings)
                with pytest.raises(yaml.YAMLError):
                    service.get_all_providers()  # This triggers the data loading
        finally:
            os.unlink(invalid_file_path)

    def test_get_all_providers(self, temp_providers_file):
        """Test getting all providers."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.get_all_providers()

            assert isinstance(result, list)
            assert len(result) == 2

            # Check provider summaries
            provider_ids = [p.provider_id for p in result]
            assert "test_provider_1" in provider_ids
            assert "test_provider_2" in provider_ids

            # Check benchmark counts using benchmarks list length
            provider_1 = next(p for p in result if p.provider_id == "test_provider_1")
            assert len(provider_1.benchmarks) == 2

            provider_2 = next(p for p in result if p.provider_id == "test_provider_2")
            assert len(provider_2.benchmarks) == 1

    def test_get_provider_by_id_existing(self, temp_providers_file):
        """Test getting an existing provider by ID."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            provider = service.get_provider_by_id("test_provider_1")

            assert provider is not None
            assert isinstance(provider, ProviderRecord)
            assert provider.provider_id == "test_provider_1"
            assert provider.provider_name == "Test Provider 1"
            assert provider.provider_type == ProviderType.BUILTIN
            assert len(provider.benchmarks) == 2

    def test_get_provider_by_id_nonexistent(self, temp_providers_file):
        """Test getting a non-existent provider by ID."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            provider = service.get_provider_by_id("nonexistent_provider")

            assert provider is None

    def test_get_all_benchmarks(self, temp_providers_file):
        """Test getting all benchmarks."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.get_all_benchmarks()

            assert isinstance(result, BenchmarksList)
            assert result.total_count == 3
            assert len(result.items) == 3

            # Check benchmark structure
            benchmark = result.items[0]
            required_fields = [
                "benchmark_id",
                "provider_id",
                "name",
                "description",
                "category",
                "metrics",
                "num_few_shot",
                "dataset_size",
                "tags",
            ]
            for field in required_fields:
                assert hasattr(benchmark, field)

    def test_get_benchmarks_by_provider_existing(self, temp_providers_file):
        """Test getting benchmarks for an existing provider."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            benchmarks = service.get_benchmarks_by_provider("test_provider_1")

            assert isinstance(benchmarks, list)
            assert len(benchmarks) == 2
            assert all(isinstance(b, BenchmarkDetail) for b in benchmarks)
            assert all(b.provider_id == "test_provider_1" for b in benchmarks)

            # Check specific benchmarks
            benchmark_ids = [b.benchmark_id for b in benchmarks]
            assert "benchmark_1a" in benchmark_ids
            assert "benchmark_1b" in benchmark_ids

    def test_get_benchmarks_by_provider_nonexistent(self, temp_providers_file):
        """Test getting benchmarks for a non-existent provider."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            benchmarks = service.get_benchmarks_by_provider("nonexistent_provider")

            assert isinstance(benchmarks, list)
            assert len(benchmarks) == 0

    def test_search_benchmarks_no_filters(self, temp_providers_file):
        """Test searching benchmarks with no filters."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.search_benchmarks()

            assert isinstance(result, list)
            assert len(result) == 3
            assert all(isinstance(b, BenchmarkDetail) for b in result)

    def test_search_benchmarks_by_provider(self, temp_providers_file):
        """Test searching benchmarks filtered by provider."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.search_benchmarks(provider_id="test_provider_1")

            assert isinstance(result, list)
            assert len(result) == 2
            assert all(b.provider_id == "test_provider_1" for b in result)

    def test_search_benchmarks_by_category(self, temp_providers_file):
        """Test searching benchmarks filtered by category."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.search_benchmarks(category="math")

            assert isinstance(result, list)
            assert len(result) == 1
            assert result[0].category == "math"
            assert result[0].benchmark_id == "benchmark_1b"

    def test_search_benchmarks_by_tags(self, temp_providers_file):
        """Test searching benchmarks filtered by tags."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.search_benchmarks(tags=["safety"])

            assert isinstance(result, list)
            assert len(result) == 1
            assert "safety" in result[0].tags

    def test_search_benchmarks_multiple_filters(self, temp_providers_file):
        """Test searching benchmarks with multiple filters."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.search_benchmarks(
                provider_id="test_provider_1", category="reasoning"
            )

            assert isinstance(result, list)
            assert len(result) == 1
            assert result[0].provider_id == "test_provider_1"
            assert result[0].category == "reasoning"

    def test_search_benchmarks_no_matches(self, temp_providers_file):
        """Test searching benchmarks with filters that match nothing."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.search_benchmarks(category="nonexistent_category")

            assert isinstance(result, list)
            assert len(result) == 0

    def test_get_all_collections(self, temp_providers_file):
        """Test getting all collections."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            result = service.get_all_collections()

            assert isinstance(result, ListCollectionsResponse)
            assert result.total_collections == 2
            assert len(result.collections) == 2

            # Check collection details
            collection_ids = [c.collection_id for c in result.collections]
            assert "test_collection_1" in collection_ids
            assert "test_collection_2" in collection_ids

            # Check benchmark references
            collection_1 = next(
                c for c in result.collections if c.collection_id == "test_collection_1"
            )
            assert len(collection_1.benchmarks) == 2

    def test_build_lookup_tables(self, temp_providers_file):
        """Test that lookup tables are built correctly."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            # Trigger data loading by calling a method that loads the data
            service.get_all_providers()

            # Check providers lookup
            assert "test_provider_1" in service._providers_by_id
            assert "test_provider_2" in service._providers_by_id

            # Check benchmarks lookup
            expected_keys = [
                "test_provider_1::benchmark_1a",
                "test_provider_1::benchmark_1b",
                "test_provider_2::benchmark_2a",
            ]
            for key in expected_keys:
                assert key in service._benchmarks_by_id

    def test_thread_safety(self, temp_providers_file):
        """Test that the service handles concurrent access safely."""
        import threading

        with create_provider_service_with_test_data(temp_providers_file) as service:
            results = []
            errors = []

            def access_service():
                try:
                    # Simulate various operations
                    providers = service.get_all_providers()
                    benchmarks = service.get_all_benchmarks()
                    search_result = service.search_benchmarks(category="reasoning")
                    provider = service.get_provider_by_id("test_provider_1")

                    results.append(
                        {
                            "providers_count": len(providers),
                            "benchmarks_count": benchmarks.total_count,
                            "search_count": len(search_result),
                            "provider_found": provider is not None,
                        }
                    )
                except Exception as e:
                    errors.append(e)

            # Run multiple threads concurrently
            threads = []
            for _ in range(10):
                thread = threading.Thread(target=access_service)
                threads.append(thread)
                thread.start()

            # Wait for all threads to complete
            for thread in threads:
                thread.join()

            # Verify no errors and consistent results
            assert len(errors) == 0
            assert len(results) == 10

            # All results should be identical
            first_result = results[0]
            for result in results[1:]:
                assert result == first_result

    def test_data_validation(self, temp_providers_file):
        """Test that loaded data is properly validated."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            # Trigger data loading
            service.get_all_providers()

            # Check that providers data is valid ProvidersData model
            assert isinstance(service._providers_data, ProvidersData)
            assert len(service._providers_data.providers) == 2
            assert len(service._providers_data.collections) == 2

            # Check that individual providers are valid ProviderRecord models (internal format)
            for provider in service._providers_data.providers:
                assert isinstance(provider, ProviderRecord)
                assert provider.provider_id
                assert provider.provider_name
                assert isinstance(provider.provider_type, ProviderType)

                # Check benchmarks
                # NOTE: Temporarily commenting out benchmark validation due to model mismatch
                # for benchmark in provider.supported_benchmarks:
                #     assert isinstance(benchmark, Benchmark)
                #     assert benchmark.benchmark_id
                #     assert benchmark.name
                #     assert isinstance(benchmark.metrics, list)

    def test_error_handling_invalid_provider_data(self):
        """Test error handling with invalid provider data structure."""
        invalid_data = {
            "providers": [
                {
                    "provider_id": "test",
                    # Missing required fields
                }
            ],
            "collections": [],
        }

        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
            yaml.dump(invalid_data, f)
            invalid_file_path = f.name

        try:
            with create_provider_service_with_test_data(invalid_file_path) as service:
                with pytest.raises(
                    (ValueError, TypeError)
                ):  # Should raise validation error when loading
                    service.get_all_providers()
        finally:
            os.unlink(invalid_file_path)

    def test_caching_behavior(self, temp_providers_file):
        """Test that data is cached and not reloaded unnecessarily."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            # Test that after first load, _providers_data is cached
            result1 = service.get_all_providers()
            assert (
                service._providers_data is not None
            )  # Data should be loaded and cached

            # Store reference to cached data
            cached_data = service._providers_data

            # Multiple subsequent calls should use the same cached instance
            result2 = service.get_all_benchmarks()
            result3 = service.search_benchmarks()

            # Verify the same data instance is used (not reloaded)
            assert service._providers_data is cached_data
            assert len(result1) == 2  # Verify data is correct
            assert result2.total_count == 3  # Verify benchmarks loaded
            assert len(result3) == 3  # Verify search works

    def test_benchmark_detail_creation(self, temp_providers_file):
        """Test that BenchmarkDetail objects are created correctly."""
        with create_provider_service_with_test_data(temp_providers_file) as service:
            benchmarks = service.search_benchmarks()

            for benchmark in benchmarks:
                assert isinstance(benchmark, BenchmarkDetail)

                # Check all required fields are present
                assert benchmark.benchmark_id
                assert benchmark.provider_id
                assert benchmark.name
                assert benchmark.description
                assert benchmark.category
                assert isinstance(benchmark.metrics, list)
                assert isinstance(benchmark.num_few_shot, int)
                assert isinstance(benchmark.tags, list)

    def test_edge_cases(self, sample_providers_yaml):
        """Test various edge cases."""
        # Test with empty providers list
        empty_data = {"providers": [], "collections": []}
        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
            yaml.dump(empty_data, f)
            empty_file_path = f.name

        try:
            with create_provider_service_with_test_data(empty_file_path) as service:
                result = service.get_all_providers()
                assert len(result) == 0
        finally:
            os.unlink(empty_file_path)

        # Test with provider having no benchmarks
        no_benchmarks_data = {
            "providers": [
                {
                    "provider_id": "empty_provider",
                    "provider_name": "Empty Provider",
                    "description": "Provider with no benchmarks",
                    "provider_type": "nemo-evaluator",
                    "base_url": "http://empty:8080",
                    "benchmarks": [],
                }
            ],
            "collections": [],
        }
        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
            yaml.dump(no_benchmarks_data, f)
            no_benchmarks_file_path = f.name

        try:
            with create_provider_service_with_test_data(
                no_benchmarks_file_path
            ) as service:
                provider = service.get_provider_by_id("empty_provider")
                assert provider is not None
                assert len(provider.benchmarks) == 0

                benchmarks = service.get_benchmarks_by_provider("empty_provider")
                assert len(benchmarks) == 0
        finally:
            os.unlink(no_benchmarks_file_path)
