"""Evaluation data models and schemas."""

from datetime import UTC, datetime
from enum import Enum
from typing import Any
from uuid import UUID, uuid4

from pydantic import BaseModel, ConfigDict, Field


class Model(BaseModel):
    """Model specification for evaluation requests."""

    model_config = ConfigDict(extra="allow")

    url: str = Field(..., description="Model endpoint URL")
    name: str = Field(..., description="Model name/identifier")
    configuration: dict[str, Any] = Field(
        default_factory=dict, description="Model-specific configuration parameters"
    )


def get_utc_now() -> datetime:
    """Get current UTC timestamp."""
    return datetime.now(UTC)


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
    CUSTOM = "custom"


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

    model_config = ConfigDict(extra="allow")

    benchmark_id: str = Field(..., description="Benchmark identifier")
    provider_id: str = Field(..., description="Provider identifier")
    config: dict[str, Any] = Field(
        default_factory=dict,
        description="Benchmark configuration including num_fewshot, limit, batch_size, etc."
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

    @property
    def model_configuration(self) -> dict[str, Any]:
        """Get model configuration from model object."""
        return self.model.configuration


class SingleBenchmarkEvaluationRequest(BaseModel):
    """Simplified request for running a single benchmark evaluation."""

    model_config = ConfigDict(extra="allow")

    model: Model = Field(..., description="Model specification for evaluation")
    timeout_minutes: int = Field(default=60, description="Timeout for the evaluation")
    retry_attempts: int = Field(
        default=3, description="Number of retry attempts on failure"
    )
    limit: int | None = Field(None, description="Limit number of samples to evaluate")
    num_fewshot: int | None = Field(None, description="Number of few-shot examples")
    experiment_name: str | None = Field(None, description="MLFlow experiment name")
    tags: dict[str, str] = Field(
        default_factory=dict, description="Tags for the evaluation run"
    )

    @property
    def model_url(self) -> str:
        """Get model URL from model object."""
        return self.model.url

    @property
    def model_name(self) -> str:
        """Get model name from model object."""
        return self.model.name

    @property
    def model_configuration(self) -> dict[str, Any]:
        """Get model configuration from model object."""
        return self.model.configuration


class EvaluationRequest(BaseModel):
    """Request payload for starting evaluations."""

    model_config = ConfigDict(extra="allow")

    request_id: UUID = Field(default_factory=uuid4, description="Unique request ID")
    evaluations: list[EvaluationSpec] = Field(
        ..., description="List of evaluations to run"
    )
    experiment_name: str | None = Field(None, description="MLFlow experiment name")
    tags: dict[str, str] = Field(
        default_factory=dict, description="Tags for the evaluation run"
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


class SimpleEvaluationRequest(BaseModel):
    """Simplified evaluation request using the new schema."""

    model_config = ConfigDict(extra="allow")

    model: Model = Field(..., description="Model specification for evaluation")
    benchmarks: list[BenchmarkConfig] = Field(
        ..., description="List of benchmarks to evaluate"
    )
    experiment_name: str | None = Field(
        None, description="Name for the evaluation experiment (optional)"
    )
    tags: dict[str, str] = Field(
        default_factory=dict, description="Tags for the evaluation run"
    )
    timeout_minutes: int = Field(
        default=60, description="Timeout for the entire evaluation"
    )
    retry_attempts: int = Field(
        default=3, description="Number of retry attempts on failure"
    )
    request_id: UUID = Field(
        default_factory=uuid4, description="Unique request ID"
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


class EvaluationResult(BaseModel):
    """Result of a single evaluation."""

    model_config = ConfigDict(extra="allow")

    evaluation_id: UUID = Field(..., description="Evaluation ID")
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


class EvaluationResponse(BaseModel):
    """Response payload for evaluation requests."""

    model_config = ConfigDict(extra="allow")

    request_id: UUID = Field(..., description="Original request ID")
    status: EvaluationStatus = Field(..., description="Overall request status")
    total_evaluations: int = Field(..., description="Total number of evaluations")
    completed_evaluations: int = Field(
        default=0, description="Number of completed evaluations"
    )
    failed_evaluations: int = Field(
        default=0, description="Number of failed evaluations"
    )
    results: list[EvaluationResult] = Field(
        default_factory=list, description="Evaluation results"
    )
    aggregated_metrics: dict[str, float | int | str] = Field(
        default_factory=dict, description="Aggregated metrics across all evaluations"
    )
    experiment_url: str | None = Field(None, description="MLFlow experiment URL")
    created_at: datetime = Field(..., description="Request creation timestamp")
    updated_at: datetime = Field(
        default_factory=get_utc_now, description="Last update timestamp"
    )
    estimated_completion: datetime | None = Field(
        None, description="Estimated completion time"
    )
    progress_percentage: float = Field(
        default=0.0, description="Overall progress percentage"
    )
