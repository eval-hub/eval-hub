"""Health check models."""

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class HealthResponse(BaseModel):
    """Health check response."""

    status: str = Field(..., title="Status", description="Overall health status")
    version: str = Field(..., title="Version", description="Service version")
    timestamp: datetime = Field(
        default_factory=datetime.utcnow,
        title="Timestamp",
        description="Health check timestamp",
    )
    components: dict[str, dict[str, Any]] = Field(
        default_factory=dict,
        title="Components",
        description="Health status of individual components",
    )
    uptime_seconds: float = Field(
        ..., title="Uptime Seconds", description="Service uptime in seconds"
    )
    active_evaluations: int = Field(
        default=0,
        title="Active Evaluations",
        description="Number of active evaluations",
    )
