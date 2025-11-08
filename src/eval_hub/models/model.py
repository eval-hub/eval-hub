"""Model data models for language model registration and management."""

from datetime import datetime
from enum import Enum
from typing import Any, Dict, List, Optional, Union
from pydantic import BaseModel, Field, ConfigDict, field_validator, HttpUrl


class ModelType(str, Enum):
    """Type of language model."""

    OPENAI = "openai"
    ANTHROPIC = "anthropic"
    HUGGINGFACE = "huggingface"
    OLLAMA = "ollama"
    VLLM = "vllm"
    OPENAI_COMPATIBLE = "openai-compatible"
    CUSTOM = "custom"


class ModelStatus(str, Enum):
    """Status of a registered model."""

    ACTIVE = "active"
    INACTIVE = "inactive"
    TESTING = "testing"
    DEPRECATED = "deprecated"


class ModelCapabilities(BaseModel):
    """Model capabilities and limitations."""

    model_config = ConfigDict(extra="allow")

    max_tokens: Optional[int] = Field(None, description="Maximum tokens supported by the model")
    supports_streaming: bool = Field(default=False, description="Whether the model supports streaming responses")
    supports_function_calling: bool = Field(default=False, description="Whether the model supports function calling")
    supports_vision: bool = Field(default=False, description="Whether the model supports vision/image inputs")
    context_window: Optional[int] = Field(None, description="Model's context window size")


class ModelConfig(BaseModel):
    """Model configuration parameters."""

    model_config = ConfigDict(extra="allow")

    temperature: Optional[float] = Field(None, ge=0.0, le=2.0, description="Default temperature setting")
    max_tokens: Optional[int] = Field(None, gt=0, description="Default max tokens for responses")
    top_p: Optional[float] = Field(None, ge=0.0, le=1.0, description="Default top_p setting")
    frequency_penalty: Optional[float] = Field(None, ge=-2.0, le=2.0, description="Default frequency penalty")
    presence_penalty: Optional[float] = Field(None, ge=-2.0, le=2.0, description="Default presence penalty")
    timeout: Optional[int] = Field(30, gt=0, description="Request timeout in seconds")
    retry_attempts: Optional[int] = Field(3, ge=0, description="Number of retry attempts")


class Model(BaseModel):
    """Language model specification."""

    model_config = ConfigDict(extra="allow")

    model_id: str = Field(..., description="Unique model identifier")
    model_name: str = Field(..., description="Human-readable model name")
    description: str = Field(..., description="Model description")
    model_type: ModelType = Field(..., description="Type of model")
    base_url: str = Field(..., description="Base URL for the model API")
    api_key_required: bool = Field(default=True, description="Whether an API key is required")
    model_path: Optional[str] = Field(None, description="Model path or identifier within the service")
    capabilities: ModelCapabilities = Field(default_factory=ModelCapabilities, description="Model capabilities")
    config: ModelConfig = Field(default_factory=ModelConfig, description="Default model configuration")
    status: ModelStatus = Field(default=ModelStatus.ACTIVE, description="Model status")
    tags: List[str] = Field(default_factory=list, description="Tags for categorization")
    created_at: datetime = Field(default_factory=datetime.utcnow, description="When the model was registered")
    updated_at: datetime = Field(default_factory=datetime.utcnow, description="When the model was last updated")

    @field_validator('base_url')
    @classmethod
    def validate_base_url(cls, v: str) -> str:
        """Validate that base_url is a valid URL."""
        if not v.startswith(('http://', 'https://')):
            raise ValueError("base_url must be a valid HTTP or HTTPS URL")
        return v

    @field_validator('model_id')
    @classmethod
    def validate_model_id(cls, v: str) -> str:
        """Validate model_id format."""
        if not v.strip():
            raise ValueError("model_id cannot be empty")
        # Allow alphanumeric, hyphens, underscores, and dots
        import re
        if not re.match(r'^[a-zA-Z0-9._-]+$', v):
            raise ValueError("model_id can only contain letters, numbers, dots, hyphens, and underscores")
        return v.strip()


class ModelSummary(BaseModel):
    """Simplified model information without detailed configuration."""

    model_config = ConfigDict(extra="allow")

    model_id: str = Field(..., description="Unique model identifier")
    model_name: str = Field(..., description="Human-readable model name")
    description: str = Field(..., description="Model description")
    model_type: ModelType = Field(..., description="Type of model")
    base_url: str = Field(..., description="Base URL for the model API")
    status: ModelStatus = Field(..., description="Model status")
    tags: List[str] = Field(default_factory=list, description="Tags for categorization")
    created_at: datetime = Field(..., description="When the model was registered")


class ModelRegistrationRequest(BaseModel):
    """Request model for registering a new model."""

    model_config = ConfigDict(extra="allow")

    model_id: str = Field(..., description="Unique model identifier")
    model_name: str = Field(..., description="Human-readable model name")
    description: str = Field(..., description="Model description")
    model_type: ModelType = Field(..., description="Type of model")
    base_url: str = Field(..., description="Base URL for the model API")
    api_key_required: bool = Field(default=True, description="Whether an API key is required")
    model_path: Optional[str] = Field(None, description="Model path or identifier within the service")
    capabilities: Optional[ModelCapabilities] = Field(None, description="Model capabilities")
    config: Optional[ModelConfig] = Field(None, description="Default model configuration")
    status: ModelStatus = Field(default=ModelStatus.ACTIVE, description="Model status")
    tags: List[str] = Field(default_factory=list, description="Tags for categorization")


class ModelUpdateRequest(BaseModel):
    """Request model for updating an existing model."""

    model_config = ConfigDict(extra="allow")

    model_name: Optional[str] = Field(None, description="Human-readable model name")
    description: Optional[str] = Field(None, description="Model description")
    model_type: Optional[ModelType] = Field(None, description="Type of model")
    base_url: Optional[str] = Field(None, description="Base URL for the model API")
    api_key_required: Optional[bool] = Field(None, description="Whether an API key is required")
    model_path: Optional[str] = Field(None, description="Model path or identifier within the service")
    capabilities: Optional[ModelCapabilities] = Field(None, description="Model capabilities")
    config: Optional[ModelConfig] = Field(None, description="Default model configuration")
    status: Optional[ModelStatus] = Field(None, description="Model status")
    tags: Optional[List[str]] = Field(None, description="Tags for categorization")


class ListModelsResponse(BaseModel):
    """Response for listing all models."""

    model_config = ConfigDict(extra="allow")

    models: List[ModelSummary] = Field(..., description="List of available models")
    total_models: int = Field(..., description="Total number of models")
    runtime_models: List[ModelSummary] = Field(default_factory=list, description="Models specified via environment variables")


class RuntimeModelConfig(BaseModel):
    """Configuration for runtime-specified models via environment variables."""

    model_config = ConfigDict(extra="allow")

    model_id: str = Field(..., description="Runtime model identifier")
    model_name: str = Field(..., description="Runtime model name")
    description: str = Field(default="Runtime-specified model", description="Model description")
    model_type: ModelType = Field(default=ModelType.OPENAI_COMPATIBLE, description="Type of model")
    base_url: str = Field(..., description="Base URL from environment variable")
    api_key_required: bool = Field(default=True, description="Whether an API key is required")
    model_path: Optional[str] = Field(None, description="Model path or identifier within the service")


class ModelsData(BaseModel):
    """Complete models configuration data."""

    model_config = ConfigDict(extra="allow")

    models: List[Model] = Field(default_factory=list, description="List of registered models")