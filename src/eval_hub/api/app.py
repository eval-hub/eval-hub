"""FastAPI application factory."""

import time
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request, Response
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from prometheus_client import CONTENT_TYPE_LATEST, Counter, Histogram, generate_latest

from ..core.config import get_settings
from ..core.exceptions import EvaluationServiceError, ExecutionError, ValidationError
from ..core.logging import get_logger, log_request_end, log_request_start, setup_logging
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
    async def logging_middleware(request: Request, call_next):
        """Middleware for request logging and metrics."""
        start_time = time.time()
        request_id = request.headers.get("X-Request-ID", "unknown")

        # Log request start
        log_request_start(logger, request_id, str(request.url.path), request.method)

        # Process request
        response = await call_next(request)

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
    async def validation_error_handler(request: Request, exc: ValidationError):
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

    @app.exception_handler(ExecutionError)
    async def execution_error_handler(request: Request, exc: ExecutionError):
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
    ):
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
    async def general_exception_handler(request: Request, exc: Exception):
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
    async def metrics():
        """Prometheus metrics endpoint."""
        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)

    # Include API routes
    app.include_router(router, prefix=settings.api_prefix)

    logger.info(
        "FastAPI application created",
        title=settings.app_name,
        version=settings.version,
        debug=settings.debug,
    )

    return app
