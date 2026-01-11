"""API routes for the evaluation service."""

import asyncio
import time
from typing import Any
from uuid import UUID, uuid4

from fastapi import (
    APIRouter,
    BackgroundTasks,
    Depends,
    HTTPException,
    Path,
    Query,
    Request,
    status,
)

from ..core.config import Settings, get_settings
from ..core.exceptions import ValidationError
from ..core.logging import get_logger
from ..models.evaluation import (
    # Other evaluation models
    BackendSpec,
    BackendType,
    BenchmarkConfig,
    BenchmarkSpec,
    Error,
    EvaluationJobRequest,
    EvaluationJobResource,
    EvaluationJobResourceList,
    EvaluationRequest,
    EvaluationResult,
    EvaluationSpec,
    EvaluationStatus,
    Resource,
    get_utc_now,
)
from ..models.health import HealthResponse
from ..models.provider import (
    Benchmark,
    BenchmarkReference,
    BenchmarksList,
    # Other provider models
    CollectionCreationRequest,
    CollectionResource,
    CollectionResourceList,
    CollectionUpdateRequest,
    PaginationLink,
    Provider,
    ProviderList,
    SupportedBenchmark,
)
from ..services.executor import EvaluationExecutor
from ..services.mlflow_client import MLFlowClient
from ..services.parser import RequestParser
from ..services.provider_service import ProviderService
from ..services.response_builder import ResponseBuilder
from ..utils.datetime_utils import parse_iso_datetime, utcnow

router = APIRouter()
logger = get_logger(__name__)

# Common responses for OpenAPI documentation
VALIDATION_ERROR_RESPONSES: dict[int | str, dict[str, Any]] = {
    422: {"model": Error, "description": "Validation Error"}
}

# Global state for active evaluations and services
active_evaluations: dict[str, EvaluationJobResource] = {}
evaluation_tasks: dict[str, asyncio.Task[Any]] = {}


def get_request_parser(settings: Settings = Depends(get_settings)) -> RequestParser:
    """Dependency to get request parser."""
    return RequestParser(settings)


def get_evaluation_executor(
    settings: Settings = Depends(get_settings),
) -> EvaluationExecutor:
    """Dependency to get evaluation executor."""
    return EvaluationExecutor(settings)


def get_mlflow_client(settings: Settings = Depends(get_settings)) -> MLFlowClient:
    """Dependency to get MLFlow client."""
    return MLFlowClient(settings)


def get_response_builder(settings: Settings = Depends(get_settings)) -> ResponseBuilder:
    """Dependency to get response builder."""
    return ResponseBuilder(settings)


def get_provider_service(request: Request) -> ProviderService:
    """Dependency to get provider service from app state (loaded at startup)."""
    if not hasattr(request.app.state, "provider_service"):
        settings = get_settings()
        provider_service = ProviderService(settings)
        provider_service.initialize()
        request.app.state.provider_service = provider_service
    return request.app.state.provider_service  # type: ignore[no-any-return]


@router.get("/health", response_model=HealthResponse)
async def health_check(settings: Settings = Depends(get_settings)) -> HealthResponse:
    """Health check endpoint."""
    # Calculate uptime
    start_time = getattr(health_check, "start_time", time.time())
    uptime_seconds = time.time() - start_time
    if not hasattr(health_check, "start_time"):
        health_check.start_time = start_time  # type: ignore[attr-defined]

    # Check component health
    components = {}

    # Check MLFlow
    try:
        # Simple health check - just verify the configuration
        components["mlflow"] = {
            "status": "healthy",
            "tracking_uri": settings.mlflow_tracking_uri,
        }
    except Exception as e:
        components["mlflow"] = {"status": "unhealthy", "error": str(e)}

    return HealthResponse(
        status="healthy",
        version=settings.version,
        timestamp=utcnow(),
        components=components,
        uptime_seconds=uptime_seconds,
        active_evaluations=len(active_evaluations),
    )


@router.post(
    "/evaluations/jobs",
    response_model=EvaluationJobResource,
    status_code=status.HTTP_202_ACCEPTED,
    tags=["Evaluations"],
    responses={422: {"model": Error, "description": "Validation Error"}},
)
async def create_evaluation(
    request: EvaluationJobRequest,
    background_tasks: BackgroundTasks,
    parser: RequestParser = Depends(get_request_parser),
    executor: EvaluationExecutor = Depends(get_evaluation_executor),
    mlflow_client: MLFlowClient = Depends(get_mlflow_client),
    response_builder: ResponseBuilder = Depends(get_response_builder),
    settings: Settings = Depends(get_settings),
    provider_service: ProviderService = Depends(get_provider_service),
) -> EvaluationJobResource:
    """Create and execute evaluation request using the simplified benchmark schema."""
    # Generate a unique request ID for this evaluation
    request_id = uuid4()

    # Use incoming request payloads as internal models
    internal_model = request.model
    internal_experiment = request.experiment
    job_benchmarks = request.benchmarks

    logger.info(
        "Received evaluation request",
        request_id=str(request_id),
        benchmark_count=len(job_benchmarks),
        experiment_name=internal_experiment.name if internal_experiment else None,
        async_mode=True,  # Always async in new API
    )

    try:
        # Validate required benchmark fields
        for benchmark in job_benchmarks:
            bench_id = benchmark.id
            provider_id = benchmark.provider_id
            if bench_id is None or provider_id is None:
                raise HTTPException(
                    status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
                    detail="Benchmark id and provider_id are required",
                )
            # Note: Allow any benchmark ID, even if not in predefined list

        # Group benchmarks by provider to create backend specs
        provider_benchmarks: dict[str, list[BenchmarkConfig]] = {}
        for benchmark in job_benchmarks:
            # Convert to BenchmarkConfig for backend processing
            legacy_benchmark = BenchmarkConfig(
                benchmark_id=benchmark.id or "",  # Map 'id' to 'benchmark_id'
                provider_id=benchmark.provider_id or "",
                config=benchmark.config,
            )
            if legacy_benchmark.provider_id not in provider_benchmarks:
                provider_benchmarks[legacy_benchmark.provider_id] = []
            provider_benchmarks[legacy_benchmark.provider_id].append(legacy_benchmark)

        # Create evaluation spec
        backend_specs = []
        for provider_id, benchmarks in provider_benchmarks.items():
            # Get provider to determine backend type
            provider = provider_service.get_provider_by_id(provider_id)
            if not provider:
                raise HTTPException(
                    status_code=status.HTTP_404_NOT_FOUND,
                    detail=f"Provider {provider_id} not found",
                )
            if not provider.provider_type:
                raise HTTPException(
                    status_code=status.HTTP_400_BAD_REQUEST,
                    detail=f"Provider {provider_id} missing provider_type",
                )

            # Map provider type to backend type
            if (
                provider.provider_type.value == "builtin"
                and provider_id == "lm_evaluation_harness"
            ):
                backend_type = BackendType.LMEVAL
            elif provider.provider_type.value == "nemo-evaluator":
                backend_type = BackendType.NEMO_EVALUATOR
            else:
                raise HTTPException(
                    status_code=status.HTTP_400_BAD_REQUEST,
                    detail=f"Unsupported provider type: {provider.provider_type} (provider_id: {provider_id})",
                )

            # Convert BenchmarkConfigs to BenchmarkSpecs
            benchmark_specs = []
            for bench_config in benchmarks:  # type: BenchmarkConfig
                # Note: No longer fetching benchmark details since validation was removed
                benchmark_spec = BenchmarkSpec(
                    name=bench_config.benchmark_id or "",
                    tasks=[bench_config.benchmark_id or ""],
                    num_fewshot=bench_config.config.get("num_fewshot"),
                    batch_size=bench_config.config.get("batch_size"),
                    limit=bench_config.config.get("limit"),
                    device=bench_config.config.get("device"),
                    config=bench_config.config,
                )
                benchmark_specs.append(benchmark_spec)

            backend_spec = BackendSpec(
                name=f"{provider_id}-backend",
                type=backend_type,
                endpoint=provider.base_url,
                config={},
                benchmarks=benchmark_specs,
            )
            backend_specs.append(backend_spec)

        # Determine evaluation name
        evaluation_name = (
            internal_experiment.name
            if internal_experiment
            else f"Evaluation-{request_id.hex[:8]}"
        )

        evaluation_spec = EvaluationSpec(
            name=evaluation_name,
            description=f"Evaluation with {len(job_benchmarks)} benchmarks",
            model=internal_model,
            backends=backend_specs,
            risk_category=None,
            collection_id=None,
            timeout_minutes=request.timeout_minutes,
            retry_attempts=request.retry_attempts,
        )

        # Create legacy evaluation request
        full_legacy_request = EvaluationRequest(
            request_id=request_id,
            evaluations=[evaluation_spec],
            experiment=internal_experiment,
            async_mode=True,
            callback_url=request.callback_url,
            created_at=get_utc_now(),
        )

        # Use existing evaluation processing logic
        parsed_request = await parser.parse_evaluation_request(
            full_legacy_request, provider_service
        )

        # Conditionally create MLFlow experiment only if experiment config is provided
        experiment_id = None
        experiment_url = None
        if internal_experiment is not None:
            experiment_id = await mlflow_client.create_experiment(parsed_request)
            experiment_url = await mlflow_client.get_experiment_url(experiment_id)

        # Build evaluation response and store in active evaluations
        response = await response_builder.build_job_resource_response(
            request_id,
            request,
            [],  # No results yet
            experiment_url,
        )
        active_evaluations[str(request_id)] = response

        # Start background task
        task = asyncio.create_task(
            _execute_evaluation_async(
                full_legacy_request,
                request,
                experiment_id,
                experiment_url,
                executor,
                mlflow_client,
                response_builder,
            )
        )
        evaluation_tasks[str(request_id)] = task

        return response

    except ValidationError as e:
        logger.error(
            "Validation failed for evaluation request",
            request_id=str(request_id),
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Validation error: {e.message}",
        ) from e

    except Exception as e:
        logger.error(
            "Failed to create evaluation",
            request_id=str(request_id),
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to create evaluation: {str(e)}",
        ) from e


@router.get(
    "/evaluations/jobs/{id}",
    response_model=EvaluationJobResource,
    tags=["Evaluations"],
    responses=VALIDATION_ERROR_RESPONSES,
)
async def get_evaluation_status(
    id: UUID,
    response_builder: ResponseBuilder = Depends(get_response_builder),
    executor: EvaluationExecutor = Depends(get_evaluation_executor),
) -> EvaluationJobResource:
    """Get the status of an evaluation request."""
    request_id_str = str(id)

    if request_id_str not in active_evaluations:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Evaluation request {id} not found",
        )

    # Get the current response
    response = active_evaluations[request_id_str]

    # If evaluation is still pending, check if the underlying job has completed
    if response.status.state == "pending":
        updated_response = await executor.check_and_update_evaluation_status(
            request_id_str, response
        )
        if updated_response:
            # Update stored evaluation with completed status
            active_evaluations[request_id_str] = updated_response
            response = updated_response

    logger.info(
        "Retrieved evaluation status",
        request_id=request_id_str,
        status=response.status.state,
    )

    return response


@router.get(
    "/evaluations/jobs",
    response_model=EvaluationJobResourceList,
    tags=["Evaluations"],
    responses=VALIDATION_ERROR_RESPONSES,
)
async def list_evaluations(
    request: Request,
    summary: bool = Query(False, description="Return summary information"),
    limit: int = Query(
        50, ge=1, le=100, description="Maximum number of evaluations to return"
    ),
    offset: int = Query(0, ge=0, description="Number of evaluations to skip"),
    status_filter: str | None = Query(None, description="Filter by status"),
) -> EvaluationJobResourceList:
    """List all evaluation requests."""
    evaluations = list(active_evaluations.values())

    # Apply status filter
    if status_filter:
        evaluations = [e for e in evaluations if e.status.state == status_filter]

    total_count = len(evaluations)

    # Apply pagination window
    evaluations = evaluations[offset : offset + limit]

    # Create pagination response (evaluations are already in correct format)
    base_url = str(request.url).split("?")[0]
    first_href = f"{base_url}?offset=0&limit={limit}"
    next_href = (
        f"{base_url}?offset={offset + limit}&limit={limit}"
        if offset + limit < total_count
        else None
    )

    logger.info(
        "Listed evaluations",
        total_count=total_count,
        returned_count=len(evaluations),
        status_filter=status_filter,
        limit=limit,
        summary=summary,
    )

    return EvaluationJobResourceList(
        first={"href": first_href},
        next={"href": next_href} if next_href else None,
        limit=limit,
        total_count=total_count,
        items=evaluations,
    )


@router.delete(
    "/evaluations/jobs/{id}",
    status_code=status.HTTP_204_NO_CONTENT,  # Explicitly set 204 status code
    tags=["Evaluations"],
    responses=VALIDATION_ERROR_RESPONSES,
)
async def cancel_evaluation(
    id: UUID,
    executor: EvaluationExecutor = Depends(get_evaluation_executor),
) -> None:
    """Cancel a running evaluation."""
    request_id_str = str(id)

    if request_id_str not in active_evaluations:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Evaluation request {id} not found",
        )

    # Cancel the task if it's running
    if request_id_str in evaluation_tasks:
        task = evaluation_tasks[request_id_str]
        task.cancel()
        del evaluation_tasks[request_id_str]

    # Update status
    response = active_evaluations[request_id_str]
    response.status.state = "cancelled"
    response.resource.updated_at = get_utc_now()

    logger.info("Cancelled evaluation", request_id=request_id_str)

    # FastAPI automatically returns 204 with None return and status_code=204 in decorator


@router.get("/metrics/system")
async def get_system_metrics(
    executor: EvaluationExecutor = Depends(get_evaluation_executor),
) -> dict[str, Any]:
    """Get system metrics and statistics."""
    active_tasks = await executor.get_active_evaluations()

    metrics = {
        "active_evaluations": len(active_evaluations),
        "running_tasks": len(active_tasks),
        "total_requests": len(active_evaluations),
        "memory_usage": {
            "active_evaluations_mb": len(active_evaluations) * 0.1,  # Rough estimation
            "cached_results_mb": sum(
                len(r.results.benchmarks) if r.results else 0
                for r in active_evaluations.values()
            )
            * 0.01,
        },
        "status_breakdown": {},
    }

    # Count evaluations by status
    status_breakdown: dict[str, int] = {}
    for response in active_evaluations.values():
        status_str = response.status.state
        status_breakdown[status_str] = status_breakdown.get(status_str, 0) + 1
    metrics["status_breakdown"] = status_breakdown

    return metrics


# Provider and Benchmark Management Endpoints


@router.get("/evaluations/providers", response_model=ProviderList, tags=["Providers"])
async def list_providers(
    provider_service: ProviderService = Depends(get_provider_service),
) -> ProviderList:
    """List all registered evaluation providers."""
    logger.info("Listing all evaluation providers")

    # Get all providers from service
    providers = provider_service.get_all_providers()

    # Convert to API format
    provider_items = []
    for provider in providers:
        supported_benchmarks = [
            SupportedBenchmark(id=benchmark.benchmark_id)
            for benchmark in provider.benchmarks
        ]

        provider_item = Provider(
            id=provider.provider_id,
            label=provider.provider_name,
            supported_benchmarks=supported_benchmarks,
        )
        provider_items.append(provider_item)

    return ProviderList(
        total_count=len(provider_items),
        items=provider_items,
    )


@router.get(
    "/evaluations/providers/{id}",
    response_model=Provider,
    tags=["Providers"],
    responses=VALIDATION_ERROR_RESPONSES,
    operation_id="get_provider_api_v1_evaluations_providers__provider_id__get",
)
async def get_provider(
    id: str = Path(..., title="Provider Id"),
    provider_service: ProviderService = Depends(get_provider_service),
) -> Provider:
    """Get details of a specific provider."""
    provider = provider_service.get_provider_by_id(id)
    if not provider:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Provider {id} not found",
        )

    logger.info("Retrieved provider details", provider_id=id)

    # Convert to simplified format
    supported_benchmarks = [
        SupportedBenchmark(id=benchmark.benchmark_id)
        for benchmark in provider.benchmarks
    ]

    return Provider(
        id=provider.provider_id,
        label=provider.provider_name,
        supported_benchmarks=supported_benchmarks,
    )


@router.get(
    "/evaluations/benchmarks",
    response_model=BenchmarksList,
    tags=["Benchmarks"],
    responses=VALIDATION_ERROR_RESPONSES,
)
async def list_all_benchmarks(
    provider_id: str | None = Query(None, description="Filter by provider ID"),
    category: str | None = Query(None, description="Filter by benchmark category"),
    tags: str | None = Query(None, description="Filter by tags (comma-separated)"),
    provider_service: ProviderService = Depends(get_provider_service),
) -> BenchmarksList:
    """List all available benchmarks across providers (simplified format)."""
    logger.info(
        "Listing benchmarks", provider_id=provider_id, category=category, tags=tags
    )

    if provider_id or category or tags:
        # Apply filters using search functionality
        tag_list = tags.split(",") if tags else None
        filtered_benchmarks = provider_service.search_benchmarks(
            category=category, provider_id=provider_id, tags=tag_list
        )

        # Convert to simplified format
        benchmarks = []
        for benchmark_detail in filtered_benchmarks:
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

        # Build the simplified response (no providers_included field)
        return BenchmarksList(
            items=benchmarks,
            total_count=len(benchmarks),
        )
    else:
        # Get all benchmarks and convert to simplified format
        full_response = provider_service.get_all_benchmarks()

        # Convert to simplified benchmarks
        simple_benchmarks = []
        for benchmark in full_response.items:
            simple_benchmarks.append(
                Benchmark(
                    id=benchmark.benchmark_id,
                    provider_id=benchmark.provider_id,
                    label=benchmark.name,
                    description=benchmark.description,
                    category=benchmark.category,
                    metrics=benchmark.metrics,
                    num_few_shot=benchmark.num_few_shot,
                    dataset_size=benchmark.dataset_size,
                    tags=benchmark.tags,
                )
            )

        return BenchmarksList(
            items=simple_benchmarks,
            total_count=full_response.total_count,
        )


@router.get(
    "/evaluations/collections",
    response_model=CollectionResourceList,
    tags=["Collections"],
)
async def list_collections(
    provider_service: ProviderService = Depends(get_provider_service),
) -> CollectionResourceList:
    """List all benchmark collections."""
    logger.info("Listing all benchmark collections")

    # Get legacy response
    legacy_response = provider_service.get_all_collections()

    # Convert to simplified format
    simple_collection_resources = []
    for collection in legacy_response.collections:
        # Handle timestamps - parse strings to datetime or use current time for None values
        now = utcnow()

        created_at = now
        if collection.created_at is not None:
            try:
                created_at = parse_iso_datetime(collection.created_at)
            except ValueError:
                # If parsing fails, fall back to current time
                created_at = now

        updated_at = now
        if collection.updated_at is not None:
            try:
                updated_at = parse_iso_datetime(collection.updated_at)
            except ValueError:
                # If parsing fails, fall back to current time
                updated_at = now

        # Convert benchmarks to simplified format
        simple_benchmarks = []
        for benchmark in collection.benchmarks:
            simple_benchmark = BenchmarkReference(
                provider_id=benchmark.provider_id,
                id=benchmark.benchmark_id,  # Use alias mapping
                weight=benchmark.weight,
                config=benchmark.config,
            )
            simple_benchmarks.append(simple_benchmark)

        simple_collection_resource = CollectionResource(
            resource=Resource(
                id=collection.collection_id,
                created_at=created_at,
                updated_at=updated_at,
            ),
            name=collection.name,
            description=collection.description,
            tags=collection.tags,
            custom=collection.metadata,  # Expose metadata as custom field
            benchmarks=simple_benchmarks,
        )
        simple_collection_resources.append(simple_collection_resource)

    return CollectionResourceList(
        first=PaginationLink(href="/api/v1/evaluations/collections"),
        next=None,  # No pagination in simple implementation
        limit=len(simple_collection_resources),
        total_count=legacy_response.total_collections,
        items=simple_collection_resources,
        # No collections field (removing duplicate)
    )


@router.get(
    "/evaluations/collections/{id}",  # Changed from collection_id to id
    response_model=CollectionResource,
    tags=["Collections"],
    responses=VALIDATION_ERROR_RESPONSES,
    operation_id="get_collection_api_v1_evaluations_collections__collection_id__get",
)
async def get_collection(
    id: str = Path(..., title="Collection Id"),  # Changed from collection_id to id
    provider_service: ProviderService = Depends(get_provider_service),
) -> CollectionResource:
    """Get details of a specific collection."""
    collection = provider_service.get_collection_by_id(id)
    if not collection:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Collection {id} not found",
        )

    logger.info("Retrieved collection details", collection_id=id)

    # Convert to new Resource format
    collection_resource = CollectionResource(
        resource=Resource(
            id=collection.collection_id,
            created_at=parse_iso_datetime(collection.created_at)
            if collection.created_at
            else utcnow(),
            updated_at=parse_iso_datetime(collection.updated_at)
            if collection.updated_at
            else utcnow(),
        ),
        name=collection.name,
        description=collection.description,
        tags=collection.tags,
        custom=collection.metadata,
        benchmarks=collection.benchmarks,
    )

    return collection_resource


@router.post(
    "/evaluations/collections",
    response_model=CollectionResource,
    status_code=status.HTTP_201_CREATED,
    tags=["Collections"],
    responses=VALIDATION_ERROR_RESPONSES,
)
async def create_collection(
    request: CollectionCreationRequest,
    provider_service: ProviderService = Depends(get_provider_service),
) -> CollectionResource:
    """Create a new collection."""
    try:
        collection = provider_service.create_collection(request)
        logger.info(
            "Collection created successfully",
            collection_id=collection.collection_id,
        )

        # Convert to new Resource format
        collection_resource = CollectionResource(
            resource=Resource(
                id=collection.collection_id,
                created_at=parse_iso_datetime(collection.created_at)
                if collection.created_at
                else utcnow(),
                updated_at=parse_iso_datetime(collection.updated_at)
                if collection.updated_at
                else utcnow(),
            ),
            name=collection.name,
            description=collection.description,
            tags=collection.tags,
            custom=collection.metadata,
            benchmarks=collection.benchmarks,
        )

        return collection_resource
    except ValueError as e:
        logger.error(
            "Failed to create collection",
            collection_id=request.name,  # Changed from collection_id since request doesn't have collection_id
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error creating collection",
            collection_id=request.name,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to create collection: {str(e)}",
        ) from e


# Legacy PUT endpoint removed - use PATCH instead


@router.patch(
    "/evaluations/collections/{id}",  # Changed from collection_id to id
    response_model=CollectionResource,
    tags=["Collections"],
    responses=VALIDATION_ERROR_RESPONSES,
    operation_id="patch_collection_api_v1_evaluations_collections__collection_id__patch",
)
async def patch_collection(
    request: CollectionUpdateRequest,
    id: str = Path(..., title="Collection Id"),
    provider_service: ProviderService = Depends(get_provider_service),
) -> CollectionResource:
    """Partially update an existing collection."""
    try:
        # Get the existing collection first
        collection = provider_service.get_collection_by_id(id)
        if not collection:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Collection {id} not found",
            )

        # Apply updates directly from the request
        update_data: dict[str, Any] = {}
        if request.name is not None:
            update_data["name"] = request.name
        if request.description is not None:
            update_data["description"] = request.description
        if request.tags is not None:
            update_data["tags"] = request.tags
        if request.metadata is not None:
            update_data["metadata"] = request.metadata
        if request.benchmarks is not None:
            update_data["benchmarks"] = request.benchmarks

        # Create updated collection object
        updated_collection = collection.model_copy(update=update_data)

        # For demonstration, we'll just update the updated_at timestamp
        updated_collection.updated_at = get_utc_now().isoformat()

        logger.info("Collection patched successfully", collection_id=id)

        # Handle timestamps - parse strings to datetime or use current time for None values
        now = utcnow()

        created_at = now
        if updated_collection.created_at is not None:
            try:
                created_at = parse_iso_datetime(updated_collection.created_at)
            except ValueError:
                # If parsing fails, fall back to current time
                created_at = now

        updated_at = now
        if updated_collection.updated_at is not None:
            try:
                updated_at = parse_iso_datetime(updated_collection.updated_at)
            except ValueError:
                # If parsing fails, fall back to current time
                updated_at = now

        # Convert to new Resource format
        collection_resource = CollectionResource(
            resource=Resource(
                id=updated_collection.collection_id,
                created_at=created_at,
                updated_at=updated_at,
            ),
            name=updated_collection.name,
            description=updated_collection.description,
            tags=updated_collection.tags,
            custom=updated_collection.metadata,
            benchmarks=updated_collection.benchmarks,
        )

        return collection_resource

    except ValueError as e:
        logger.error("Failed to patch collection", collection_id=id, error=str(e))
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error patching collection",
            collection_id=id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to patch collection: {str(e)}",
        ) from e


@router.delete(
    "/evaluations/collections/{id}",
    status_code=status.HTTP_204_NO_CONTENT,  # Explicitly set 204 status code
    tags=["Collections"],
    operation_id="delete_collection_api_v1_evaluations_collections__collection_id__delete",
    responses=VALIDATION_ERROR_RESPONSES,
)
async def delete_collection(
    id: str = Path(..., title="Collection Id"),
    provider_service: ProviderService = Depends(get_provider_service),
) -> None:
    """Delete a collection."""
    try:
        success = provider_service.delete_collection(id)
        if not success:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Collection {id} not found",
            )

        logger.info("Collection deleted successfully", collection_id=id)

        # FastAPI automatically returns 204 with None return and status_code=204 in decorator

    except ValueError as e:
        logger.error(
            "Failed to delete collection",
            collection_id=id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error deleting collection",
            collection_id=id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to delete collection: {str(e)}",
        ) from e


async def _execute_evaluation_async(
    request: EvaluationRequest,
    job_request: EvaluationJobRequest,
    experiment_id: str | None,
    experiment_url: str | None,
    executor: EvaluationExecutor,
    mlflow_client: MLFlowClient,
    response_builder: ResponseBuilder,
) -> None:
    """Execute evaluation asynchronously and update stored response."""
    request_id_str = str(request.request_id)

    try:
        # Progress callback to update stored response
        def progress_callback_sync(eval_id: str, progress: float, message: str) -> None:
            if request_id_str in active_evaluations:
                active_evaluations[request_id_str].status.state = "running"

        # Execute evaluations
        results = await executor.execute_evaluation_request(
            request, progress_callback_sync, mlflow_client if experiment_id else None
        )

        # Log results to MLFlow only if experiment_id is provided
        if experiment_id is not None:
            for result in results:
                if result.mlflow_run_id:
                    await mlflow_client.log_evaluation_result(result)

        # Build final response
        final_response = await response_builder.build_job_resource_response(
            request.request_id, job_request, results, experiment_url
        )

        # Update stored response
        active_evaluations[request_id_str] = final_response

        logger.info(
            "Completed async evaluation",
            request_id=request_id_str,
            total_results=len(results),
            successful_results=len(
                [r for r in results if r.status == EvaluationStatus.COMPLETED]
            ),
        )

    except Exception as e:
        logger.error("Async evaluation failed", request_id=request_id_str, error=str(e))

        # Update response with error
        if request_id_str in active_evaluations:
            response = active_evaluations[request_id_str]
            response.status.state = "failed"
            response.status.message = str(e)
            response.resource.updated_at = get_utc_now()

    finally:
        # Clean up task reference
        if request_id_str in evaluation_tasks:
            del evaluation_tasks[request_id_str]


async def _execute_evaluation_sync(
    request: EvaluationRequest,
    experiment_id: str | None,
    executor: EvaluationExecutor,
    mlflow_client: MLFlowClient,
) -> list[EvaluationResult]:
    """Execute evaluation synchronously."""
    # Execute evaluations
    results = await executor.execute_evaluation_request(
        request, None, mlflow_client if experiment_id else None
    )

    # Log results to MLFlow only if experiment_id is provided
    if experiment_id is not None:
        for result in results:
            if result.mlflow_run_id:
                await mlflow_client.log_evaluation_result(result)

    return results
