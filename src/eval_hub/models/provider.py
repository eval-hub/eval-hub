"""Provider and benchmark data models."""

from collections.abc import Callable
from enum import Enum
from typing import Any

from pydantic import (
    BaseModel,
    ConfigDict,
    Field,
    SerializationInfo,
    ValidationInfo,
    field_validator,
    model_serializer,
)


class ProviderType(str, Enum):
    """Type of evaluation provider."""

    BUILTIN = "builtin"
    NEMO_EVALUATOR = "nemo-evaluator"


# BenchmarkCategory enum removed - using flexible string categories instead


class Benchmark(BaseModel):
    """Benchmark specification."""

    model_config = ConfigDict(extra="allow")

    benchmark_id: str = Field(..., description="Unique benchmark identifier")
    name: str = Field(..., description="Human-readable benchmark name")
    description: str = Field(..., description="Benchmark description")
    category: str = Field(..., description="Benchmark category")
    metrics: list[str] = Field(
        ..., description="List of metrics provided by this benchmark"
    )
    num_few_shot: int = Field(..., description="Number of few-shot examples")
    dataset_size: int | None = Field(None, description="Size of the evaluation dataset")
    tags: list[str] = Field(default_factory=list, description="Tags for categorization")


class Provider(BaseModel):
    """Evaluation provider specification."""

    model_config = ConfigDict(extra="allow")

    provider_id: str = Field(..., description="Unique provider identifier")
    provider_name: str = Field(..., description="Human-readable provider name")
    description: str = Field(..., description="Provider description")
    provider_type: ProviderType = Field(..., description="Type of provider")
    base_url: str | None = Field(
        default=None, description="Base URL for the provider API"
    )
    benchmarks: list[Benchmark] = Field(
        ..., description="List of benchmarks supported by this provider"
    )

    @field_validator("base_url")
    @classmethod
    def validate_base_url(cls, v: str | None, info: ValidationInfo) -> str | None:
        """Validate that base_url is provided for nemo-evaluator providers."""
        provider_type = info.data.get("provider_type")

        if provider_type == ProviderType.NEMO_EVALUATOR and v is None:
            raise ValueError("base_url is required for nemo-evaluator providers")

        return v

    @model_serializer(mode="wrap")
    def serialize_model(
        self,
        serializer: Callable[[BaseModel], dict[str, Any]],
        info: SerializationInfo,
    ) -> dict[str, Any]:
        """Custom serialization to exclude base_url for builtin providers when it's None."""
        data = serializer(self)
        if self.provider_type == ProviderType.BUILTIN and self.base_url is None:
            data.pop("base_url", None)
        return data


class BenchmarkReference(BaseModel):
    """Reference to a benchmark within a collection."""

    model_config = ConfigDict(extra="allow")

    provider_id: str = Field(..., description="Provider identifier")
    benchmark_id: str = Field(..., description="Benchmark identifier")
    weight: float = Field(
        default=1.0, description="Weight for this benchmark in collection scoring"
    )
    config: dict[str, Any] = Field(
        default_factory=dict, description="Benchmark-specific configuration"
    )


class Collection(BaseModel):
    """Collection of benchmarks for specific evaluation scenarios."""

    model_config = ConfigDict(extra="allow")

    collection_id: str = Field(..., description="Unique collection identifier")
    name: str = Field(..., description="Human-readable collection name")
    description: str = Field(..., description="Collection description")
    provider_id: str | None = Field(
        default=None, description="Primary provider for this collection"
    )
    tags: list[str] = Field(
        default_factory=list, description="Tags for categorizing the collection"
    )
    metadata: dict[str, Any] = Field(
        default_factory=dict, description="Additional collection metadata"
    )
    benchmarks: list[BenchmarkReference] = Field(
        ..., description="List of benchmark references in this collection"
    )
    created_at: str | None = Field(
        default=None, description="Collection creation timestamp"
    )
    updated_at: str | None = Field(
        default=None, description="Collection last update timestamp"
    )


class ProvidersData(BaseModel):
    """Complete providers configuration data."""

    model_config = ConfigDict(extra="allow")

    providers: list[Provider] = Field(..., description="List of evaluation providers")
    collections: list[Collection] = Field(
        ..., description="List of benchmark collections"
    )


class ProviderSummary(BaseModel):
    """Simplified provider information without benchmark details."""

    model_config = ConfigDict(extra="allow")

    provider_id: str = Field(..., description="Unique provider identifier")
    provider_name: str = Field(..., description="Human-readable provider name")
    description: str = Field(..., description="Provider description")
    provider_type: ProviderType = Field(..., description="Type of provider")
    base_url: str | None = Field(
        default=None, description="Base URL for the provider API"
    )
    benchmark_count: int = Field(
        ..., description="Number of benchmarks supported by this provider"
    )

    @model_serializer(mode="wrap")
    def serialize_model(
        self,
        serializer: Callable[[BaseModel], dict[str, Any]],
        info: SerializationInfo,
    ) -> dict[str, Any]:
        """Custom serialization to exclude base_url for builtin providers when it's None."""
        data = serializer(self)
        if self.provider_type == ProviderType.BUILTIN and self.base_url is None:
            data.pop("base_url", None)
        return data


class ListProvidersResponse(BaseModel):
    """Response for listing all providers."""

    model_config = ConfigDict(extra="allow")

    providers: list[ProviderSummary] = Field(
        ..., description="List of available providers"
    )
    total_providers: int = Field(..., description="Total number of providers")
    total_benchmarks: int = Field(
        ..., description="Total number of benchmarks across all providers"
    )


class ListBenchmarksResponse(BaseModel):
    """Response for listing all benchmarks (similar to Llama Stack format)."""

    model_config = ConfigDict(extra="allow")

    benchmarks: list[dict[str, Any]] = Field(
        ..., description="List of all available benchmarks"
    )
    total_count: int = Field(..., description="Total number of benchmarks")
    providers_included: list[str] = Field(
        ..., description="List of provider IDs included in the response"
    )


class ListCollectionsResponse(BaseModel):
    """Response for listing all collections."""

    model_config = ConfigDict(extra="allow")

    collections: list[Collection] = Field(
        ..., description="List of available collections"
    )
    total_collections: int = Field(..., description="Total number of collections")


class CollectionCreationRequest(BaseModel):
    """Request for creating a new collection."""

    model_config = ConfigDict(extra="allow")

    collection_id: str = Field(..., description="Unique collection identifier")
    name: str = Field(..., description="Human-readable collection name")
    description: str = Field(..., description="Collection description")
    provider_id: str | None = Field(
        default=None, description="Primary provider for this collection"
    )
    tags: list[str] = Field(
        default_factory=list, description="Tags for categorizing the collection"
    )
    metadata: dict[str, Any] = Field(
        default_factory=dict, description="Additional collection metadata"
    )
    benchmarks: list[BenchmarkReference] = Field(
        ..., description="List of benchmark references in this collection"
    )


class CollectionUpdateRequest(BaseModel):
    """Request for updating an existing collection."""

    model_config = ConfigDict(extra="allow")

    name: str | None = Field(default=None, description="Human-readable collection name")
    description: str | None = Field(default=None, description="Collection description")
    provider_id: str | None = Field(
        default=None, description="Primary provider for this collection"
    )
    tags: list[str] | None = Field(
        default=None, description="Tags for categorizing the collection"
    )
    metadata: dict[str, Any] | None = Field(
        default=None, description="Additional collection metadata"
    )
    benchmarks: list[BenchmarkReference] | None = Field(
        default=None, description="List of benchmark references in this collection"
    )


class BenchmarkDetail(BaseModel):
    """Detailed benchmark information for API responses."""

    model_config = ConfigDict(extra="allow")

    benchmark_id: str = Field(..., description="Unique benchmark identifier")
    provider_id: str = Field(..., description="Provider that owns this benchmark")
    name: str = Field(..., description="Human-readable benchmark name")
    description: str = Field(..., description="Benchmark description")
    category: str = Field(..., description="Benchmark category")
    metrics: list[str] = Field(
        ..., description="List of metrics provided by this benchmark"
    )
    num_few_shot: int = Field(..., description="Number of few-shot examples")
    dataset_size: int | None = Field(None, description="Size of the evaluation dataset")
    tags: list[str] = Field(default_factory=list, description="Tags for categorization")
