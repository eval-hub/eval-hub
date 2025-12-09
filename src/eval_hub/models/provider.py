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

from .evaluation import Resource


class ProviderType(str, Enum):
    """Type of evaluation provider."""

    BUILTIN = "builtin"
    NEMO_EVALUATOR = "nemo-evaluator"


class BenchmarkRecord(BaseModel):
    """Internal benchmark schema used for provider configuration."""

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


class ProviderRecord(BaseModel):
    """Internal provider schema used for configuration."""

    model_config = ConfigDict(extra="allow")

    provider_id: str = Field(..., description="Unique provider identifier")
    provider_name: str = Field(..., description="Human-readable provider name")
    description: str = Field(..., description="Provider description")
    provider_type: ProviderType = Field(..., description="Type of provider")
    base_url: str | None = Field(
        default=None, description="Base URL for the provider API"
    )
    benchmarks: list[BenchmarkRecord] = Field(
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

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    provider_id: str = Field(
        ..., title="Provider Id", description="Provider identifier"
    )
    benchmark_id: str = Field(
        ..., title="Benchmark Id", description="Benchmark identifier", alias="id"
    )
    weight: float = Field(
        default=1.0,
        title="Weight",
        description="Weight for this benchmark in collection scoring",
    )
    config: dict[str, Any] = Field(
        default_factory=dict,
        title="Config",
        description="Benchmark-specific configuration",
    )


class Collection(BaseModel):
    """Collection of benchmarks for specific evaluation scenarios."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    collection_id: str = Field(
        ..., description="Unique collection identifier", alias="id"
    )
    name: str = Field(..., description="Human-readable collection name")
    description: str = Field(..., description="Collection description")
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

    providers: list[ProviderRecord] = Field(
        ..., description="List of evaluation providers"
    )
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

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    name: str = Field(..., title="Name", description="Human-readable collection name")
    description: str = Field(
        ..., title="Description", description="Collection description"
    )
    tags: list[str] = Field(
        default_factory=list,
        title="Tags",
        description="Tags for categorizing the collection",
    )
    custom: dict[str, Any] = Field(
        default_factory=dict,
        title="Custom",
        description="Additional collection metadata",
    )
    benchmarks: list[BenchmarkReference] = Field(
        ...,
        title="Benchmarks",
        description="List of benchmark references in this collection",
    )


class CollectionUpdateRequest(BaseModel):
    """Request for updating an existing collection."""

    model_config = ConfigDict(extra="allow")

    name: str | None = Field(
        default=None,
        description="Human-readable collection name",
        json_schema_extra={"anyOf": [{"type": "string"}, {"type": "null"}]},
    )
    description: str | None = Field(
        default=None,
        description="Collection description",
        json_schema_extra={"anyOf": [{"type": "string"}, {"type": "null"}]},
    )
    provider_id: str | None = Field(
        default=None,
        description="Primary provider for this collection",
        json_schema_extra={"anyOf": [{"type": "string"}, {"type": "null"}]},
    )
    tags: list[str] | None = Field(
        default=None,
        description="Tags for categorizing the collection",
        json_schema_extra={
            "anyOf": [{"type": "array", "items": {"type": "string"}}, {"type": "null"}]
        },
    )
    metadata: dict[str, Any] | None = Field(
        default=None,
        description="Additional collection metadata",
        json_schema_extra={
            "anyOf": [
                {"type": "object", "additionalProperties": True},
                {"type": "null"},
            ]
        },
    )
    benchmarks: list[BenchmarkReference] | None = Field(
        default=None,
        description="List of benchmark references in this collection",
        json_schema_extra={
            "anyOf": [
                {
                    "type": "array",
                    "items": {"$ref": "#/components/schemas/BenchmarkReference"},
                },
                {"type": "null"},
            ]
        },
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


class SupportedBenchmark(BaseModel):
    """Simplified benchmark reference for provider list response."""

    model_config = ConfigDict(extra="forbid")

    id: str = Field(..., description="Benchmark identifier")


class Provider(BaseModel):
    """Provider specification."""

    model_config = ConfigDict(extra="forbid")

    id: str = Field(..., description="Provider identifier")
    label: str = Field(..., description="Provider display name")
    supported_benchmarks: list[SupportedBenchmark] = Field(
        default_factory=list, description="Supported benchmarks"
    )


class ProviderList(BaseModel):
    """Response for listing providers."""

    model_config = ConfigDict(extra="forbid")

    total_count: int = Field(..., description="Total number of providers")
    items: list[Provider] = Field(..., description="List of providers")


class Benchmark(BaseModel):
    """Benchmark specification."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    benchmark_id: str = Field(
        ..., title="Benchmark Id", description="Unique benchmark identifier", alias="id"
    )
    provider_id: str = Field(..., description="The provider of this benchmark")
    name: str = Field(
        ..., title="Label", description="Human-readable benchmark name", alias="label"
    )
    description: str = Field(
        ..., title="Description", description="Benchmark description"
    )
    category: str = Field(..., title="Category", description="Benchmark category")
    metrics: list[str] = Field(
        ..., title="Metrics", description="List of metrics provided by this benchmark"
    )
    num_few_shot: int = Field(
        ..., title="Num Few Shot", description="Number of few-shot examples"
    )
    dataset_size: int | None = Field(
        None, title="Dataset Size", description="Size of the evaluation dataset"
    )
    tags: list[str] = Field(
        default_factory=list, title="Tags", description="Tags for categorization"
    )


class BenchmarksList(BaseModel):
    """Response for listing benchmarks."""

    model_config = ConfigDict(extra="forbid")

    total_count: int = Field(
        ..., title="Total Count", description="Total number of benchmarks"
    )
    items: list[Benchmark] = Field(
        ..., title="Benchmarks", description="List of all available benchmarks"
    )


class PaginationLink(BaseModel):
    """Pagination link with href field."""

    model_config = ConfigDict(extra="forbid")

    href: str = Field(..., description="Link URL")


class CollectionResource(BaseModel):
    """Collection resource."""

    model_config = ConfigDict(extra="forbid")

    # Resource metadata field
    resource: Resource = Field(..., description="Resource metadata")

    # Collection fields
    name: str = Field(..., description="Collection name")
    description: str = Field(..., description="Collection description")
    tags: list[str] = Field(default_factory=list, description="Collection tags")
    custom: dict[str, Any] = Field(default_factory=dict, description="Custom metadata")
    benchmarks: list[BenchmarkReference] = Field(
        default_factory=list, description="Collection benchmarks"
    )


class CollectionResourceList(BaseModel):
    """List of collection resources."""

    model_config = ConfigDict(extra="forbid")

    first: PaginationLink = Field(..., description="Link to first page")
    next: PaginationLink | None = Field(None, description="Link to next page")
    limit: int = Field(..., description="Page size limit")
    total_count: int = Field(..., description="Total number of items")
    items: list[CollectionResource] = Field(..., description="Collection resources")
