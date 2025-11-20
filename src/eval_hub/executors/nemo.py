"""NeMo Evaluator executor for remote Evaluator container integration."""

import asyncio
import json
from collections.abc import Callable
from pathlib import Path
from typing import Any

import httpx

from ..core.exceptions import BackendError
from ..core.logging import get_logger
from ..models.evaluation import EvaluationResult, EvaluationStatus
from ..models.nemo import (
    EndpointType,
    NemoApiEndpoint,
    NemoConfigParams,
    NemoContainerConfig,
    NemoEvaluationConfig,
    NemoEvaluationRequest,
    NemoEvaluationResult,
    NemoEvaluationTarget,
)
from ..utils import safe_duration_seconds, utcnow
from .base import ExecutionContext, Executor


class NemoEvaluatorExecutor(Executor):
    """Executor for communicating with remote NeMo Evaluator containers."""

    def __init__(self, backend_config: dict[str, Any]):
        self.logger = get_logger(__name__)
        super().__init__(backend_config)

        # Create container configuration from backend config
        self.container_config = NemoContainerConfig(
            endpoint=backend_config.get("endpoint", "localhost"),
            port=backend_config.get("port", 3825),
            timeout_seconds=backend_config.get("timeout_seconds", 3600),
            max_retries=backend_config.get("max_retries", 3),
            health_check_endpoint=backend_config.get("health_check_endpoint"),
            auth_token=backend_config.get("auth_token"),
            verify_ssl=backend_config.get("verify_ssl", True),
        )

        self.base_url = self.container_config.get_full_endpoint()

    def _validate_config(self) -> None:
        """Validate NeMo Evaluator configuration."""
        required_fields = ["endpoint"]

        for field in required_fields:
            if field not in self.backend_config:
                raise ValueError(
                    f"NeMo Evaluator configuration missing required field: {field}"
                )

        # Additional validation
        endpoint = self.backend_config.get("endpoint")
        if not endpoint or not isinstance(endpoint, str):
            raise ValueError("NeMo Evaluator endpoint must be a non-empty string")

    @classmethod
    def get_backend_type(cls) -> str:
        """Get the backend type identifier."""
        return "nemo-evaluator"

    async def health_check(self) -> bool:
        """Check if the NeMo Evaluator container is healthy and responsive."""
        try:
            health_url = self.container_config.get_health_check_url()
            if not health_url:
                # If no specific health check endpoint, try a simple ping to the base URL
                health_url = f"{self.base_url}/"

            async with httpx.AsyncClient(
                timeout=10.0, verify=self.container_config.verify_ssl
            ) as client:
                # NeMo Evaluator server expects POST requests
                response = await client.post(
                    health_url,
                    json={"ping": "health_check"},
                    headers=self._get_headers(),
                )

                # If we get any response (even an error), the container is responding
                return response.status_code < 500

        except Exception as e:
            self.logger.warning(
                "Health check failed for NeMo Evaluator container",
                endpoint=self.base_url,
                error=str(e),
            )
            return False

    async def execute_benchmark(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Execute a benchmark evaluation on the remote NeMo Evaluator container."""

        self.logger.info(
            "Starting NeMo Evaluator execution",
            evaluation_id=str(context.evaluation_id),
            endpoint=self.base_url,
            benchmark=context.benchmark_spec.name,
        )

        try:
            # Report progress start
            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    0.0,
                    f"Preparing {context.benchmark_spec.name} for NeMo Evaluator",
                )

            # Prepare the evaluation request
            nemo_request = await self._build_nemo_evaluation_request(context)

            # Report progress: request prepared
            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    10.0,
                    f"Sending {context.benchmark_spec.name} to NeMo Evaluator",
                )

            # Execute with retries
            result = await self._execute_with_retries(
                nemo_request, progress_callback, context
            )

            # Report completion
            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    100.0,
                    f"Completed {context.benchmark_spec.name} on NeMo Evaluator",
                )

            # Convert NeMo result to eval-hub format
            eval_result = await self._convert_nemo_result_to_eval_hub(result, context)

            self.logger.info(
                "NeMo Evaluator execution completed",
                evaluation_id=str(context.evaluation_id),
                benchmark=context.benchmark_spec.name,
                status=eval_result.status,
            )

            return eval_result

        except Exception as e:
            self.logger.error(
                "NeMo Evaluator execution failed",
                evaluation_id=str(context.evaluation_id),
                benchmark=context.benchmark_spec.name,
                error=str(e),
            )

            return EvaluationResult(
                evaluation_id=context.evaluation_id,
                provider_id="nemo-evaluator",
                benchmark_id=context.benchmark_spec.name,
                benchmark_name=context.benchmark_spec.name,
                status=EvaluationStatus.FAILED,
                error_message=str(e),
                started_at=context.started_at,
                completed_at=utcnow(),
                duration_seconds=safe_duration_seconds(utcnow(), context.started_at),
                mlflow_run_id=None,
            )

    async def cleanup(self) -> None:
        """Perform post-evaluation cleanup."""
        # Trigger post-evaluation hooks if configured
        if self.backend_config.get("run_post_hooks", False):
            try:
                await self._run_post_eval_hooks()
            except Exception as e:
                self.logger.warning(
                    "Failed to run post-evaluation hooks during cleanup", error=str(e)
                )

    async def _build_nemo_evaluation_request(
        self, context: ExecutionContext
    ) -> NemoEvaluationRequest:
        """Build a NeMo Evaluator request from eval-hub parameters."""

        # Create output directory
        output_dir = (
            f"/tmp/nemo_eval_{context.evaluation_id}_{context.benchmark_spec.name}"
        )
        Path(output_dir).mkdir(parents=True, exist_ok=True)

        # Build API endpoint configuration
        # Use model server base URL if available, otherwise fall back to config or default
        model_endpoint = (
            context.model_url
            or self.backend_config.get("model_endpoint")
            or "http://localhost:8000"
        )
        api_endpoint = NemoApiEndpoint(
            url=model_endpoint,
            model_id=context.model_name,
            type=EndpointType(self.backend_config.get("endpoint_type", "chat")),
            api_key=self.backend_config.get("api_key_env"),
            stream=self.backend_config.get("stream", False),
        )

        # Build configuration parameters
        config_params = NemoConfigParams(
            limit_samples=self.backend_config.get("limit_samples")
            or context.benchmark_spec.config.get("limit"),
            max_new_tokens=self.backend_config.get("max_new_tokens", 512),
            max_retries=self.backend_config.get("max_retries", 3),
            parallelism=self.backend_config.get("parallelism", 1),
            task=context.benchmark_spec.name,
            temperature=self.backend_config.get("temperature", 0.0),
            request_timeout=self.backend_config.get("request_timeout", 60),
            top_p=self.backend_config.get("top_p", 0.95),
            extra=self.backend_config.get("extra", {}),
        )

        # Build evaluation configuration
        eval_config = NemoEvaluationConfig(
            output_dir=output_dir,
            params=config_params,
            type=context.benchmark_spec.name,
            supported_endpoint_types=(
                [api_endpoint.type.value] if api_endpoint.type else None
            ),
        )

        # Build target configuration
        target_config = NemoEvaluationTarget(api_endpoint=api_endpoint)

        # Build complete request
        nemo_request = NemoEvaluationRequest(
            command=self.backend_config.get("command", "evaluate {{ config.type }}"),
            framework_name=self.backend_config.get("framework_name", "eval-hub"),
            pkg_name=context.benchmark_spec.name,
            config=eval_config,
            target=target_config,
        )

        return nemo_request

    async def _execute_with_retries(
        self,
        nemo_request: NemoEvaluationRequest,
        progress_callback: Callable[[str, float, str], None] | None,
        context: ExecutionContext,
    ) -> NemoEvaluationResult:
        """Execute the NeMo evaluation with retry logic."""

        last_error = None

        for attempt in range(self.container_config.max_retries + 1):
            try:
                if attempt > 0:
                    self.logger.info(
                        "Retrying NeMo Evaluator request",
                        evaluation_id=str(context.evaluation_id),
                        attempt=attempt + 1,
                        max_attempts=self.container_config.max_retries + 1,
                    )

                    # Exponential backoff
                    wait_time = 2**attempt
                    await asyncio.sleep(wait_time)

                result = await self._make_evaluation_request(
                    nemo_request, progress_callback, context
                )
                return result

            except Exception as e:
                last_error = e
                self.logger.warning(
                    "NeMo Evaluator request attempt failed",
                    evaluation_id=str(context.evaluation_id),
                    attempt=attempt + 1,
                    error=str(e),
                )

        # All retries exhausted
        raise BackendError(
            f"NeMo Evaluator failed after {self.container_config.max_retries + 1} attempts: {last_error}"
        )

    async def _make_evaluation_request(
        self,
        nemo_request: NemoEvaluationRequest,
        progress_callback: Callable[[str, float, str], None] | None,
        context: ExecutionContext,
    ) -> NemoEvaluationResult:
        """Make the actual HTTP request to the NeMo Evaluator container."""

        async with httpx.AsyncClient(
            timeout=self.container_config.timeout_seconds,
            verify=self.container_config.verify_ssl,
        ) as client:
            # Report progress: sending request
            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    20.0,
                    "Sending evaluation request to NeMo Evaluator",
                )

            # NeMo Evaluator expects POST to any path
            response = await client.post(
                f"{self.base_url}/evaluate",
                json=nemo_request.model_dump(),
                headers=self._get_headers(),
            )

            # Report progress: request sent
            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    40.0,
                    "NeMo Evaluator processing request",
                )

            if response.status_code >= 400:
                error_msg = (
                    f"NeMo Evaluator returned {response.status_code}: {response.text}"
                )
                raise BackendError(error_msg)

            # Report progress: processing response
            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    80.0,
                    "Processing NeMo Evaluator response",
                )

            # Parse the response
            try:
                result_data = response.json()
                result = NemoEvaluationResult(**result_data)
                return result
            except Exception as e:
                raise BackendError(
                    f"Failed to parse NeMo Evaluator response: {e}"
                ) from e

    async def _convert_nemo_result_to_eval_hub(
        self, nemo_result: NemoEvaluationResult, context: ExecutionContext
    ) -> EvaluationResult:
        """Convert NeMo Evaluator result to eval-hub EvaluationResult format."""

        metrics = {}
        artifacts = {}

        # Extract metrics from task results
        if nemo_result.tasks:
            for task_name, task_result in nemo_result.tasks.items():
                for metric_name, metric_result in task_result.metrics.items():
                    for score_name, score in metric_result.scores.items():
                        # Flatten the metric name
                        full_metric_name = (
                            f"{task_name}_{metric_name}_{score_name}"
                            if len(nemo_result.tasks) > 1
                            else f"{metric_name}_{score_name}"
                        )
                        metrics[full_metric_name] = score.value

                        # Also include statistics as separate metrics
                        if score.stats.mean is not None:
                            metrics[f"{full_metric_name}_mean"] = score.stats.mean
                        if score.stats.stddev is not None:
                            metrics[f"{full_metric_name}_stddev"] = score.stats.stddev

        # Extract metrics from group results
        if nemo_result.groups:
            for group_name, group_result in nemo_result.groups.items():
                for metric_name, metric_result in group_result.metrics.items():
                    for score_name, score in metric_result.scores.items():
                        # Flatten the metric name
                        full_metric_name = f"{group_name}_{metric_name}_{score_name}"
                        metrics[full_metric_name] = score.value

        # Add some default artifacts
        artifacts[
            "nemo_evaluator_response"
        ] = f"/tmp/nemo_eval_{context.evaluation_id}_{context.benchmark_spec.name}_response.json"

        # Save the full response for debugging
        with open(artifacts["nemo_evaluator_response"], "w") as f:
            json.dump(nemo_result.model_dump(), f, indent=2, default=str)

        metrics_typed: dict[str, float | int | str] = dict(metrics.items())

        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id="nemo-evaluator",
            benchmark_id=context.benchmark_spec.name,
            benchmark_name=context.benchmark_spec.name,
            status=EvaluationStatus.COMPLETED,
            metrics=metrics_typed,
            artifacts=artifacts,
            started_at=context.started_at,
            completed_at=utcnow(),
            duration_seconds=safe_duration_seconds(utcnow(), context.started_at),
            error_message=None,
            mlflow_run_id=None,
        )

    def _get_headers(self) -> dict[str, str]:
        """Get HTTP headers for requests to NeMo Evaluator."""
        headers = {
            "Content-Type": "application/json",
            "User-Agent": "eval-hub-executor/1.0",
        }

        if self.container_config.auth_token:
            headers["Authorization"] = f"Bearer {self.container_config.auth_token}"

        return headers

    async def _run_post_eval_hooks(self) -> bool:
        """Trigger post-evaluation hooks on the NeMo Evaluator container."""
        try:
            async with httpx.AsyncClient(
                timeout=30.0, verify=self.container_config.verify_ssl
            ) as client:
                response = await client.post(
                    f"{self.base_url}/adapterserver/run-post-hook",
                    json={},
                    headers=self._get_headers(),
                )

                if response.status_code == 200:
                    self.logger.info(
                        "Successfully triggered post-evaluation hooks",
                        endpoint=self.base_url,
                    )
                    return True
                else:
                    self.logger.warning(
                        "Failed to trigger post-evaluation hooks",
                        endpoint=self.base_url,
                        status_code=response.status_code,
                        response=response.text,
                    )
                    return False

        except Exception as e:
            self.logger.error(
                "Error triggering post-evaluation hooks",
                endpoint=self.base_url,
                error=str(e),
            )
            return False

    def get_recommended_timeout_minutes(self) -> int:
        """Get the recommended timeout for NeMo Evaluator."""
        # NeMo Evaluator can handle long-running evaluations
        return self.container_config.timeout_seconds // 60

    def get_max_retry_attempts(self) -> int:
        """Get the maximum retry attempts for NeMo Evaluator."""
        return self.container_config.max_retries


class NemoEvaluatorExecutorFactory:
    """Factory for creating NeMo Evaluator executors."""

    @staticmethod
    def create_executor(backend_config: dict[str, Any]) -> NemoEvaluatorExecutor:
        """Create a NeMo Evaluator executor from backend configuration."""
        return NemoEvaluatorExecutor(backend_config)

    @staticmethod
    def validate_config(backend_config: dict[str, Any]) -> bool:
        """Validate NeMo Evaluator backend configuration."""
        return NemoEvaluatorExecutor.validate_backend_config(backend_config)
