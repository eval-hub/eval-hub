"""FastAPI application factory."""

import time
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager
from typing import Any

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.openapi.utils import get_openapi
from fastapi.responses import JSONResponse
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)
from structlog.typing import FilteringBoundLogger

from ..core.config import get_settings
from ..core.exceptions import EvaluationServiceError, ExecutionError, ValidationError
from ..core.logging import get_logger, log_request_end, log_request_start, setup_logging
from ..models.evaluation import Page, Patch
from ..services.provider_service import ProviderService
from .routes import router

# Prometheus metrics
REQUEST_COUNT = Counter(
    "http_requests_total", "Total HTTP requests", ["method", "endpoint", "status_code"]
)

REQUEST_DURATION = Histogram(
    "http_request_duration_seconds",
    "HTTP request duration in seconds",
    ["method", "endpoint"],
)

MLFLOW_HEALTH = Gauge(
    "mlflow_health_status",
    "MLflow tracking server health status (1 = healthy, 0 = unhealthy)",
)


def custom_openapi() -> dict[str, Any]:
    """Custom OpenAPI generator to ensure base schemas are included."""
    if app.openapi_schema:
        return app.openapi_schema

    openapi_schema = get_openapi(
        title=app.title,
        version=app.version,
        description=app.description,
        routes=app.routes,
    )

    # Ensure base schemas used only through inheritance are included
    if "components" not in openapi_schema:
        openapi_schema["components"] = {}
    if "schemas" not in openapi_schema["components"]:
        openapi_schema["components"]["schemas"] = {}

    # Include Page and Patch schemas
    openapi_schema["components"]["schemas"]["Page"] = Page.model_json_schema()
    openapi_schema["components"]["schemas"]["Patch"] = Patch.model_json_schema()

    app.openapi_schema = openapi_schema
    return openapi_schema


async def check_mlflow_health(tracking_uri: str, logger: FilteringBoundLogger) -> bool:
    """Check MLflow tracking server health by making an HTTP request to /health endpoint."""
    try:
        base_url = tracking_uri.rstrip("/")
        health_url = f"{base_url}/health"

        async with httpx.AsyncClient(timeout=5.0) as client:
            response = await client.get(health_url)
            is_healthy = response.status_code == 200
            if not is_healthy:
                logger.warning(
                    "MLflow health check failed",
                    status_code=response.status_code,
                    tracking_uri=tracking_uri,
                )
            return is_healthy
    except Exception as e:
        logger.warning(
            "MLflow health check error",
            error=str(e),
            error_type=type(e).__name__,
            tracking_uri=tracking_uri,
        )
        return False


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    """Application lifespan manager."""
    settings = get_settings()
    logger = get_logger(__name__)

    # Startup
    logger.info(
        "Starting evaluation service", version=settings.version, debug=settings.debug
    )

    # Set startup time for health checks
    app.state.start_time = time.time()

    # Initialize and load provider service at startup
    provider_service = ProviderService(settings)
    provider_service.initialize()
    app.state.provider_service = provider_service
    logger.info("Provider service initialized and loaded into memory")

    yield

    # Shutdown
    logger.info("Shutting down evaluation service")


def create_app() -> FastAPI:
    """Create and configure the FastAPI application."""
    settings = get_settings()

    # Set up logging
    setup_logging(settings)
    logger = get_logger(__name__)

    # Create FastAPI app
    app = FastAPI(
        title=settings.app_name,
        version=settings.version,
        description="API REST server for evaluation backend orchestration on OpenShift",
        docs_url=settings.docs_url if not settings.debug else "/docs",
        redoc_url=settings.redoc_url if not settings.debug else "/redoc",
        openapi_url=settings.openapi_url if not settings.debug else "/openapi.json",
        lifespan=lifespan,
    )

    # Add CORS middleware
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],  # Configure appropriately for production
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    # Add request logging middleware
    @app.middleware("http")
    async def logging_middleware(request: Request, call_next) -> Response:  # type: ignore[no-untyped-def]
        """Middleware for request logging and metrics."""
        start_time = time.time()
        request_id = request.headers.get("X-Request-ID", "unknown")

        # Log request start
        log_request_start(logger, request_id, str(request.url.path), request.method)

        # Process request
        response: Response = await call_next(request)

        # Calculate duration
        duration = time.time() - start_time
        duration_ms = duration * 1000

        # Log request end
        log_request_end(logger, request_id, response.status_code, duration_ms)

        # Update metrics
        REQUEST_COUNT.labels(
            method=request.method,
            endpoint=request.url.path,
            status_code=response.status_code,
        ).inc()

        REQUEST_DURATION.labels(
            method=request.method, endpoint=request.url.path
        ).observe(duration)

        return response

    # Add exception handlers
    @app.exception_handler(ValidationError)
    async def validation_error_handler(
        request: Request, exc: ValidationError
    ) -> JSONResponse:
        """Handle validation errors."""
        logger.warning(
            "Validation error",
            error=exc.message,
            details=exc.details,
            path=request.url.path,
        )
        return JSONResponse(
            status_code=400,
            content={
                "error": "Validation Error",
                "message": exc.message,
                "details": exc.details,
                "type": "validation_error",
            },
        )

    # Custom handler for FastAPI request validation errors (422)
    @app.exception_handler(RequestValidationError)
    async def request_validation_error_handler(
        request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        """Handle FastAPI request validation errors with custom Error schema."""
        logger.warning(
            "Request validation error",
            errors=exc.errors(),
            path=request.url.path,
        )
        # Format according to our Error schema
        return JSONResponse(
            status_code=422,
            content={
                "code": "validation_error",
                "message": f"Validation failed: {len(exc.errors())} error(s)",
                "type": "RequestValidationError",
            },
        )

    @app.exception_handler(ExecutionError)
    async def execution_error_handler(
        request: Request, exc: ExecutionError
    ) -> JSONResponse:
        """Handle execution errors."""
        logger.error(
            "Execution error",
            error=exc.message,
            details=exc.details,
            path=request.url.path,
        )
        return JSONResponse(
            status_code=500,
            content={
                "error": "Execution Error",
                "message": exc.message,
                "details": exc.details,
                "type": "execution_error",
            },
        )

    @app.exception_handler(EvaluationServiceError)
    async def evaluation_service_error_handler(
        request: Request, exc: EvaluationServiceError
    ) -> JSONResponse:
        """Handle general evaluation service errors."""
        logger.error(
            "Service error",
            error=exc.message,
            details=exc.details,
            path=request.url.path,
        )
        return JSONResponse(
            status_code=500,
            content={
                "error": "Service Error",
                "message": exc.message,
                "details": exc.details,
                "type": "service_error",
            },
        )

    @app.exception_handler(Exception)
    async def general_exception_handler(
        request: Request, exc: Exception
    ) -> JSONResponse:
        """Handle unexpected exceptions."""
        logger.error(
            "Unexpected error",
            error=str(exc),
            error_type=type(exc).__name__,
            path=request.url.path,
        )
        return JSONResponse(
            status_code=500,
            content={
                "error": "Internal Server Error",
                "message": "An unexpected error occurred",
                "type": "internal_error",
            },
        )

    # Add metrics endpoint
    @app.get("/metrics")
    async def metrics() -> Response:
        """Prometheus metrics endpoint with MLflow health check."""
        settings = get_settings()

        # Perform MLflow health check
        mlflow_healthy = await check_mlflow_health(settings.mlflow_tracking_uri, logger)
        MLFLOW_HEALTH.set(1 if mlflow_healthy else 0)

        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)

    # Include API routes
    app.include_router(router, prefix=settings.api_prefix)

    # Set custom OpenAPI generator
    app.openapi = custom_openapi  # type: ignore[method-assign]

    logger.info(
        "FastAPI application created",
        title=settings.app_name,
        version=settings.version,
        docs_url=app.docs_url,
    )

    return app


# Create app instance for uvicorn to find
app = create_app()
