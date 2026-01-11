"""Provider service for managing evaluation providers and benchmarks."""

from pathlib import Path
from typing import Any

import yaml  # type: ignore[import-untyped]

from ..core.config import Settings
from ..core.logging import get_logger
from ..models.provider import (
    Benchmark,
    BenchmarkDetail,
    BenchmarksList,
    Collection,
    CollectionCreationRequest,
    CollectionUpdateRequest,
    ListCollectionsResponse,
    ProviderRecord,
    ProvidersData,
)

logger = get_logger(__name__)


class ProviderService:
    """Service for managing evaluation providers and benchmarks."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self._providers_data: ProvidersData | None = None
        self._providers_by_id: dict[str, ProviderRecord] = {}
        self._benchmarks_by_id: dict[str, BenchmarkDetail] = {}
        self._collections_by_id: dict[str, Collection] = {}

    def _get_providers_file_path(self) -> Path:
        """Get the path to the providers YAML file."""
        # Look for the file in the package data directory
        current_dir = Path(__file__).parent.parent
        providers_file = current_dir / "data" / "providers.yaml"

        if providers_file.exists():
            return providers_file

        # Fallback to a configurable path if needed
        fallback_path = Path("providers.yaml")
        if fallback_path.exists():
            return fallback_path

        raise FileNotFoundError(
            f"Providers configuration file not found at {providers_file}"
        )

    def _load_providers_data(self) -> ProvidersData:
        """Load providers data from YAML file."""
        if self._providers_data is not None:
            return self._providers_data

        try:
            providers_file = self._get_providers_file_path()

            with open(providers_file, encoding="utf-8") as f:
                yaml_data = yaml.safe_load(f)

            self._providers_data = ProvidersData(**yaml_data)
            self._build_lookup_tables()

            logger.info(
                "Loaded providers configuration",
                providers_count=len(self._providers_data.providers),
                collections_count=len(self._providers_data.collections),
                total_benchmarks=sum(
                    len(p.benchmarks) for p in self._providers_data.providers
                ),
            )

            return self._providers_data

        except FileNotFoundError as e:
            logger.error("Failed to load providers data", error=str(e))
            raise
        except Exception as e:
            logger.error("Failed to load providers data", error=str(e))
            raise

    def initialize(self) -> None:
        """Initialize and load providers data at startup."""
        self._load_providers_data()

    def _build_lookup_tables(self) -> None:
        """Build lookup tables for fast access to providers, benchmarks, and collections."""
        if not self._providers_data:
            return

        # Build providers lookup
        for provider in self._providers_data.providers:
            self._providers_by_id[provider.provider_id] = provider

            # Build benchmarks lookup with provider context
            for benchmark in provider.benchmarks:
                benchmark_detail = BenchmarkDetail(
                    benchmark_id=benchmark.benchmark_id,
                    provider_id=provider.provider_id,
                    name=benchmark.name,
                    description=benchmark.description,
                    category=benchmark.category,
                    metrics=benchmark.metrics,
                    num_few_shot=benchmark.num_few_shot,
                    dataset_size=benchmark.dataset_size,
                    tags=benchmark.tags,
                )

                # Use composite key for uniqueness
                composite_key = f"{provider.provider_id}::{benchmark.benchmark_id}"
                self._benchmarks_by_id[composite_key] = benchmark_detail

        # Build collections lookup
        for collection in self._providers_data.collections:
            self._collections_by_id[collection.collection_id] = collection

    def get_all_providers(self) -> list[ProviderRecord]:
        """Get all providers in public API format."""
        providers_data = self._load_providers_data()

        return providers_data.providers

    def get_provider_by_id(self, provider_id: str) -> ProviderRecord | None:
        """Get a provider by ID."""
        self._load_providers_data()
        provider = self._providers_by_id.get(provider_id)
        if not provider:
            return None
        return provider

    def get_all_benchmarks(self) -> BenchmarksList:
        """Get all benchmarks in public API format."""
        self._load_providers_data()

        benchmarks: list[Benchmark] = []
        providers_included = set()

        for benchmark_detail in self._benchmarks_by_id.values():
            benchmarks.append(
                Benchmark(
                    id=benchmark_detail.benchmark_id,
                    provider_id=benchmark_detail.provider_id,
                    label=benchmark_detail.name,
                    description=benchmark_detail.description,
                    category=benchmark_detail.category,
                    metrics=benchmark_detail.metrics,
                    num_few_shot=benchmark_detail.num_few_shot,
                    dataset_size=benchmark_detail.dataset_size,
                    tags=benchmark_detail.tags,
                )
            )
            providers_included.add(benchmark_detail.provider_id)

        return BenchmarksList(
            items=benchmarks,
            total_count=len(benchmarks),
        )

    def get_benchmarks_by_provider(self, provider_id: str) -> list[BenchmarkDetail]:
        """Get all benchmarks for a specific provider."""
        self._load_providers_data()

        benchmarks = []
        for _key, benchmark in self._benchmarks_by_id.items():
            if benchmark.provider_id == provider_id:
                benchmarks.append(benchmark)

        return benchmarks

    def get_benchmark_by_id(
        self, provider_id: str, benchmark_id: str
    ) -> BenchmarkDetail | None:
        """Get a specific benchmark by provider and benchmark ID."""
        self._load_providers_data()
        composite_key = f"{provider_id}::{benchmark_id}"
        return self._benchmarks_by_id.get(composite_key)

    def get_all_collections(self) -> ListCollectionsResponse:
        """Get all benchmark collections."""
        providers_data = self._load_providers_data()

        return ListCollectionsResponse(
            collections=providers_data.collections,
            total_collections=len(providers_data.collections),
        )

    def get_collection_by_id(self, collection_id: str) -> Collection | None:
        """Get a collection by ID."""
        self._load_providers_data()
        return self._collections_by_id.get(collection_id)

    def search_benchmarks(
        self,
        category: str | None = None,
        provider_id: str | None = None,
        tags: list[str] | None = None,
    ) -> list[BenchmarkDetail]:
        """Search benchmarks by various criteria."""
        self._load_providers_data()

        results = []
        for benchmark in self._benchmarks_by_id.values():
            # Filter by category
            if category and benchmark.category.lower() != category.lower():
                continue

            # Filter by provider
            if provider_id and benchmark.provider_id != provider_id:
                continue

            # Filter by tags
            if tags:
                benchmark_tags = [tag.lower() for tag in benchmark.tags]
                search_tags = [tag.lower() for tag in tags]
                if not any(tag in benchmark_tags for tag in search_tags):
                    continue

            results.append(benchmark)

        return results

    def reload_providers(self) -> None:
        """Reload providers data from file."""
        self._providers_data = None
        self._providers_by_id.clear()
        self._benchmarks_by_id.clear()
        self._collections_by_id.clear()
        self._load_providers_data()
        logger.info("Providers configuration reloaded")

    def create_collection(self, request: CollectionCreationRequest) -> Collection:
        """Create a new collection."""
        from datetime import datetime
        from uuid import uuid4

        self._load_providers_data()

        new_id = uuid4().hex

        # Note: Allow any benchmark ID, even if not in predefined list
        # Validation removed to support custom/external benchmarks

        # Create the collection
        now = datetime.utcnow().isoformat() + "Z"
        collection = Collection(
            id=new_id,
            name=request.name,
            description=request.description,
            tags=request.tags,
            metadata=request.custom,
            benchmarks=request.benchmarks,
            created_at=now,
            updated_at=now,
        )

        # Store in memory (in a real implementation, this would persist to storage)
        self._collections_by_id[collection.collection_id] = collection
        if self._providers_data:
            self._providers_data.collections.append(collection)

        logger.info(f"Created collection {collection.collection_id}")
        return collection

    def update_collection(
        self, collection_id: str, request: CollectionUpdateRequest
    ) -> Collection | None:
        """Update an existing collection."""
        from datetime import datetime

        self._load_providers_data()

        collection = self._collections_by_id.get(collection_id)
        if not collection:
            return None

        # Validate benchmarks if being updated
        # Note: Allow any benchmark ID, even if not in predefined list
        # Validation removed to support custom/external benchmarks

        # Update fields that are provided
        update_data: dict[str, Any] = {}
        if request.name is not None:
            update_data["name"] = request.name
        if request.description is not None:
            update_data["description"] = request.description
        if request.provider_id is not None:
            update_data["provider_id"] = request.provider_id
        if request.tags is not None:
            update_data["tags"] = request.tags
        if request.metadata is not None:
            update_data["metadata"] = request.metadata
        if request.benchmarks is not None:
            update_data["benchmarks"] = request.benchmarks

        # Update timestamp
        update_data["updated_at"] = datetime.utcnow().isoformat() + "Z"

        # Create updated collection
        updated_collection = collection.model_copy(update=update_data)

        # Store in memory
        self._collections_by_id[collection_id] = updated_collection

        # Update in providers data list
        if self._providers_data:
            for i, coll in enumerate(self._providers_data.collections):
                if coll.collection_id == collection_id:
                    self._providers_data.collections[i] = updated_collection
                    break

        logger.info(f"Updated collection {collection_id}")
        return updated_collection

    def delete_collection(self, collection_id: str) -> bool:
        """Delete a collection."""
        self._load_providers_data()

        if collection_id not in self._collections_by_id:
            return False

        # Remove from memory
        del self._collections_by_id[collection_id]

        # Remove from providers data list
        if self._providers_data:
            self._providers_data.collections = [
                coll
                for coll in self._providers_data.collections
                if coll.collection_id != collection_id
            ]

        logger.info(f"Deleted collection {collection_id}")
        return True
