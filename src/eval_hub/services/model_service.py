"""Model service for managing language model registration and runtime configuration."""

import os
import re
from datetime import datetime
from typing import Dict, List, Optional, Union
from ..core.config import Settings
from ..core.logging import get_logger
from ..models.model import (
    Model,
    ModelSummary,
    ModelType,
    ModelStatus,
    ModelCapabilities,
    ModelConfig,
    ModelRegistrationRequest,
    ModelUpdateRequest,
    ListModelsResponse,
    RuntimeModelConfig,
    ModelsData,
)

logger = get_logger(__name__)


class ModelService:
    """Service for managing language models."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self._registered_models: Dict[str, Model] = {}
        self._runtime_models: Dict[str, Model] = {}
        self._initialized = False

    def _initialize(self) -> None:
        """Initialize the model service by loading runtime models from environment variables."""
        if self._initialized:
            return

        self._load_runtime_models()
        self._initialized = True
        logger.info(
            "Model service initialized",
            registered_models=len(self._registered_models),
            runtime_models=len(self._runtime_models)
        )

    def _load_runtime_models(self) -> None:
        """Load models specified via environment variables."""
        # Pattern: EVAL_HUB_MODEL_<MODEL_ID>_URL=<url>
        # Optional: EVAL_HUB_MODEL_<MODEL_ID>_NAME=<name>
        # Optional: EVAL_HUB_MODEL_<MODEL_ID>_TYPE=<type>
        # Optional: EVAL_HUB_MODEL_<MODEL_ID>_PATH=<path>

        runtime_models = {}

        for env_var, env_value in os.environ.items():
            # Look for model URL environment variables
            if env_var.startswith("EVAL_HUB_MODEL_") and env_var.endswith("_URL"):
                # Extract model ID from environment variable name
                # EVAL_HUB_MODEL_GPT4_URL -> GPT4
                match = re.match(r'EVAL_HUB_MODEL_(.+)_URL', env_var)
                if not match:
                    continue

                model_id = match.group(1).lower()
                base_url = env_value.strip()

                if not base_url:
                    logger.warning(f"Empty URL for runtime model {model_id}, skipping")
                    continue

                # Get optional configuration from other environment variables
                name_var = f"EVAL_HUB_MODEL_{match.group(1)}_NAME"
                type_var = f"EVAL_HUB_MODEL_{match.group(1)}_TYPE"
                path_var = f"EVAL_HUB_MODEL_{match.group(1)}_PATH"

                model_name = os.getenv(name_var, f"Runtime Model {model_id.upper()}")
                model_type_str = os.getenv(type_var, "openai-compatible")
                model_path = os.getenv(path_var)

                # Validate model type
                try:
                    model_type = ModelType(model_type_str.lower())
                except ValueError:
                    logger.warning(
                        f"Invalid model type '{model_type_str}' for runtime model {model_id}, "
                        f"using default 'openai-compatible'"
                    )
                    model_type = ModelType.OPENAI_COMPATIBLE

                # Create runtime model
                runtime_model = Model(
                    model_id=model_id,
                    model_name=model_name,
                    description=f"Runtime-specified model: {model_name}",
                    model_type=model_type,
                    base_url=base_url,
                    api_key_required=True,  # Default to requiring API key for security
                    model_path=model_path,
                    capabilities=ModelCapabilities(),
                    config=ModelConfig(),
                    status=ModelStatus.ACTIVE,
                    tags=["runtime"],
                    created_at=datetime.utcnow(),
                    updated_at=datetime.utcnow()
                )

                runtime_models[model_id] = runtime_model
                logger.info(
                    "Loaded runtime model from environment",
                    model_id=model_id,
                    model_name=model_name,
                    model_type=model_type.value,
                    base_url=base_url
                )

        self._runtime_models = runtime_models

    def register_model(self, request: ModelRegistrationRequest) -> Model:
        """Register a new model."""
        self._initialize()

        # Check if model ID already exists
        if request.model_id in self._registered_models:
            raise ValueError(f"Model with ID '{request.model_id}' already exists")

        if request.model_id in self._runtime_models:
            raise ValueError(f"Model with ID '{request.model_id}' is specified as runtime model via environment variable")

        # Create the model
        now = datetime.utcnow()
        model = Model(
            model_id=request.model_id,
            model_name=request.model_name,
            description=request.description,
            model_type=request.model_type,
            base_url=request.base_url,
            api_key_required=request.api_key_required,
            model_path=request.model_path,
            capabilities=request.capabilities or ModelCapabilities(),
            config=request.config or ModelConfig(),
            status=request.status,
            tags=request.tags,
            created_at=now,
            updated_at=now
        )

        self._registered_models[request.model_id] = model

        logger.info(
            "Model registered successfully",
            model_id=request.model_id,
            model_name=request.model_name,
            model_type=request.model_type.value
        )

        return model

    def update_model(self, model_id: str, request: ModelUpdateRequest) -> Optional[Model]:
        """Update an existing registered model."""
        self._initialize()

        if model_id in self._runtime_models:
            raise ValueError("Cannot update runtime models specified via environment variables")

        if model_id not in self._registered_models:
            return None

        model = self._registered_models[model_id]

        # Update fields that are provided
        if request.model_name is not None:
            model.model_name = request.model_name
        if request.description is not None:
            model.description = request.description
        if request.model_type is not None:
            model.model_type = request.model_type
        if request.base_url is not None:
            model.base_url = request.base_url
        if request.api_key_required is not None:
            model.api_key_required = request.api_key_required
        if request.model_path is not None:
            model.model_path = request.model_path
        if request.capabilities is not None:
            model.capabilities = request.capabilities
        if request.config is not None:
            model.config = request.config
        if request.status is not None:
            model.status = request.status
        if request.tags is not None:
            model.tags = request.tags

        model.updated_at = datetime.utcnow()

        logger.info(
            "Model updated successfully",
            model_id=model_id,
            model_name=model.model_name
        )

        return model

    def delete_model(self, model_id: str) -> bool:
        """Delete a registered model."""
        self._initialize()

        if model_id in self._runtime_models:
            raise ValueError("Cannot delete runtime models specified via environment variables")

        if model_id in self._registered_models:
            del self._registered_models[model_id]
            logger.info("Model deleted successfully", model_id=model_id)
            return True

        return False

    def get_model_by_id(self, model_id: str) -> Optional[Model]:
        """Get a model by ID (from either registered or runtime models)."""
        self._initialize()

        # Check registered models first
        if model_id in self._registered_models:
            return self._registered_models[model_id]

        # Check runtime models
        if model_id in self._runtime_models:
            return self._runtime_models[model_id]

        return None

    def get_all_models(self, include_inactive: bool = True) -> ListModelsResponse:
        """Get all models (registered and runtime)."""
        self._initialize()

        # Convert registered models to summaries
        registered_summaries = []
        for model in self._registered_models.values():
            if include_inactive or model.status == ModelStatus.ACTIVE:
                summary = ModelSummary(
                    model_id=model.model_id,
                    model_name=model.model_name,
                    description=model.description,
                    model_type=model.model_type,
                    base_url=model.base_url,
                    status=model.status,
                    tags=model.tags,
                    created_at=model.created_at
                )
                registered_summaries.append(summary)

        # Convert runtime models to summaries
        runtime_summaries = []
        for model in self._runtime_models.values():
            summary = ModelSummary(
                model_id=model.model_id,
                model_name=model.model_name,
                description=model.description,
                model_type=model.model_type,
                base_url=model.base_url,
                status=model.status,
                tags=model.tags,
                created_at=model.created_at
            )
            runtime_summaries.append(summary)

        # Combine all models
        all_summaries = registered_summaries + runtime_summaries

        return ListModelsResponse(
            models=all_summaries,
            total_models=len(all_summaries),
            runtime_models=runtime_summaries
        )

    def search_models(
        self,
        model_type: Optional[ModelType] = None,
        status: Optional[ModelStatus] = None,
        tags: Optional[List[str]] = None
    ) -> List[Model]:
        """Search models by criteria."""
        self._initialize()

        # Combine all models for searching
        all_models = list(self._registered_models.values()) + list(self._runtime_models.values())

        filtered_models = []
        for model in all_models:
            # Filter by model type
            if model_type and model.model_type != model_type:
                continue

            # Filter by status
            if status and model.status != status:
                continue

            # Filter by tags
            if tags and not any(tag in model.tags for tag in tags):
                continue

            filtered_models.append(model)

        return filtered_models

    def get_active_models(self) -> List[Model]:
        """Get all active models."""
        return self.search_models(status=ModelStatus.ACTIVE)

    def reload_runtime_models(self) -> None:
        """Reload runtime models from environment variables."""
        self._runtime_models.clear()
        self._load_runtime_models()
        logger.info("Runtime models reloaded from environment variables")