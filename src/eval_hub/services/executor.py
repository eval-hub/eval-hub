"""Evaluation executor service for orchestrating evaluation runs."""

import asyncio
import builtins
import time
from collections.abc import Callable
from typing import Any
from uuid import UUID

from ..core.config import Settings
from ..core.exceptions import BackendError, TimeoutError
from ..core.logging import get_logger, log_evaluation_complete, log_evaluation_start
from ..executors import ExecutionContext, ExecutorFactory
from ..models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationRequest,
    EvaluationResult,
    EvaluationSpec,
    EvaluationStatus,
)
from ..models.status import TaskInfo, TaskStatus
from ..utils import safe_duration_seconds, utcnow
from .mlflow_client import MLFlowClient


class EvaluationExecutor:
    """Service for executing and monitoring evaluations."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self.logger = get_logger(__name__)
        self.active_tasks: dict[str, TaskInfo] = {}
        self.execution_semaphore = asyncio.Semaphore(
            settings.max_concurrent_evaluations
        )

    async def execute_evaluation_request(
        self,
        request: EvaluationRequest,
        progress_callback: Callable[[str, float, str], None] | None = None,
        mlflow_client: MLFlowClient | None = None,
    ) -> list[EvaluationResult]:
        """Execute all evaluations in a request."""
        self.logger.info(
            "Starting evaluation request execution",
            request_id=str(request.request_id),
            evaluation_count=len(request.evaluations),
        )

        all_results = []

        # Execute evaluations concurrently (but respect semaphore limits)
        tasks = []
        for evaluation in request.evaluations:
            task = asyncio.create_task(
                self._execute_single_evaluation(
                    evaluation,
                    progress_callback,
                    mlflow_client,
                    request.experiment.name if request.experiment else None,
                )
            )
            tasks.append(task)

        # Wait for all evaluations to complete
        evaluation_results = await asyncio.gather(*tasks, return_exceptions=True)

        # Process results
        for i, result in enumerate(evaluation_results):
            if isinstance(result, Exception):
                # Create error result for failed evaluation
                error_result = EvaluationResult(
                    evaluation_id=request.evaluations[i].id,
                    provider_id="unknown",
                    benchmark_id="unknown",
                    benchmark_name="unknown",
                    status=EvaluationStatus.FAILED,
                    error_message=str(result),
                    started_at=utcnow(),
                    completed_at=utcnow(),
                    duration_seconds=0.0,
                    mlflow_run_id=None,
                )
                all_results.append(error_result)
            elif isinstance(result, list):
                all_results.extend(result)

        self.logger.info(
            "Completed evaluation request execution",
            request_id=str(request.request_id),
            total_results=len(all_results),
            successful_results=len(
                [r for r in all_results if r.status == EvaluationStatus.COMPLETED]
            ),
        )

        return all_results

    async def _execute_single_evaluation(
        self,
        evaluation: EvaluationSpec,
        progress_callback: Callable[[str, float, str], None] | None = None,
        mlflow_client: MLFlowClient | None = None,
        experiment_name: str | None = None,
    ) -> list[EvaluationResult]:
        """Execute a single evaluation across all its backends."""
        log_evaluation_start(
            self.logger,
            str(evaluation.id),
            evaluation.model_name,
            len(evaluation.backends),
        )

        start_time = time.time()
        results = []

        try:
            # Execute each backend
            backend_tasks = []
            for backend in evaluation.backends:
                task = asyncio.create_task(
                    self._execute_backend(
                        evaluation,
                        backend,
                        progress_callback,
                        mlflow_client,
                        experiment_name,
                    )
                )
                backend_tasks.append(task)

            # Wait for all backends to complete
            backend_results = await asyncio.gather(
                *backend_tasks, return_exceptions=True
            )

            # Process backend results
            for i, backend_result in enumerate(backend_results):
                if isinstance(backend_result, Exception):
                    # Create error result for failed backend
                    error_result = EvaluationResult(
                        evaluation_id=evaluation.id,
                        provider_id=evaluation.backends[i].name,
                        benchmark_id="unknown",
                        benchmark_name="unknown",
                        status=EvaluationStatus.FAILED,
                        error_message=str(backend_result),
                        started_at=utcnow(),
                        completed_at=utcnow(),
                        duration_seconds=0.0,
                        mlflow_run_id=None,
                    )
                    results.append(error_result)
                elif isinstance(backend_result, list):
                    results.extend(backend_result)

            # Determine overall status
            overall_status = EvaluationStatus.COMPLETED
            if any(r.status == EvaluationStatus.FAILED for r in results):
                overall_status = EvaluationStatus.FAILED
            elif any(r.status == EvaluationStatus.RUNNING for r in results):
                overall_status = EvaluationStatus.RUNNING

            duration = time.time() - start_time
            log_evaluation_complete(
                self.logger, str(evaluation.id), overall_status, duration
            )

        except Exception as e:
            self.logger.error(
                "Evaluation execution failed",
                evaluation_id=str(evaluation.id),
                error=str(e),
            )
            # Create error result
            error_result = EvaluationResult(
                evaluation_id=evaluation.id,
                provider_id="unknown",
                benchmark_id="unknown",
                benchmark_name="unknown",
                status=EvaluationStatus.FAILED,
                error_message=str(e),
                started_at=utcnow(),
                completed_at=utcnow(),
                duration_seconds=0.0,
                mlflow_run_id=None,
            )
            results = [error_result]

        return results

    async def _execute_backend(
        self,
        evaluation: EvaluationSpec,
        backend: BackendSpec,
        progress_callback: Callable[[str, float, str], None] | None = None,
        mlflow_client: MLFlowClient | None = None,
        experiment_name: str | None = None,
    ) -> list[EvaluationResult]:
        """Execute all benchmarks for a specific backend."""
        self.logger.info(
            "Starting backend execution",
            evaluation_id=str(evaluation.id),
            backend_name=backend.name,
            benchmark_count=len(backend.benchmarks),
        )

        results = []

        # Special handling for LMEval backends - group all benchmarks into a single execution
        if backend.type == BackendType.LMEVAL:
            self.logger.info(
                "Grouping LMEval benchmarks for single execution",
                evaluation_id=str(evaluation.id),
                backend_name=backend.name,
                benchmark_count=len(backend.benchmarks),
            )

            try:
                # Combine all benchmark tasks into a single benchmark spec
                all_tasks = []
                all_benchmark_names = []
                combined_config = {}

                for benchmark in backend.benchmarks:
                    all_tasks.extend(benchmark.tasks)
                    all_benchmark_names.append(benchmark.name)
                    # Merge configs (later configs override earlier ones)
                    combined_config.update(benchmark.config)

                # Create combined benchmark spec
                combined_benchmark = BenchmarkSpec(
                    name=f"collection-{'-'.join(all_benchmark_names)}",
                    tasks=all_tasks,
                    config=combined_config,
                    # Use the first benchmark's settings for these fields
                    num_fewshot=(
                        backend.benchmarks[0].num_fewshot
                        if backend.benchmarks
                        else None
                    ),
                    batch_size=(
                        backend.benchmarks[0].batch_size if backend.benchmarks else None
                    ),
                    limit=backend.benchmarks[0].limit if backend.benchmarks else None,
                    device=backend.benchmarks[0].device if backend.benchmarks else None,
                )

                self.logger.info(
                    "Created combined LMEval benchmark",
                    evaluation_id=str(evaluation.id),
                    combined_name=combined_benchmark.name,
                    task_count=len(all_tasks),
                    tasks=all_tasks,
                )

                # Execute the combined benchmark
                result = await self._execute_benchmark(
                    evaluation,
                    backend,
                    combined_benchmark,
                    progress_callback,
                    mlflow_client,
                    experiment_name,
                )
                results.extend(
                    self._expand_combined_lmeval_results(result, backend.benchmarks)
                )

            except Exception as e:
                self.logger.error(
                    "Combined LMEval execution failed",
                    evaluation_id=str(evaluation.id),
                    backend_name=backend.name,
                    error=str(e),
                )
                # Create error result for the combined execution
                for benchmark in backend.benchmarks:
                    error_result = EvaluationResult(
                        evaluation_id=evaluation.id,
                        provider_id=backend.name,
                        benchmark_id=benchmark.name,
                        benchmark_name=benchmark.name,
                        status=EvaluationStatus.FAILED,
                        error_message=str(e),
                        started_at=utcnow(),
                        completed_at=utcnow(),
                        duration_seconds=0.0,
                        mlflow_run_id=None,
                    )
                    results.append(error_result)
        else:
            # Original behavior for non-LMEval backends - execute each benchmark individually
            for benchmark in backend.benchmarks:
                try:
                    result = await self._execute_benchmark(
                        evaluation,
                        backend,
                        benchmark,
                        progress_callback,
                        mlflow_client,
                        experiment_name,
                    )
                    results.append(result)
                except Exception as e:
                    self.logger.error(
                        "Benchmark execution failed",
                        evaluation_id=str(evaluation.id),
                        backend_name=backend.name,
                        benchmark_name=benchmark.name,
                        error=str(e),
                    )
                    # Create error result
                    error_result = EvaluationResult(
                        evaluation_id=evaluation.id,
                        provider_id=backend.name,
                        benchmark_id=benchmark.name,
                        benchmark_name=benchmark.name,
                        status=EvaluationStatus.FAILED,
                        error_message=str(e),
                        started_at=utcnow(),
                        completed_at=utcnow(),
                        duration_seconds=0.0,
                        mlflow_run_id=None,
                    )
                    results.append(error_result)

        return results

    def _expand_combined_lmeval_results(
        self, result: EvaluationResult, benchmarks: list[BenchmarkSpec]
    ) -> list[EvaluationResult]:
        if len(benchmarks) <= 1:
            return [result]

        metrics = result.metrics or {}
        expanded_results = []

        for benchmark in benchmarks:
            benchmark_metrics: dict[str, Any] = {}
            matched = False
            for task in benchmark.tasks:
                prefix = f"{task}_"
                for metric_name, metric_value in metrics.items():
                    if metric_name.startswith(prefix):
                        matched = True
                        stripped_name = metric_name[len(prefix) :]
                        if stripped_name in benchmark_metrics:
                            benchmark_metrics[metric_name] = metric_value
                        else:
                            benchmark_metrics[stripped_name] = metric_value

            if not matched:
                benchmark_metrics = metrics.copy()

            expanded_results.append(
                result.model_copy(
                    update={
                        "benchmark_id": benchmark.name,
                        "benchmark_name": benchmark.name,
                        "metrics": benchmark_metrics,
                    }
                )
            )

        return expanded_results

    async def _execute_benchmark(
        self,
        evaluation: EvaluationSpec,
        backend: BackendSpec,
        benchmark: BenchmarkSpec,
        progress_callback: Callable[[str, float, str], None] | None = None,
        mlflow_client: MLFlowClient | None = None,
        experiment_name: str | None = None,
    ) -> EvaluationResult:
        """Execute a single benchmark on a specific backend."""
        async with self.execution_semaphore:
            # Get model URL directly from evaluation
            if not evaluation.model_url:
                raise BackendError(
                    f"Model URL is required but not provided for evaluation {evaluation.id}"
                )

            self.logger.info(
                "Using model URL for evaluation",
                model_url=evaluation.model_url,
                model_name=evaluation.model_name,
            )

            context = ExecutionContext(
                evaluation_id=evaluation.id,
                model_url=evaluation.model_url,
                model_name=evaluation.model_name,
                backend_spec=backend,
                benchmark_spec=benchmark,
                timeout_minutes=evaluation.timeout_minutes,
                retry_attempts=evaluation.retry_attempts,
                started_at=utcnow(),
                metadata=evaluation.metadata,
                mlflow_client=mlflow_client,
                experiment_name=experiment_name,
            )

            self.logger.info(
                "Starting benchmark execution",
                evaluation_id=str(context.evaluation_id),
                backend_name=backend.name,
                benchmark_name=benchmark.name,
                model_name=context.model_name,
            )

            # Execute with retries
            last_error = None
            for attempt in range(context.retry_attempts + 1):
                try:
                    if attempt > 0:
                        self.logger.info(
                            "Retrying benchmark execution",
                            evaluation_id=str(context.evaluation_id),
                            backend_name=backend.name,
                            benchmark_name=benchmark.name,
                            attempt=attempt + 1,
                            max_attempts=context.retry_attempts + 1,
                        )

                    result = await self._execute_benchmark_with_timeout(
                        context, progress_callback
                    )
                    return result

                except Exception as e:
                    last_error = e
                    self.logger.warning(
                        "Benchmark execution attempt failed",
                        evaluation_id=str(context.evaluation_id),
                        backend_name=backend.name,
                        benchmark_name=benchmark.name,
                        attempt=attempt + 1,
                        error=str(e),
                    )

                    # Wait before retry (exponential backoff)
                    if attempt < context.retry_attempts:
                        wait_time = 2**attempt
                        await asyncio.sleep(wait_time)

            # All retries failed
            return EvaluationResult(
                evaluation_id=context.evaluation_id,
                provider_id=backend.name,
                benchmark_id=benchmark.name,
                benchmark_name=benchmark.name,
                status=EvaluationStatus.FAILED,
                error_message=f"Failed after {context.retry_attempts + 1} attempts: {last_error}",
                started_at=context.started_at,
                completed_at=utcnow(),
                duration_seconds=safe_duration_seconds(utcnow(), context.started_at),
                mlflow_run_id=None,
            )

    async def _execute_benchmark_with_timeout(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Execute a benchmark with timeout handling."""
        timeout_seconds = context.timeout_minutes * 60

        try:
            result = await asyncio.wait_for(
                self._execute_benchmark_impl(context, progress_callback),
                timeout=timeout_seconds,
            )
            return result
        except builtins.TimeoutError:
            raise TimeoutError(  # noqa: B904
                f"Benchmark execution timed out after {context.timeout_minutes} minutes"
            )

    async def _execute_benchmark_impl(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Implementation of benchmark execution using the executor pattern."""

        backend_type = context.backend_spec.type.value
        backend_name = context.backend_spec.name
        benchmark_name = context.benchmark_spec.name

        self.logger.debug(
            "Executing benchmark implementation",
            evaluation_id=str(context.evaluation_id),
            backend_type=backend_type,
            backend_name=backend_name,
            benchmark_name=benchmark_name,
        )

        # Check if we have a registered executor for this backend type
        if ExecutorFactory.is_backend_supported(backend_type):
            # Use the executor pattern for supported backends
            try:
                executor = ExecutorFactory.create_executor(
                    backend_type, context.backend_spec.config
                )

                # Optional health check
                if not await executor.health_check():
                    self.logger.warning(
                        "Backend health check failed, proceeding anyway",
                        evaluation_id=str(context.evaluation_id),
                        backend_type=backend_type,
                    )

                # Execute using the executor
                result = await executor.execute_benchmark(context, progress_callback)

                # Cleanup
                try:
                    await executor.cleanup()
                except Exception as e:
                    self.logger.warning(
                        "Executor cleanup failed",
                        evaluation_id=str(context.evaluation_id),
                        backend_type=backend_type,
                        error=str(e),
                    )

                return result

            except Exception as e:
                self.logger.error(
                    "Executor-based execution failed",
                    evaluation_id=str(context.evaluation_id),
                    backend_type=backend_type,
                    error=str(e),
                )

                return EvaluationResult(
                    evaluation_id=context.evaluation_id,
                    provider_id=backend_name,
                    benchmark_id=benchmark_name,
                    benchmark_name=benchmark_name,
                    status=EvaluationStatus.FAILED,
                    error_message=str(e),
                    started_at=context.started_at,
                    completed_at=utcnow(),
                    duration_seconds=safe_duration_seconds(
                        utcnow(), context.started_at
                    ),
                    mlflow_run_id=None,
                )

        # Fall back to legacy implementations for unsupported backends
        self.logger.info(
            "Using legacy implementation for backend",
            evaluation_id=str(context.evaluation_id),
            backend_type=backend_type,
        )

        # Report progress
        if progress_callback:
            progress_callback(
                str(context.evaluation_id),
                0.0,
                f"Starting {benchmark_name} on {backend_name}",
            )

        # Legacy implementations
        if backend_type == "lm-evaluation-harness":
            result = await self._execute_lm_eval_harness(context, progress_callback)
        elif backend_type == "guidellm":
            result = await self._execute_guidellm(context, progress_callback)
        else:
            result = await self._execute_custom_backend(context, progress_callback)

        # Report completion
        if progress_callback:
            progress_callback(
                str(context.evaluation_id),
                100.0,
                f"Completed {benchmark_name} on {backend_name}",
            )

        return result

    async def _execute_lm_eval_harness(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Execute evaluation using lm-evaluation-harness."""
        # Simulate execution time
        total_time = 30  # 30 seconds simulation
        for i in range(10):
            await asyncio.sleep(total_time / 10)
            if progress_callback:
                progress = (i + 1) * 10
                progress_callback(
                    str(context.evaluation_id),
                    progress,
                    f"Running {context.benchmark_spec.name} - {progress}% complete",
                )

        # Simulate results
        metrics: dict[str, float | int | str] = {
            "accuracy": 0.85 + (hash(str(context.evaluation_id)) % 100) / 1000,
            "perplexity": 2.3 + (hash(str(context.evaluation_id)) % 50) / 100,
            "bleu_score": 0.75 + (hash(str(context.evaluation_id)) % 25) / 1000,
        }

        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id=context.backend_spec.name,
            benchmark_id=context.benchmark_spec.name,
            benchmark_name=context.benchmark_spec.name,
            status=EvaluationStatus.COMPLETED,
            metrics=metrics,
            artifacts={
                "results_json": f"/tmp/results_{context.evaluation_id}_{context.benchmark_spec.name}.json"
            },
            started_at=context.started_at,
            completed_at=utcnow(),
            duration_seconds=safe_duration_seconds(utcnow(), context.started_at),
            error_message=None,
            mlflow_run_id=None,
        )

    async def _execute_guidellm(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Execute evaluation using GuideLL."""
        # Simulate execution time
        total_time = 20  # 20 seconds simulation
        for i in range(10):
            await asyncio.sleep(total_time / 10)
            if progress_callback:
                progress = (i + 1) * 10
                progress_callback(
                    str(context.evaluation_id),
                    progress,
                    f"Running {context.benchmark_spec.name} with GuideLL - {progress}% complete",
                )

        # Simulate results
        metrics: dict[str, float | int | str] = {
            "throughput_tokens_per_second": 150
            + (hash(str(context.evaluation_id)) % 50),
            "latency_p50_ms": 45 + (hash(str(context.evaluation_id)) % 20),
            "latency_p95_ms": 85 + (hash(str(context.evaluation_id)) % 30),
            "error_rate": 0.01 + (hash(str(context.evaluation_id)) % 5) / 1000,
        }

        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id=context.backend_spec.name,
            benchmark_id=context.benchmark_spec.name,
            benchmark_name=context.benchmark_spec.name,
            status=EvaluationStatus.COMPLETED,
            metrics=metrics,
            artifacts={
                "performance_report": f"/tmp/perf_{context.evaluation_id}_{context.benchmark_spec.name}.json"
            },
            started_at=context.started_at,
            completed_at=utcnow(),
            duration_seconds=safe_duration_seconds(utcnow(), context.started_at),
            error_message=None,
            mlflow_run_id=None,
        )

    async def _execute_custom_backend(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Execute evaluation using a custom backend."""
        # For custom backends, we would make HTTP requests to their APIs
        # This is a placeholder implementation

        backend_config = context.backend_spec.config
        endpoint = backend_config.get("endpoint")

        if not endpoint:
            raise BackendError(
                f"No endpoint configured for custom backend {context.backend_spec.name}"
            )

        # Simulate custom backend execution
        await asyncio.sleep(15)

        metrics: dict[str, float | int | str] = {
            "custom_metric_1": 0.9,
            "custom_metric_2": 1.2,
        }

        return EvaluationResult(
            evaluation_id=context.evaluation_id,
            provider_id=context.backend_spec.name,
            benchmark_id=context.benchmark_spec.name,
            benchmark_name=context.benchmark_spec.name,
            status=EvaluationStatus.COMPLETED,
            metrics=metrics,
            artifacts={
                "custom_results": f"/tmp/custom_{context.evaluation_id}_{context.benchmark_spec.name}.json"
            },
            started_at=context.started_at,
            completed_at=utcnow(),
            duration_seconds=safe_duration_seconds(utcnow(), context.started_at),
            error_message=None,
            mlflow_run_id=None,
        )

    async def get_active_evaluations(self) -> list[TaskInfo]:
        """Get list of currently active evaluations."""
        return list(self.active_tasks.values())

    async def check_and_update_evaluation_status(
        self, evaluation_id: str, current_response: Any
    ) -> Any | None:
        """Check if a pending evaluation has completed and update its status.

        This handles multi-benchmark evaluations by checking if all benchmarks have
        completed based on the results counts.

        Args:
            evaluation_id: The evaluation ID to check
            current_response: Current EvaluationJobResource response

        Returns:
            Updated response if evaluation should be marked completed, None if still pending
        """
        try:
            # Skip if not pending
            if current_response.status.state != "pending":
                return None

            # Check if we have results section with evaluation counts
            if not hasattr(current_response, "results") or not current_response.results:
                return None

            results = current_response.results
            total_evals = getattr(results, "total_evaluations", 0)
            completed_evals = getattr(results, "completed_evaluations", 0)
            failed_evals = getattr(results, "failed_evaluations", 0)

            # If all evaluations are done (completed + failed >= total), mark as completed
            if total_evals > 0 and (completed_evals + failed_evals >= total_evals):
                from copy import deepcopy

                updated_response = deepcopy(current_response)

                # Determine overall status
                if failed_evals > 0:
                    updated_response.status.state = "completed"
                    updated_response.status.message = f"Evaluation completed with mixed results: {completed_evals} succeeded, {failed_evals} failed"
                else:
                    updated_response.status.state = "completed"
                    updated_response.status.message = (
                        f"All {completed_evals} evaluations completed successfully"
                    )

                # Update benchmark statuses to match
                if hasattr(updated_response.status, "benchmarks"):
                    for benchmark_status in updated_response.status.benchmarks:
                        if benchmark_status.state == "pending":
                            # Mark remaining as completed (they should have results by now)
                            benchmark_status.state = "completed"
                            benchmark_status.message = "Evaluation completed"

                self.logger.info(
                    "Updated evaluation status from pending to completed",
                    evaluation_id=evaluation_id,
                    total=total_evals,
                    completed=completed_evals,
                    failed=failed_evals,
                )

                return updated_response

            # Still have pending evaluations
            return None

        except Exception as e:
            self.logger.error(
                "Error checking evaluation status",
                evaluation_id=evaluation_id,
                error=str(e),
            )
            return None

    async def cancel_evaluation(self, evaluation_id: UUID) -> bool:
        """Cancel a running evaluation."""
        task_id = str(evaluation_id)
        if task_id in self.active_tasks:
            task_info = self.active_tasks[task_id]
            task_info.status = TaskStatus.CANCELLED
            self.logger.info("Evaluation cancelled", evaluation_id=str(evaluation_id))
            return True
        return False

    async def get_evaluation_status(self, evaluation_id: UUID) -> TaskInfo | None:
        """Get status of a specific evaluation."""
        task_id = str(evaluation_id)
        return self.active_tasks.get(task_id)
