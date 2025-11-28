"""Shared test configuration and fixtures."""

import os
import tempfile
from unittest.mock import Mock, patch

import pytest
import yaml

from eval_hub.models.provider import (
    Benchmark,
    BenchmarkDetail,
    Collection,
    ListBenchmarksResponse,
    ListCollectionsResponse,
    ListProvidersResponse,
    Provider,
    ProviderType,
)
from eval_hub.services.provider_service import ProviderService


@pytest.fixture
def sample_benchmark_data():
    """Sample benchmark data for testing."""
    return {
        "benchmark_id": "test_benchmark",
        "name": "Test Benchmark",
        "description": "A benchmark for testing",
        "category": "test",
        "metrics": ["accuracy", "f1_score"],
        "num_few_shot": 5,
        "dataset_size": 1000,
        "tags": ["test", "sample"],
    }


@pytest.fixture
def sample_provider_data(sample_benchmark_data):
    """Sample provider data for testing."""
    return {
        "provider_id": "test_provider",
        "provider_name": "Test Provider",
        "description": "A provider for testing",
        "provider_type": "external",
        "base_url": "http://test:8080",
        "benchmarks": [sample_benchmark_data],
    }


@pytest.fixture
def minimal_providers_yaml():
    """Minimal providers YAML configuration for testing."""
    return {
        "providers": [
            {
                "provider_id": "minimal_provider",
                "provider_name": "Minimal Provider",
                "description": "Minimal provider for basic testing",
                "provider_type": "external",
                "base_url": "http://minimal:8080",
                "benchmarks": [
                    {
                        "benchmark_id": "minimal_benchmark",
                        "name": "Minimal Benchmark",
                        "description": "Minimal benchmark for testing",
                        "category": "test",
                        "metrics": ["accuracy"],
                        "num_few_shot": 0,
                        "dataset_size": 100,
                        "tags": ["test"],
                    }
                ],
            }
        ],
        "collections": [
            {
                "collection_id": "minimal_collection",
                "name": "Minimal Collection",
                "description": "Minimal collection for testing",
                "benchmarks": [
                    {
                        "provider_id": "minimal_provider",
                        "benchmark_id": "minimal_benchmark",
                    }
                ],
            }
        ],
    }


@pytest.fixture
def comprehensive_providers_yaml():
    """Comprehensive providers YAML configuration for thorough testing."""
    return {
        "providers": [
            {
                "provider_id": "lm_evaluation_harness",
                "provider_name": "LM Evaluation Harness",
                "description": "Language model evaluation framework",
                "provider_type": "external",
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
                    },
                    {
                        "benchmark_id": "gsm8k",
                        "name": "GSM8K",
                        "description": "Grade School Math 8K",
                        "category": "math",
                        "metrics": ["exact_match"],
                        "num_few_shot": 5,
                        "dataset_size": 1319,
                        "tags": ["math", "arithmetic"],
                    },
                ],
            },
            {
                "provider_id": "custom_provider",
                "provider_name": "Custom Provider",
                "description": "Custom evaluation provider",
                "provider_type": "internal",
                "base_url": "http://localhost:8323",
                "benchmarks": [
                    {
                        "benchmark_id": "custom_task",
                        "name": "Custom Task",
                        "description": "Custom evaluation task",
                        "category": "domain_specific",
                        "metrics": ["custom_score"],
                        "num_few_shot": 0,
                        "dataset_size": 500,
                        "tags": ["custom", "domain"],
                    }
                ],
            },
        ],
        "collections": [
            {
                "collection_id": "general_eval",
                "name": "General Evaluation",
                "description": "General purpose evaluation suite",
                "benchmarks": [
                    {
                        "provider_id": "lm_evaluation_harness",
                        "benchmark_id": "hellaswag",
                    },
                    {"provider_id": "lm_evaluation_harness", "benchmark_id": "gsm8k"},
                ],
            },
            {
                "collection_id": "custom_eval",
                "name": "Custom Evaluation",
                "description": "Custom evaluation suite",
                "benchmarks": [
                    {"provider_id": "custom_provider", "benchmark_id": "custom_task"}
                ],
            },
        ],
    }


@pytest.fixture
def temp_yaml_file():
    """Create a temporary YAML file for testing."""

    def _create_temp_file(data):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
            yaml.dump(data, f)
            return f.name

    created_files = []

    def create_file(data):
        file_path = _create_temp_file(data)
        created_files.append(file_path)
        return file_path

    yield create_file

    # Cleanup
    for file_path in created_files:
        if os.path.exists(file_path):
            os.unlink(file_path)


@pytest.fixture
def mock_provider_service():
    """Create a mock provider service for testing."""
    service = Mock(spec=ProviderService)

    # Default mock responses
    service.get_all_providers.return_value = ListProvidersResponse(
        providers=[], total_providers=0, total_benchmarks=0
    )

    service.get_all_benchmarks.return_value = ListBenchmarksResponse(
        benchmarks=[], total_count=0, providers_included=[]
    )

    service.get_all_collections.return_value = ListCollectionsResponse(
        collections=[], total_collections=0
    )

    service.get_provider_by_id.return_value = None
    service.get_benchmarks_by_provider.return_value = []
    service.search_benchmarks.return_value = []

    return service


@pytest.fixture
def provider_service_with_data(comprehensive_providers_yaml, temp_yaml_file):
    """Create a real provider service with test data."""
    yaml_file = temp_yaml_file(comprehensive_providers_yaml)

    with patch("eval_hub.services.provider_service.PROVIDERS_DATA_PATH", yaml_file):
        service = ProviderService()
        yield service


class TestDataFactory:
    """Factory for creating test data objects."""

    @staticmethod
    def create_benchmark(
        benchmark_id="test_benchmark",
        name="Test Benchmark",
        category="test",
        provider_id="test_provider",
        **kwargs,
    ):
        """Create a test benchmark."""
        defaults = {
            "description": f"{name} description",
            "metrics": ["accuracy"],
            "num_few_shot": 0,
            "dataset_size": 100,
            "tags": ["test"],
        }
        defaults.update(kwargs)

        return Benchmark(
            benchmark_id=benchmark_id, name=name, category=category, **defaults
        )

    @staticmethod
    def create_provider(
        provider_id="test_provider",
        provider_name="Test Provider",
        benchmarks=None,
        **kwargs,
    ):
        """Create a test provider."""
        if benchmarks is None:
            benchmarks = [TestDataFactory.create_benchmark()]

        defaults = {
            "description": f"{provider_name} description",
            "provider_type": ProviderType.EXTERNAL,
            "base_url": "http://test:8080",
        }
        defaults.update(kwargs)

        return Provider(
            provider_id=provider_id,
            provider_name=provider_name,
            benchmarks=benchmarks,
            **defaults,
        )

    @staticmethod
    def create_benchmark_detail(
        benchmark_id="test_benchmark", provider_id="test_provider", **kwargs
    ):
        """Create a test benchmark detail."""
        defaults = {
            "provider_name": "Test Provider",
            "name": "Test Benchmark",
            "description": "Test benchmark description",
            "category": "test",
            "metrics": ["accuracy"],
            "num_few_shot": 0,
            "dataset_size": 100,
            "tags": ["test"],
            "provider_type": "external",
            "base_url": "http://test:8080",
        }
        defaults.update(kwargs)

        return BenchmarkDetail(
            benchmark_id=benchmark_id, provider_id=provider_id, **defaults
        )

    @staticmethod
    def create_collection(
        collection_id="test_collection",
        name="Test Collection",
        benchmarks=None,
        **kwargs,
    ):
        """Create a test collection."""
        if benchmarks is None:
            benchmarks = [
                {"provider_id": "test_provider", "benchmark_id": "test_benchmark"}
            ]

        defaults = {"description": f"{name} description"}
        defaults.update(kwargs)

        return Collection(
            collection_id=collection_id, name=name, benchmarks=benchmarks, **defaults
        )


@pytest.fixture
def test_data_factory():
    """Provide the test data factory."""
    return TestDataFactory


@pytest.fixture(autouse=True)
def cleanup_temp_files():
    """Automatically cleanup temporary files after each test."""
    created_files = []

    def track_file(file_path):
        created_files.append(file_path)
        return file_path

    # Make the tracker available to tests
    pytest.temp_file_tracker = track_file

    yield

    # Cleanup
    for file_path in created_files:
        if os.path.exists(file_path):
            try:
                os.unlink(file_path)
            except OSError:
                pass  # Ignore cleanup errors


# Test markers for categorizing tests
def pytest_configure(config):
    """Configure pytest with custom markers."""
    config.addinivalue_line("markers", "unit: mark test as a unit test")
    config.addinivalue_line("markers", "integration: mark test as an integration test")
    config.addinivalue_line("markers", "slow: mark test as slow running")
    config.addinivalue_line("markers", "api: mark test as an API test")
    config.addinivalue_line("markers", "service: mark test as a service test")
    config.addinivalue_line("markers", "provider: mark test as provider-related")


# Pytest collection customization
def pytest_collection_modifyitems(config, items):
    """Modify test collection to add markers automatically."""
    for item in items:
        # Auto-mark tests based on file location
        if "unit" in str(item.fspath):
            item.add_marker(pytest.mark.unit)
        elif "integration" in str(item.fspath):
            item.add_marker(pytest.mark.integration)

        # Auto-mark tests based on name patterns
        if "provider" in item.name.lower():
            item.add_marker(pytest.mark.provider)
        if "api" in item.name.lower():
            item.add_marker(pytest.mark.api)
        if "service" in item.name.lower():
            item.add_marker(pytest.mark.service)


# Custom test utilities
class TestUtils:
    """Utility functions for testing."""

    @staticmethod
    def assert_valid_provider_response(response_data):
        """Assert that a provider response has the correct structure."""
        required_fields = ["providers", "total_providers", "total_benchmarks"]
        for field in required_fields:
            assert field in response_data, f"Missing field: {field}"

        assert isinstance(response_data["providers"], list)
        assert isinstance(response_data["total_providers"], int)
        assert isinstance(response_data["total_benchmarks"], int)

        for provider in response_data["providers"]:
            assert "provider_id" in provider
            assert "provider_name" in provider
            assert "description" in provider
            assert "provider_type" in provider
            assert "base_url" in provider
            assert "benchmark_count" in provider

    @staticmethod
    def assert_valid_benchmark_response(response_data):
        """Assert that a benchmark response has the correct structure."""
        required_fields = ["benchmarks", "total_count", "providers_included"]
        for field in required_fields:
            assert field in response_data, f"Missing field: {field}"

        assert isinstance(response_data["benchmarks"], list)
        assert isinstance(response_data["total_count"], int)
        assert isinstance(response_data["providers_included"], list)

        for benchmark in response_data["benchmarks"]:
            assert "benchmark_id" in benchmark
            assert "provider_id" in benchmark
            assert "name" in benchmark
            assert "description" in benchmark
            assert "category" in benchmark
            assert "metrics" in benchmark
            assert "tags" in benchmark

    @staticmethod
    def assert_valid_collection_response(response_data):
        """Assert that a collection response has the correct structure."""
        required_fields = ["collections", "total_collections"]
        for field in required_fields:
            assert field in response_data, f"Missing field: {field}"

        assert isinstance(response_data["collections"], list)
        assert isinstance(response_data["total_collections"], int)

        for collection in response_data["collections"]:
            assert "collection_id" in collection
            assert "name" in collection
            assert "description" in collection
            assert "benchmarks" in collection


@pytest.fixture
def test_utils():
    """Provide test utilities."""
    return TestUtils


@pytest.fixture
def live_server():
    """Start a live HTTP server for integration testing."""
    import socket
    import threading

    import uvicorn

    from eval_hub.api.app import create_app

    # Find an available port
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()

    app = create_app()
    server_config = uvicorn.Config(app, host="127.0.0.1", port=port, log_level="error")
    server = uvicorn.Server(server_config)

    # Start server in a separate thread
    thread = threading.Thread(target=server.run, daemon=True)
    thread.start()

    # Wait for server to be ready
    import time

    max_attempts = 30
    for _ in range(max_attempts):
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(1)
            result = sock.connect_ex(("127.0.0.1", port))
            sock.close()
            if result == 0:
                break
        except Exception:
            pass
        time.sleep(0.1)
    else:
        raise RuntimeError("Server failed to start")

    base_url = f"http://127.0.0.1:{port}"

    yield base_url

    # Server will be stopped when thread exits (daemon=True)
