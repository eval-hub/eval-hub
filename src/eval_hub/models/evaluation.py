"""Evaluation data models and schemas."""

from datetime import UTC, datetime
from enum import Enum
from typing import Any, Literal
from uuid import UUID, uuid4

from pydantic import AliasChoices, BaseModel, ConfigDict, Field, RootModel


class Model(BaseModel):
    """Model specification for evaluation requests."""

    model_config = ConfigDict(extra="forbid")

    url: str = Field(..., description="Model endpoint URL")
    name: str = Field(..., description="Model name/identifier")


def get_utc_now() -> datetime:
    """Get current UTC timestamp."""
    return datetime.now(UTC)


# ===== New Base Schemas for RESTful API =====


class Resource(BaseModel):
    """Resource"""

    model_config = ConfigDict(extra="forbid")

    id: str = Field(..., title="")
    created_at: datetime = Field(..., title="")
    updated_at: datetime = Field(..., title="")


class Page(BaseModel):
    """Generic pagination schema."""

    model_config = ConfigDict(
        extra="forbid",
        json_schema_extra={
            "type": "object",
            "properties": {
                "first": {
                    "type": "object",
                    "properties": {"href": {"type": "string"}},
                },
                "next": {
                    "type": "object",
                    "properties": {"href": {"type": "string"}},
                },
                "limit": {"type": "integer"},
                "total_count": {"type": "integer"},
            },
            "required": ["first", "limit", "total_count"],
        },
    )

    first: dict[str, str] = Field(..., description="Link to first page")
    next: dict[str, str] | None = Field(None, description="Link to next page")
    limit: int = Field(..., description="Page size limit")
    total_count: int = Field(..., description="Total number of items")


class Status(BaseModel):
    """Status"""

    model_config = ConfigDict(extra="forbid")

    state: Literal["pending", "running", "completed", "failed", "cancelled"] = Field(
        ..., title="", description="Current state"
    )
    message: str = Field(..., title="", description="Status message")
    benchmarks: list[dict[str, Any]] = Field(
        default_factory=list,
        json_schema_extra={
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "name": {"type": "string"},
                    "state": {
                        "type": "string",
                        "enum": [
                            "pending",
                            "running",
                            "completed",
                            "failed",
                            "cancelled",
                        ],
                    },
                    "started_at": {"type": "string", "format": "date-time"},
                    "completed_at": {"type": "string", "format": "date-time"},
                    "message": {"type": "string"},
                    "logs": {
                        "type": "object",
                        "properties": {"path": {"type": "string"}},
                    },
                },
                "additionalProperties": True,
            },
        },
    )


class System(BaseModel):
    """System information containing status and other system-level metadata."""

    model_config = ConfigDict(extra="allow")

    status: Status = Field(..., description="System status information")


class Error(BaseModel):
    """Error"""

    model_config = ConfigDict(extra="forbid")

    code: str | None = Field(default=None, title="")
    message: str | None = Field(default=None, title="Message")
    type: str | None = Field(default=None, title="Error Type")


class PatchOperation(BaseModel):
    """Single JSON Patch operation."""

    model_config = ConfigDict(extra="allow")

    op: str = Field(..., description="Operation type")
    path: str = Field(..., description="JSON path to operate on")
    value: Any = Field(None, description="Value for operation")


# Patch is an array of PatchOperation-like objects; keep schema inline to avoid extra components
class Patch(RootModel[list[dict[str, Any]]]):
    """Patch"""

    root: list[dict[str, Any]]

    model_config = ConfigDict(
        json_schema_extra={
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "op": {
                        "type": "string",
                        "enum": ["replace", "add", "remove"],
                    },
                    "path": {"type": "string"},
                    "value": {"type": "object"},
                },
            },
        },
    )

    def __iter__(self) -> Any:
        """Make Patch iterable like a list."""
        return iter(self.root)

    def __len__(self) -> int:
        """Get length of patch operations."""
        return len(self.root)

    def __getitem__(self, index: int) -> dict[str, Any]:
        """Get patch operation by index."""
        return self.root[index]


class RiskCategory(str, Enum):
    """Risk categories for evaluations."""

    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class BackendType(str, Enum):
    """Supported evaluation backends."""

    LMEVAL = "lm-evaluation-harness"
    GUIDELLM = "guidellm"
    NEMO_EVALUATOR = "nemo-evaluator"
    KFP = "kubeflow-pipelines"
    CUSTOM = "custom"


class ExperimentConfig(BaseModel):
    """Configuration for MLFlow experiment tracking."""

    model_config = ConfigDict(extra="forbid")

    name: str = Field(..., description="Experiment name for MLFlow tracking")
    tags: dict[str, str] = Field(
        default_factory=dict, description="Tags for the evaluation experiment"
    )


class EvaluationStatus(str, Enum):
    """Status of an evaluation."""

    PENDING = "pending"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"


class BenchmarkSpec(BaseModel):
    """Specification for a benchmark within an evaluation."""

    model_config = ConfigDict(extra="allow")

    name: str = Field(..., description="Benchmark name")
    tasks: list[str] = Field(..., description="List of tasks to evaluate")
    num_fewshot: int | None = Field(None, description="Number of few-shot examples")
    batch_size: int | None = Field(None, description="Batch size for evaluation")
    limit: int | None = Field(None, description="Limit number of examples")
    device: str | None = Field(None, description="Device to run evaluation on")
    config: dict[str, Any] = Field(
        default_factory=dict, description="Additional configuration"
    )


class BenchmarkConfig(BaseModel):
    """New simplified benchmark specification."""

    model_config = ConfigDict(extra="forbid")

    benchmark_id: str = Field(..., description="Benchmark identifier")
    provider_id: str = Field(..., description="Provider identifier")
    config: dict[str, Any] = Field(
        default_factory=dict,
        description="Benchmark configuration including num_fewshot, limit, batch_size, etc.",
    )


class BackendSpec(BaseModel):
    """Specification for an evaluation backend."""

    model_config = ConfigDict(extra="allow")

    name: str = Field(..., description="Backend name/identifier")
    type: BackendType = Field(..., description="Backend type")
    endpoint: str | None = Field(None, description="Backend API endpoint")
    config: dict[str, Any] = Field(
        default_factory=dict, description="Backend-specific configuration"
    )
    benchmarks: list[BenchmarkSpec] = Field(
        ..., description="Benchmarks to run on this backend"
    )


class EvaluationSpec(BaseModel):
    """Specification for a single evaluation request."""

    model_config = ConfigDict(extra="allow")

    id: UUID = Field(default_factory=uuid4, description="Unique evaluation ID")
    name: str = Field(..., description="Human-readable evaluation name")
    description: str | None = Field(None, description="Evaluation description")
    model: Model = Field(..., description="Model specification for evaluation")
    backends: list[BackendSpec] = Field(
        default_factory=list, description="Backends to run evaluations on"
    )
    risk_category: RiskCategory | None = Field(
        None, description="Risk category for automatic benchmark selection"
    )
    collection_id: str | None = Field(
        None, description="Collection ID for automatic benchmark selection"
    )
    priority: int = Field(
        default=0, description="Evaluation priority (higher = more urgent)"
    )
    timeout_minutes: int = Field(
        default=60, description="Timeout for the entire evaluation"
    )
    retry_attempts: int = Field(
        default=3, description="Number of retry attempts on failure"
    )
    metadata: dict[str, Any] = Field(
        default_factory=dict, description="Additional metadata"
    )

    @property
    def model_url(self) -> str:
        """Get model URL from model object."""
        return self.model.url

    @property
    def model_name(self) -> str:
        """Get model name from model object."""
        return self.model.name


class EvaluationRequest(BaseModel):
    """Request payload for starting evaluations."""

    model_config = ConfigDict(extra="allow")

    request_id: UUID = Field(default_factory=uuid4, description="Unique request ID")
    evaluations: list[EvaluationSpec] = Field(
        ..., description="List of evaluations to run"
    )
    experiment: ExperimentConfig | None = Field(
        None, description="Experiment configuration for MLFlow tracking (optional)"
    )
    async_mode: bool = Field(
        default=True, description="Whether to run evaluations asynchronously"
    )
    callback_url: str | None = Field(
        None, description="URL to call when evaluation completes"
    )
    created_at: datetime = Field(
        default_factory=get_utc_now, description="Request creation timestamp"
    )


# ===== New Request Schemas for Proposal API =====


class EvaluationJobBenchmarkConfig(BaseModel):
    """Benchmark configuration for EvaluationJobRequest."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    name: str | None = Field(
        None, description="Optional user-provided name for this benchmark"
    )
    id: str = Field(
        ...,
        description="Benchmark identifier",
        validation_alias=AliasChoices("id", "benchmark_id"),
    )
    provider_id: str = Field(
        ..., description="Provider identifier", alias="provider_id"
    )
    config: dict[str, Any] = Field(
        default_factory=dict,
        description="Benchmark configuration including num_fewshot, limit, batch_size, etc.",
    )


class EvaluationJobBenchmarkSpec(BaseModel):
    """Benchmark specification for EvaluationJobResource response."""

    model_config = ConfigDict(extra="forbid")

    name: str = Field(..., description="User-provided name for this benchmark")
    id: str = Field(..., description="Benchmark identifier")
    provider_id: str = Field(..., description="Provider identifier")
    config: dict[str, Any] = Field(
        default_factory=dict,
        description="Benchmark configuration including num_fewshot, limit, batch_size, etc.",
    )


class EvaluationJobRequest(BaseModel):
    """Simplified evaluation request using the new schema."""

    model_config = ConfigDict(extra="forbid")

    model: Model = Field(..., description="Model specification for evaluation")
    benchmarks: list[EvaluationJobBenchmarkConfig] = Field(
        ...,
        title="Benchmarks",
        description="List of benchmarks to evaluate",
    )
    experiment: ExperimentConfig | None = Field(
        None,
        description="Experiment configuration for MLFlow tracking (optional)",
    )
    timeout_minutes: int = Field(
        default=60,
        title="Timeout Minutes",
        description="Timeout for the entire evaluation",
    )
    retry_attempts: int = Field(
        default=3,
        title="Retry Attempts",
        description="Number of retry attempts on failure",
    )
    callback_url: str | None = Field(
        None,
        title="Callback Url",
        description="URL to call when evaluation completes",
        json_schema_extra={"anyOf": [{"type": "string"}, {"type": "null"}]},
    )


# ===== New Response Schemas for Proposal API =====


class EvaluationResultSummary(BaseModel):
    """Lightweight benchmark result for the proposal API."""

    model_config = ConfigDict(extra="allow")

    provider_id: str = Field(..., description="Provider identifier")
    id: str = Field(..., description="Benchmark identifier")
    status: str = Field(..., description="Result status")
    name: str | None = Field(None, description="Benchmark name")
    metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Evaluation metrics"
    )
    artifacts: dict[str, str] = Field(
        default_factory=dict, description="Paths to result artifacts"
    )
    mlflow_run_id: str | None = Field(None, description="MLFlow run ID")


class EvaluationJobBenchmarkResult(BaseModel):
    """Benchmark result for EvaluationJobResource - matches proposal structure."""

    model_config = ConfigDict(extra="forbid")

    name: str = Field(..., description="Benchmark name")
    metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Evaluation metrics"
    )
    artifacts: dict[str, str] = Field(
        default_factory=dict, description="Paths to result artifacts"
    )
    mlflow_run_id: str | None = Field(None, description="MLFlow run ID")


class EvaluationJobResults(BaseModel):
    """Results section for EvaluationJobResource."""

    model_config = ConfigDict(extra="forbid")

    total_evaluations: int = Field(..., description="Total number of evaluations")
    completed_evaluations: int = Field(
        default=0, description="Number of completed evaluations"
    )
    failed_evaluations: int = Field(
        default=0, description="Number of failed evaluations"
    )
    benchmarks: list[EvaluationJobBenchmarkResult] = Field(
        default_factory=list, description="Benchmark results"
    )
    aggregated_metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Aggregated metrics across all evaluations"
    )
    mlflow_experiment_url: str | None = Field(None, description="MLFlow experiment URL")


class EvaluationJobResource(BaseModel):
    """Evaluation job resource response schema matching the proposal."""

    model_config = ConfigDict(extra="allow")

    # Runtime validation fields
    resource: Resource = Field(..., description="Resource metadata")
    status: Status = Field(..., description="Current status")
    results: EvaluationJobResults | None = Field(None, description="Evaluation results")

    # Fields from EvaluationJobRequest
    model: Model = Field(..., description="Model specification")
    benchmarks: list[EvaluationJobBenchmarkSpec] = Field(
        ..., description="Benchmark configurations"
    )
    experiment: ExperimentConfig | None = Field(
        None, description="Experiment configuration (optional)"
    )
    timeout_minutes: int = Field(default=60, description="Timeout in minutes")
    retry_attempts: int = Field(default=3, description="Retry attempts")
    callback_url: str | None = Field(None, description="Callback URL")


class EvaluationJobResourceList(Page):
    """List of evaluation job resources with pagination."""

    model_config = ConfigDict(
        extra="allow",
        json_schema_extra={
            "allOf": [
                {"$ref": "#/components/schemas/Page"},
                {
                    "properties": {
                        "items": {
                            "type": "array",
                            "items": {
                                "$ref": "#/components/schemas/EvaluationJobResource"
                            },
                        }
                    }
                },
            ]
        },
    )

    # Items field
    items: list[EvaluationJobResource] = Field(
        ..., description="Evaluation job resources"
    )


class EvaluationResult(BaseModel):
    """Result of a single evaluation."""

    model_config = ConfigDict(extra="allow")

    evaluation_id: UUID | None = Field(
        None,
        description="Evaluation ID",
        exclude=True,
    )  # Internal tracking only
    provider_id: str = Field(..., description="Provider that ran the evaluation")
    benchmark_id: str = Field(..., description="Benchmark identifier")
    benchmark_name: str | None = Field(None, description="Benchmark display name")
    status: EvaluationStatus = Field(..., description="Evaluation status")
    metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Evaluation metrics"
    )
    artifacts: dict[str, str] = Field(
        default_factory=dict, description="Paths to result artifacts"
    )
    error_message: str | None = Field(None, description="Error message if failed")
    started_at: datetime | None = Field(None, description="Start timestamp")
    completed_at: datetime | None = Field(None, description="Completion timestamp")
    duration_seconds: float | None = Field(None, description="Evaluation duration")
    mlflow_run_id: str | None = Field(None, description="MLFlow run ID")


class RunStatus(BaseModel):
    """Status information for an individual benchmark run."""

    model_config = ConfigDict(extra="allow")

    name: str = Field(..., description="User-provided benchmark name")
    state: str = Field(..., description="Run state (pending, running, success, failed)")
    message: str = Field(default="", description="Optional status message")
    logs_path: str | None = Field(
        default=None, description="Optional path to run-specific logs"
    )


class SystemStatus(BaseModel):
    """Overall status for an evaluation request."""

    model_config = ConfigDict(extra="allow")

    state: str = Field(..., description="Overall state of the request")
    message: str = Field(default="", description="Optional status message")
    logs_path: str | None = Field(
        default=None, description="Optional path to system logs"
    )
    runs: list[RunStatus] = Field(
        default_factory=list, description="Status for each benchmark run"
    )


class SystemInfo(BaseModel):
    """Metadata about the evaluation resource."""

    model_config = ConfigDict(extra="allow")

    id: UUID = Field(..., description="Unique evaluation request ID")
    status: SystemStatus = Field(..., description="Current status of the request")
    created_at: datetime = Field(..., description="Creation timestamp")
    completed_at: datetime | None = Field(
        default=None, description="Completion timestamp"
    )


class BenchmarkResultPayload(BaseModel):
    """Result payload for a single benchmark."""

    model_config = ConfigDict(extra="allow")

    name: str = Field(..., description="Benchmark name")
    metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Benchmark metrics"
    )
    artifacts: dict[str, str] = Field(
        default_factory=dict, description="Benchmark artifacts"
    )
    mlflow_run_id: str | None = Field(None, description="MLFlow run ID")


class ResultsPayload(BaseModel):
    """Container for evaluation results."""

    model_config = ConfigDict(extra="allow")

    benchmarks: list[BenchmarkResultPayload] = Field(
        default_factory=list, description="List of benchmark results"
    )
    aggregated_metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Aggregated metrics across benchmarks"
    )
    mlflow_experiment_url: str | None = Field(
        None, description="Link to the MLFlow experiment"
    )


class EvaluationResponse(BaseModel):
    """Response payload for evaluation requests."""

    model_config = ConfigDict(extra="allow")

    system: SystemInfo = Field(..., description="System metadata for the request")
    results: ResultsPayload | None = Field(
        default=None, description="Results payload available upon success"
    )
    model: Model = Field(..., description="Model configuration provided by the user")
    benchmarks: list[BenchmarkConfig] = Field(
        ..., description="Benchmarks provided in the request"
    )
    experiment: ExperimentConfig | None = Field(
        None, description="Experiment configuration provided by the user (optional)"
    )
    timeout_minutes: int = Field(
        ..., description="Timeout for the entire evaluation (user-provided)"
    )
    retry_attempts: int = Field(
        ..., description="Number of retry attempts (user-provided)"
    )
    callback_url: str | None = Field(
        None, description="Callback URL provided by the user"
    )
    async_mode: bool = Field(
        ..., description="Whether the evaluation runs asynchronously"
    )
    custom: dict[str, Any] = Field(
        default_factory=dict,
        description="Custom fields supplied by the user in the request",
    )
    evaluation_results: list[EvaluationResult] = Field(
        default_factory=list,
        exclude=True,
        description="Raw evaluation results (internal use only)",
    )


class PaginatedEvaluations(BaseModel):
    """Paginated list response for evaluation resources."""

    model_config = ConfigDict(
        extra="allow",
        json_schema_extra={
            "properties": {
                "first": {
                    "type": "object",
                    "properties": {"href": {"type": "string"}},
                },
                "next": {
                    "type": "object",
                    "properties": {"href": {"type": "string"}},
                },
            }
        },
    )

    first: dict[str, str] = Field(..., description="Link to the first page")
    next: dict[str, str] | None = Field(
        default=None, description="Link to the next page, if available"
    )
    limit: int = Field(..., description="Page size used for this response")
    total_count: int = Field(..., description="Total number of evaluations")
    items: list[EvaluationResponse] = Field(
        default_factory=list, description="Evaluations returned for this page"
    )
