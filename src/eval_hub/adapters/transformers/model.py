"""Model configuration transformer for schema adapters."""

from typing import Any

from pydantic import BaseModel, Field

from ...executors.base import ExecutionContext


class ModelConfig(BaseModel):
    """Extracted model configuration.

    Standardized model configuration extracted from execution context
    and backend configuration.
    """

    url: str = Field(..., description="Model endpoint URL")
    name: str = Field(..., description="Model name/identifier")
    configuration: dict[str, Any] = Field(
        default_factory=dict, description="Additional model configuration"
    )


class ModelConfigTransformer:
    """Transformer for extracting model configuration.

    Provides reusable logic for extracting and normalizing model
    configuration from execution contexts and backend configs.
    """

    def extract(
        self,
        context: ExecutionContext,
        backend_config: dict[str, Any],
    ) -> ModelConfig:
        """Extract model configuration from context and backend config.

        Args:
            context: Execution context containing model information
            backend_config: Backend-specific configuration

        Returns:
            ModelConfig with extracted model information
        """
        # Extract base model info from context
        model_url = context.model_url
        model_name = context.model_name

        # Extract additional configuration from backend config
        model_configuration = backend_config.get("model_configuration", {})

        # Allow backend config to override model URL/name if specified
        if "model_url_override" in backend_config:
            model_url = backend_config["model_url_override"]

        if "model_name_override" in backend_config:
            model_name = backend_config["model_name_override"]

        return ModelConfig(
            url=model_url,
            name=model_name,
            configuration=model_configuration,
        )

    def validate_model_config(self, model_config: ModelConfig) -> bool:
        """Validate extracted model configuration.

        Args:
            model_config: Model configuration to validate

        Returns:
            True if configuration is valid

        Raises:
            ValueError: If configuration is invalid
        """
        if not model_config.url:
            raise ValueError("Model URL is required")

        if not model_config.name:
            raise ValueError("Model name is required")

        return True
