"""Request parser service for evaluation specifications."""

from typing import Any

from ..core.config import Settings
from ..core.exceptions import ValidationError
from ..core.logging import get_logger
from ..models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationRequest,
    EvaluationSpec,
    RiskCategory,
)


class RequestParser:
    """Parser for evaluation requests and specifications."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self.logger = get_logger(__name__)

    async def parse_evaluation_request(
        self, request: EvaluationRequest, provider_service: Any = None
    ) -> EvaluationRequest:
        """Parse and validate an evaluation request."""
        self.logger.info(
            "Parsing evaluation request",
            request_id=str(request.request_id),
            evaluation_count=len(request.evaluations),
        )

        # Validate the request
        await self._validate_request(request)

        # Process each evaluation spec
        processed_evaluations = []
        for eval_spec in request.evaluations:
            processed_spec = await self._process_evaluation_spec(
                eval_spec, provider_service
            )
            processed_evaluations.append(processed_spec)

        # Create processed request
        processed_request = request.model_copy()
        processed_request.evaluations = processed_evaluations

        self.logger.info(
            "Successfully parsed evaluation request",
            request_id=str(request.request_id),
            total_backends=sum(len(spec.backends) for spec in processed_evaluations),
        )

        return processed_request

    async def _validate_request(self, request: EvaluationRequest) -> None:
        """Validate the evaluation request."""
        if not request.evaluations:
            raise ValidationError("Request must contain at least one evaluation")

        if len(request.evaluations) > 100:
            raise ValidationError("Request cannot contain more than 100 evaluations")

        # Validate each evaluation
        for i, eval_spec in enumerate(request.evaluations):
            await self._validate_evaluation_spec(eval_spec, f"evaluations[{i}]")

    async def _validate_evaluation_spec(
        self, spec: EvaluationSpec, context: str
    ) -> None:
        """Validate a single evaluation specification."""
        # model_server_id is optional during parsing - will be validated/derived during execution
        if not spec.model_name:
            raise ValidationError(f"{context}: model_name is required")

        if not spec.backends and not spec.risk_category and not spec.collection_id:
            raise ValidationError(
                f"{context}: must specify one of: backends, risk_category, or collection_id"
            )

        if spec.timeout_minutes <= 0:
            raise ValidationError(f"{context}: timeout_minutes must be positive")

        if spec.retry_attempts < 0:
            raise ValidationError(f"{context}: retry_attempts cannot be negative")

        # Validate backends if provided
        if spec.backends:
            for j, backend in enumerate(spec.backends):
                await self._validate_backend_spec(backend, f"{context}.backends[{j}]")

    async def _validate_backend_spec(self, backend: BackendSpec, context: str) -> None:
        """Validate a backend specification."""
        if not backend.name:
            raise ValidationError(f"{context}: name is required")

        if not backend.benchmarks:
            raise ValidationError(f"{context}: must specify at least one benchmark")

        # Check if backend type is supported
        from ..executors.factory import ExecutorFactory

        if backend.type == BackendType.CUSTOM:
            # Custom backends don't need executor validation
            pass
        elif not ExecutorFactory.is_backend_supported(backend.type.value):
            supported_backends = ExecutorFactory.get_supported_backend_types()
            raise ValidationError(
                f"{context}: unsupported backend type '{backend.type.value}'. "
                f"Supported backend types: {supported_backends}"
            )

        # Validate benchmarks
        for k, benchmark in enumerate(backend.benchmarks):
            await self._validate_benchmark_spec(benchmark, f"{context}.benchmarks[{k}]")

    async def _validate_benchmark_spec(
        self, benchmark: BenchmarkSpec, context: str
    ) -> None:
        """Validate a benchmark specification."""
        if not benchmark.name:
            raise ValidationError(f"{context}: name is required")

        if not benchmark.tasks:
            raise ValidationError(f"{context}: must specify at least one task")

        if benchmark.num_fewshot is not None and benchmark.num_fewshot < 0:
            raise ValidationError(f"{context}: num_fewshot cannot be negative")

        if benchmark.batch_size is not None and benchmark.batch_size <= 0:
            raise ValidationError(f"{context}: batch_size must be positive")

        if benchmark.limit is not None and benchmark.limit <= 0:
            raise ValidationError(f"{context}: limit must be positive")

    async def _process_evaluation_spec(
        self, spec: EvaluationSpec, provider_service: Any = None
    ) -> EvaluationSpec:
        """Process an evaluation specification, expanding risk categories if needed."""
        processed_spec = spec.model_copy()

        # If risk category is specified but no backends, generate backends
        if spec.risk_category and not spec.backends:
            self.logger.info(
                "Generating backends from risk category",
                evaluation_id=str(spec.id),
                risk_category=spec.risk_category,
            )
            processed_spec.backends = await self._generate_backends_from_risk_category(
                spec.risk_category, spec.model_name
            )

        # If collection ID is specified but no backends, generate backends
        elif spec.collection_id and not spec.backends:
            self.logger.info(
                "Generating backends from collection",
                evaluation_id=str(spec.id),
                collection_id=spec.collection_id,
            )
            processed_spec.backends = await self._generate_backends_from_collection(
                spec.collection_id, spec.model_name, provider_service
            )

        # Apply default configurations to backends
        for backend in processed_spec.backends:
            await self._apply_backend_defaults(backend)

        return processed_spec

    async def _generate_backends_from_risk_category(
        self, risk_category: RiskCategory, model_name: str
    ) -> list[BackendSpec]:
        """Generate backend specifications based on risk category."""
        risk_config = self.settings.risk_category_benchmarks.get(risk_category.value)
        if not risk_config:
            raise ValidationError(
                f"No configuration found for risk category: {risk_category}"
            )

        backends = []

        # Generate backend for each configured backend type
        for backend_name, backend_config in self.settings.backend_configs.items():
            # Create benchmarks based on risk category
            benchmarks = []
            for benchmark_name in risk_config["benchmarks"]:
                benchmark = BenchmarkSpec(
                    name=benchmark_name,
                    tasks=[
                        benchmark_name
                    ],  # Simplified - each benchmark is a single task
                    num_fewshot=risk_config.get("num_fewshot"),
                    batch_size=risk_config.get("batch_size"),
                    limit=risk_config.get("limit"),
                    device=risk_config.get("device"),
                    config={},
                )
                benchmarks.append(benchmark)

            # Create backend spec
            backend = BackendSpec(
                name=backend_name,
                type=(
                    BackendType.LMEVAL
                    if "lm-evaluation" in backend_name
                    else BackendType.GUIDELLM
                ),
                endpoint=None,
                benchmarks=benchmarks,
                config=backend_config.copy(),
            )
            backends.append(backend)

            self.logger.debug(
                "Generated backend from risk category",
                backend_name=backend_name,
                risk_category=risk_category,
                benchmark_count=len(benchmarks),
            )

        return backends

    async def _generate_backends_from_collection(
        self, collection_id: str, model_name: str, provider_service: Any = None
    ) -> list[BackendSpec]:
        """Generate backend specifications based on collection ID."""
        if provider_service is None:
            raise ValidationError(
                "Provider service is required for collection expansion"
            )

        # Get the collection
        collection = provider_service.get_collection_by_id(collection_id)
        if collection is None:
            raise ValidationError(f"Collection {collection_id} not found")

        self.logger.info(
            "Expanding collection to backends",
            collection_id=collection_id,
            benchmark_count=len(collection.benchmarks),
        )

        # Group benchmarks by provider
        providers: dict[str, list[Any]] = {}
        for benchmark_ref in collection.benchmarks:
            provider_id = benchmark_ref.provider_id
            if provider_id not in providers:
                providers[provider_id] = []
            providers[provider_id].append(benchmark_ref)

        # Create backend configurations
        backends = []
        backend_type_map = {
            "lm_evaluation_harness": BackendType.LMEVAL,
            "nemo_evaluator": BackendType.NEMO_EVALUATOR,
            "guidellm": BackendType.GUIDELLM,
        }

        for provider_id, benchmark_refs in providers.items():
            # Map provider to backend type
            backend_type = backend_type_map.get(provider_id, BackendType.CUSTOM)

            # Create benchmarks for this backend
            benchmarks = []
            for benchmark_ref in benchmark_refs:
                benchmark = BenchmarkSpec(
                    name=benchmark_ref.id,
                    tasks=[benchmark_ref.id],
                    config=benchmark_ref.config.copy(),
                    num_fewshot=benchmark_ref.config.get("num_fewshot"),
                    batch_size=benchmark_ref.config.get("batch_size"),
                    limit=benchmark_ref.config.get("limit"),
                    device=benchmark_ref.config.get("device"),
                )
                benchmarks.append(benchmark)

            # Create backend spec
            backend = BackendSpec(
                name=f"collection-{provider_id}",
                type=backend_type,
                endpoint=None,
                benchmarks=benchmarks,
                config={"batch_size": 1, "device": "auto"},
            )
            backends.append(backend)

            self.logger.debug(
                "Generated backend from collection",
                backend_name=backend.name,
                provider_id=provider_id,
                benchmark_count=len(benchmarks),
            )

        return backends

    async def _apply_backend_defaults(self, backend: BackendSpec) -> None:
        """Apply default configurations to a backend specification."""
        # Get default config for this backend type
        default_config = self.settings.backend_configs.get(backend.name, {})

        # Merge configurations (backend.config takes precedence)
        merged_config = default_config.copy()
        merged_config.update(backend.config)
        backend.config = merged_config

        # Apply defaults to benchmarks
        for benchmark in backend.benchmarks:
            await self._apply_benchmark_defaults(benchmark, backend.name)

    async def _apply_benchmark_defaults(
        self, benchmark: BenchmarkSpec, backend_name: str
    ) -> None:
        """Apply default configurations to a benchmark specification."""
        # Set default batch size if not specified
        if benchmark.batch_size is None:
            benchmark.batch_size = 1

        # Set default device if not specified
        if benchmark.device is None:
            benchmark.device = "auto"

        # Apply backend-specific defaults
        if backend_name == "lm-evaluation-harness":
            if benchmark.num_fewshot is None:
                benchmark.num_fewshot = 5

    def get_total_benchmark_count(self, request: EvaluationRequest) -> int:
        """Get the total number of benchmarks across all evaluations."""
        total = 0
        for evaluation in request.evaluations:
            for backend in evaluation.backends:
                total += len(backend.benchmarks)
        return total

    def estimate_completion_time(self, request: EvaluationRequest) -> int:
        """Estimate completion time in minutes for the request."""
        total_benchmarks = self.get_total_benchmark_count(request)

        # Simple estimation: 5 minutes per benchmark on average
        base_time = total_benchmarks * 5

        # Add overhead for concurrent execution
        max_concurrent = self.settings.max_concurrent_evaluations
        if total_benchmarks > max_concurrent:
            # Account for queuing time
            base_time = int(base_time * 1.5)

        return max(base_time, 10)  # Minimum 10 minutes
