"""API routes for the evaluation service."""

import asyncio
import time
from typing import Any
from uuid import UUID

from fastapi import (
    APIRouter,
    BackgroundTasks,
    Depends,
    HTTPException,
    Query,
    Request,
    status,
)
from fastapi.responses import JSONResponse

from ..core.config import Settings, get_settings
from ..core.exceptions import ValidationError
from ..core.logging import get_logger
from ..models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkConfig,
    BenchmarkSpec,
    EvaluationRequest,
    EvaluationResponse,
    EvaluationResult,
    EvaluationSpec,
    EvaluationStatus,
    ExperimentConfig,
    SimpleEvaluationRequest,
)
from ..models.health import HealthResponse
from ..models.provider import (
    Collection,
    CollectionCreationRequest,
    CollectionUpdateRequest,
    ListBenchmarksResponse,
    ListCollectionsResponse,
    ListProvidersResponse,
    Provider,
)
from ..services.executor import EvaluationExecutor
from ..services.mlflow_client import MLFlowClient
from ..services.parser import RequestParser
from ..services.provider_service import ProviderService
from ..services.response_builder import ResponseBuilder
from ..utils import utcnow

router = APIRouter()
logger = get_logger(__name__)

# Global state for active evaluations and services
active_evaluations: dict[str, EvaluationResponse] = {}
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
    response_model=EvaluationResponse,
    status_code=status.HTTP_202_ACCEPTED,
    tags=["Evaluations"],
)
async def create_evaluation(
    request: SimpleEvaluationRequest,
    background_tasks: BackgroundTasks,
    parser: RequestParser = Depends(get_request_parser),
    executor: EvaluationExecutor = Depends(get_evaluation_executor),
    mlflow_client: MLFlowClient = Depends(get_mlflow_client),
    response_builder: ResponseBuilder = Depends(get_response_builder),
    settings: Settings = Depends(get_settings),
    provider_service: ProviderService = Depends(get_provider_service),
) -> EvaluationResponse:
    """Create and execute evaluation request using the simplified benchmark schema."""
    logger.info(
        "Received evaluation request",
        request_id=str(request.request_id),
        benchmark_count=len(request.benchmarks),
        experiment_name=request.experiment.name,
        async_mode=request.async_mode,
    )

    try:
        # Validate benchmarks exist
        for benchmark in request.benchmarks:
            benchmark_detail = provider_service.get_benchmark_by_id(
                benchmark.provider_id, benchmark.benchmark_id
            )
            if not benchmark_detail:
                raise HTTPException(
                    status_code=status.HTTP_404_NOT_FOUND,
                    detail=f"Benchmark {benchmark.provider_id}::{benchmark.benchmark_id} not found",
                )

        # Convert SimpleEvaluationRequest to legacy EvaluationRequest format for processing
        # Group benchmarks by provider to create backend specs
        provider_benchmarks: dict[str, list[BenchmarkConfig]] = {}
        for benchmark in request.benchmarks:
            if benchmark.provider_id not in provider_benchmarks:
                provider_benchmarks[benchmark.provider_id] = []
            provider_benchmarks[benchmark.provider_id].append(benchmark)

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
            for benchmark in benchmarks:
                # Get benchmark details for additional info
                benchmark_detail = provider_service.get_benchmark_by_id(
                    benchmark.provider_id, benchmark.benchmark_id
                )

                benchmark_spec = BenchmarkSpec(
                    name=benchmark.benchmark_id,
                    tasks=[benchmark.benchmark_id],
                    num_fewshot=benchmark.config.get("num_fewshot"),
                    batch_size=benchmark.config.get("batch_size"),
                    limit=benchmark.config.get("limit"),
                    device=benchmark.config.get("device"),
                    config=benchmark.config,
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

        evaluation_spec = EvaluationSpec(
            name=request.experiment.name or f"Evaluation-{request.request_id.hex[:8]}",
            description=f"Evaluation with {len(request.benchmarks)} benchmarks",
            model=request.model,
            backends=backend_specs,
            risk_category=None,
            collection_id=None,
            timeout_minutes=request.timeout_minutes,
            retry_attempts=request.retry_attempts,
        )

        # Create legacy evaluation request
        legacy_request = EvaluationRequest(
            request_id=request.request_id,
            evaluations=[evaluation_spec],
            experiment=request.experiment,
            async_mode=request.async_mode,
            callback_url=request.callback_url,
            created_at=request.created_at,
        )

        # Use existing evaluation processing logic
        parsed_request = await parser.parse_evaluation_request(
            legacy_request, provider_service
        )

        # Create MLFlow experiment
        experiment_id = await mlflow_client.create_experiment(parsed_request)
        experiment_url = await mlflow_client.get_experiment_url(experiment_id)

        if request.async_mode:
            # Initialize response for async execution
            initial_response = await response_builder.build_response(
                parsed_request,
                [],  # No results yet
                experiment_url,
            )
            initial_response.status = EvaluationStatus.PENDING

            # Calculate total number of benchmark evaluations
            initial_response.total_evaluations = len(request.benchmarks)

            # Store in active evaluations
            active_evaluations[str(request.request_id)] = initial_response

            # Start background task
            task = asyncio.create_task(
                _execute_evaluation_async(
                    legacy_request,
                    experiment_id,
                    experiment_url,
                    executor,
                    mlflow_client,
                    response_builder,
                )
            )
            evaluation_tasks[str(request.request_id)] = task

            return initial_response

        else:
            # Synchronous execution
            results = await _execute_evaluation_sync(
                legacy_request, experiment_id, executor, mlflow_client
            )

            # Build final response
            response = await response_builder.build_response(
                legacy_request, results, experiment_url
            )

            return response

    except ValidationError as e:
        logger.error(
            "Validation failed for evaluation request",
            request_id=str(request.request_id),
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Validation error: {e.message}",
        ) from e

    except Exception as e:
        logger.error(
            "Failed to create evaluation",
            request_id=str(request.request_id),
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to create evaluation: {str(e)}",
        ) from e


@router.get(
    "/evaluations/jobs/{id}", response_model=EvaluationResponse, tags=["Evaluations"]
)
async def get_evaluation_status(
    id: UUID,
    response_builder: ResponseBuilder = Depends(get_response_builder),
) -> EvaluationResponse:
    """Get the status of an evaluation request."""
    request_id_str = str(id)

    if request_id_str not in active_evaluations:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Evaluation request {id} not found",
        )

    response = active_evaluations[request_id_str]

    logger.info(
        "Retrieved evaluation status",
        request_id=request_id_str,
        status=response.status,
        progress=response.progress_percentage,
    )

    return response


@router.get(
    "/evaluations/jobs", response_model=list[EvaluationResponse], tags=["Evaluations"]
)
async def list_evaluations(
    limit: int = Query(
        50, ge=1, le=100, description="Maximum number of evaluations to return"
    ),
    status_filter: str | None = Query(None, description="Filter by status"),
) -> list[EvaluationResponse]:
    """List all evaluation requests."""
    evaluations = list(active_evaluations.values())

    # Apply status filter
    if status_filter:
        evaluations = [e for e in evaluations if e.status == status_filter]

    # Apply limit
    evaluations = evaluations[:limit]

    logger.info(
        "Listed evaluations",
        total_count=len(active_evaluations),
        returned_count=len(evaluations),
        status_filter=status_filter,
    )

    return evaluations


@router.delete("/evaluations/jobs/{id}", tags=["Evaluations"])
async def cancel_evaluation(
    id: UUID,
    executor: EvaluationExecutor = Depends(get_evaluation_executor),
) -> JSONResponse:
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
    response.status = EvaluationStatus.CANCELLED
    response.updated_at = utcnow()

    logger.info("Cancelled evaluation", request_id=request_id_str)

    return JSONResponse(
        status_code=status.HTTP_200_OK,
        content={"message": f"Evaluation {id} cancelled successfully"},
    )


@router.get("/evaluations/jobs/{id}/summary", tags=["Evaluations"])
async def get_evaluation_summary(
    id: UUID,
    response_builder: ResponseBuilder = Depends(get_response_builder),
) -> dict[str, Any]:
    """Get a summary of an evaluation request."""
    request_id_str = str(id)

    if request_id_str not in active_evaluations:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Evaluation request {id} not found",
        )

    response = active_evaluations[request_id_str]

    # Create a mock request for summary building
    # In a real implementation, you'd store the original request
    mock_request = EvaluationRequest(
        request_id=id,
        evaluations=[],  # Would be populated from stored data
        created_at=response.created_at,
        experiment=ExperimentConfig(name="mock-experiment"),
        callback_url=None,
    )

    summary = await response_builder.build_summary_response(
        mock_request, response.results
    )

    logger.info("Generated evaluation summary", request_id=request_id_str)

    return summary


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
                len(r.results) for r in active_evaluations.values()
            )
            * 0.01,
        },
        "status_breakdown": {},
    }

    # Count evaluations by status
    status_breakdown: dict[str, int] = {}
    for response in active_evaluations.values():
        status_str = response.status.value
        status_breakdown[status_str] = status_breakdown.get(status_str, 0) + 1
    metrics["status_breakdown"] = status_breakdown

    return metrics


# Provider and Benchmark Management Endpoints


@router.get(
    "/evaluations/providers", response_model=ListProvidersResponse, tags=["Providers"]
)
async def list_providers(
    provider_service: ProviderService = Depends(get_provider_service),
) -> ListProvidersResponse:
    """List all registered evaluation providers."""
    logger.info("Listing all evaluation providers")
    return provider_service.get_all_providers()


@router.get(
    "/evaluations/providers/{provider_id}", response_model=Provider, tags=["Providers"]
)
async def get_provider(
    provider_id: str,
    provider_service: ProviderService = Depends(get_provider_service),
) -> Provider:
    """Get details of a specific provider."""
    provider = provider_service.get_provider_by_id(provider_id)
    if not provider:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Provider {provider_id} not found",
        )

    logger.info("Retrieved provider details", provider_id=provider_id)
    return provider


@router.get(
    "/evaluations/benchmarks",
    response_model=ListBenchmarksResponse,
    tags=["Benchmarks"],
)
async def list_all_benchmarks(
    provider_id: str | None = Query(None, description="Filter by provider ID"),
    category: str | None = Query(None, description="Filter by benchmark category"),
    tags: str | None = Query(None, description="Filter by tags (comma-separated)"),
    provider_service: ProviderService = Depends(get_provider_service),
) -> ListBenchmarksResponse:
    """List all available benchmarks across providers (similar to Llama Stack format)."""
    logger.info(
        "Listing benchmarks", provider_id=provider_id, category=category, tags=tags
    )

    if provider_id or category or tags:
        # Apply filters using search functionality
        tag_list = tags.split(",") if tags else None
        filtered_benchmarks = provider_service.search_benchmarks(
            category=category, provider_id=provider_id, tags=tag_list
        )

        # Convert to Llama Stack format
        benchmarks = []
        for benchmark_detail in filtered_benchmarks:
            benchmark_dict = {
                "benchmark_id": f"{benchmark_detail.provider_id}::{benchmark_detail.benchmark_id}",
                "provider_id": benchmark_detail.provider_id,
                "name": benchmark_detail.name,
                "description": benchmark_detail.description,
                "category": benchmark_detail.category,
                "metrics": benchmark_detail.metrics,
                "num_few_shot": benchmark_detail.num_few_shot,
                "dataset_size": benchmark_detail.dataset_size,
                "tags": benchmark_detail.tags,
            }
            benchmarks.append(benchmark_dict)

        provider_ids: list[str] = [
            str(b["provider_id"]) for b in benchmarks if "provider_id" in b
        ]

        return ListBenchmarksResponse(
            benchmarks=benchmarks,
            total_count=len(benchmarks),
            providers_included=list(set(provider_ids)),
        )
    else:
        # Return all benchmarks
        return provider_service.get_all_benchmarks()


@router.get(
    "/evaluations/collections",
    response_model=ListCollectionsResponse,
    tags=["Collections"],
)
async def list_collections(
    provider_service: ProviderService = Depends(get_provider_service),
) -> ListCollectionsResponse:
    """List all benchmark collections."""
    logger.info("Listing all benchmark collections")
    return provider_service.get_all_collections()


@router.get(
    "/evaluations/collections/{collection_id}",
    response_model=Collection,
    tags=["Collections"],
)
async def get_collection(
    collection_id: str,
    provider_service: ProviderService = Depends(get_provider_service),
) -> Collection:
    """Get details of a specific collection."""
    collection = provider_service.get_collection_by_id(collection_id)
    if not collection:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Collection {collection_id} not found",
        )

    logger.info("Retrieved collection details", collection_id=collection_id)
    return collection


@router.post(
    "/evaluations/collections",
    response_model=Collection,
    status_code=status.HTTP_201_CREATED,
    tags=["Collections"],
)
async def create_collection(
    request: CollectionCreationRequest,
    provider_service: ProviderService = Depends(get_provider_service),
) -> Collection:
    """Create a new collection."""
    try:
        collection = provider_service.create_collection(request)
        logger.info(
            "Collection created successfully",
            collection_id=request.collection_id,
        )
        return collection
    except ValueError as e:
        logger.error(
            "Failed to create collection",
            collection_id=request.collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error creating collection",
            collection_id=request.collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to create collection: {str(e)}",
        ) from e


@router.put(
    "/evaluations/collections/{collection_id}",
    response_model=Collection,
    tags=["Collections"],
)
async def update_collection(
    collection_id: str,
    request: CollectionUpdateRequest,
    provider_service: ProviderService = Depends(get_provider_service),
) -> Collection:
    """Update an existing collection."""
    try:
        collection = provider_service.update_collection(collection_id, request)
        if not collection:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Collection {collection_id} not found",
            )

        logger.info("Collection updated successfully", collection_id=collection_id)
        return collection
    except ValueError as e:
        logger.error(
            "Failed to update collection",
            collection_id=collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error updating collection",
            collection_id=collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to update collection: {str(e)}",
        ) from e


@router.patch(
    "/evaluations/collections/{collection_id}",
    response_model=Collection,
    tags=["Collections"],
)
async def patch_collection(
    collection_id: str,
    request: CollectionUpdateRequest,
    provider_service: ProviderService = Depends(get_provider_service),
) -> Collection:
    """Partially update an existing collection."""
    try:
        collection = provider_service.update_collection(collection_id, request)
        if not collection:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Collection {collection_id} not found",
            )

        logger.info("Collection patched successfully", collection_id=collection_id)
        return collection
    except ValueError as e:
        logger.error(
            "Failed to patch collection", collection_id=collection_id, error=str(e)
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error patching collection",
            collection_id=collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to patch collection: {str(e)}",
        ) from e


@router.delete("/evaluations/collections/{collection_id}", tags=["Collections"])
async def delete_collection(
    collection_id: str,
    provider_service: ProviderService = Depends(get_provider_service),
) -> JSONResponse:
    """Delete a collection."""
    try:
        success = provider_service.delete_collection(collection_id)
        if not success:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Collection {collection_id} not found",
            )

        logger.info("Collection deleted successfully", collection_id=collection_id)
        return JSONResponse(
            status_code=status.HTTP_200_OK,
            content={"message": f"Collection {collection_id} deleted successfully"},
        )
    except ValueError as e:
        logger.error(
            "Failed to delete collection",
            collection_id=collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=str(e),
        ) from e
    except Exception as e:
        logger.error(
            "Unexpected error deleting collection",
            collection_id=collection_id,
            error=str(e),
        )
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to delete collection: {str(e)}",
        ) from e


async def _execute_evaluation_async(
    request: EvaluationRequest,
    experiment_id: str,
    experiment_url: str,
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
                response = active_evaluations[request_id_str]
                response.updated_at = utcnow()
                # In a real implementation, you'd update individual evaluation progress

        # Execute evaluations
        results = await executor.execute_evaluation_request(
            request, progress_callback_sync, mlflow_client
        )

        # Log results to MLFlow
        for result in results:
            if result.mlflow_run_id:
                await mlflow_client.log_evaluation_result(result)

        # Build final response
        final_response = await response_builder.build_response(
            request, results, experiment_url
        )

        # Update stored response
        active_evaluations[request_id_str] = final_response

        logger.info(
            "Completed async evaluation",
            request_id=request_id_str,
            total_results=len(results),
            successful_results=len([r for r in results if r.status == "completed"]),
        )

    except Exception as e:
        logger.error("Async evaluation failed", request_id=request_id_str, error=str(e))

        # Update response with error
        if request_id_str in active_evaluations:
            response = active_evaluations[request_id_str]
            response.status = EvaluationStatus.FAILED
            response.updated_at = utcnow()

    finally:
        # Clean up task reference
        if request_id_str in evaluation_tasks:
            del evaluation_tasks[request_id_str]


async def _execute_evaluation_sync(
    request: EvaluationRequest,
    experiment_id: str,
    executor: EvaluationExecutor,
    mlflow_client: MLFlowClient,
) -> list[EvaluationResult]:
    """Execute evaluation synchronously."""
    # Execute evaluations
    results = await executor.execute_evaluation_request(request, None, mlflow_client)

    # Log results to MLFlow
    for result in results:
        if result.mlflow_run_id:
            await mlflow_client.log_evaluation_result(result)

    return results
