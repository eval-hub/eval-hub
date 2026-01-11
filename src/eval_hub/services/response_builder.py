"""Response builder service for aggregating evaluation results."""

import statistics
from datetime import datetime
from typing import Any, Literal, cast
from uuid import UUID

from ..core.config import Settings
from ..core.logging import get_logger
from ..models.evaluation import (
    BenchmarkConfig,
    BenchmarkResultPayload,
    EvaluationJobBenchmarkConfig,
    EvaluationJobBenchmarkResult,
    EvaluationJobBenchmarkSpec,
    EvaluationJobRequest,
    EvaluationJobResource,
    EvaluationJobResults,
    EvaluationRequest,
    EvaluationResponse,
    EvaluationResult,
    EvaluationStatus,
    ExperimentConfig,
    Model,
    Resource,
    ResultsPayload,
    RunStatus,
    Status,
    SystemInfo,
    SystemStatus,
    get_utc_now,
)


class ResponseBuilder:
    """Service for building and aggregating evaluation responses."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self.logger = get_logger(__name__)

    async def build_response(
        self,
        request_id: UUID,
        job_request: EvaluationJobRequest,
        results: list[EvaluationResult],
        experiment_url: str | None = None,
    ) -> EvaluationResponse:
        """Build a comprehensive response from evaluation results."""
        self.logger.info(
            "Building evaluation response",
            request_id=str(request_id),
            result_count=len(results),
        )

        job_benchmarks = job_request.benchmarks

        status_counts = self._count_results_by_status(results)
        expected_runs = len(job_benchmarks)
        system_state = self._determine_system_state(status_counts, expected_runs)

        runs = self._build_runs(job_benchmarks, results)
        status_message = next((run.message for run in runs if run.message), "")
        status_logs = next((run.logs_path for run in runs if run.logs_path), None)
        completed_at = self._determine_completed_at(results, system_state)

        results_payload: ResultsPayload | None = None
        if system_state == "success" and results:
            aggregated_metrics = await self._aggregate_metrics(results)
            benchmark_payloads = [
                BenchmarkResultPayload(
                    name=result.benchmark_name or result.benchmark_id,
                    metrics=result.metrics,
                    artifacts=result.artifacts,
                    mlflow_run_id=result.mlflow_run_id,
                )
                for result in results
                if result.status == EvaluationStatus.COMPLETED
            ]
            results_payload = ResultsPayload(
                benchmarks=benchmark_payloads,
                aggregated_metrics=aggregated_metrics,
                mlflow_experiment_url=experiment_url,
            )

        system_info = SystemInfo(
            id=request_id,
            status=SystemStatus(
                state=system_state,
                message=status_message,
                logs_path=status_logs,
                runs=runs,
            ),
            created_at=get_utc_now(),
            completed_at=completed_at,
        )

        # Convert EvaluationJobBenchmarkConfig to BenchmarkConfig for response
        converted_benchmarks = []
        for benchmark in job_benchmarks:
            converted_benchmarks.append(
                BenchmarkConfig(
                    benchmark_id=benchmark.id or "",
                    provider_id=benchmark.provider_id or "",
                    config=benchmark.config,
                )
            )

        response = EvaluationResponse(
            system=system_info,
            results=results_payload,
            model=job_request.model,
            benchmarks=converted_benchmarks,
            experiment=job_request.experiment,
            timeout_minutes=job_request.timeout_minutes,
            retry_attempts=job_request.retry_attempts,
            callback_url=job_request.callback_url,
            async_mode=True,  # Always async in new API
            custom={},  # Default empty dict
            evaluation_results=results,
        )

        self.logger.info(
            "Built evaluation response",
            request_id=str(request_id),
            overall_status=system_state,
            completed_runs=len([r for r in runs if r.state == "success"]),
        )

        return response

    async def build_job_resource_response(
        self,
        request_id: UUID,
        job_request: EvaluationJobRequest,
        results: list[EvaluationResult],
        experiment_url: str | None = None,
    ) -> EvaluationJobResource:
        """Build a job resource response matching the proposal structure."""
        self.logger.info(
            "Building evaluation job resource response",
            request_id=str(request_id),
            result_count=len(results),
        )

        # Build resource metadata
        resource = Resource(
            id=str(request_id),
            created_at=get_utc_now(),
            updated_at=get_utc_now(),
        )

        # Build benchmarks status for the Status object
        benchmarks_status = []
        for benchmark in job_request.benchmarks:
            benchmark_id = benchmark.id
            benchmark_name = benchmark.name

            # Find corresponding result
            result = next((r for r in results if r.benchmark_id == benchmark_id), None)

            if result:
                state = self._map_status_to_state(result.status)
                started_at = (
                    result.started_at.isoformat() if result.started_at else None
                )
                completed_at = (
                    result.completed_at.isoformat() if result.completed_at else None
                )
                message = result.error_message or ""
            else:
                state = "pending"
                started_at = None
                completed_at = None
                message = ""

            benchmark_status: dict[str, Any] = {
                "name": benchmark_name,
                "state": state,
                "message": message,
            }

            if started_at:
                benchmark_status["started_at"] = started_at
            if completed_at:
                benchmark_status["completed_at"] = completed_at

            # Add logs if available
            if result and result.artifacts.get("logs_path"):
                benchmark_status["logs"] = {"path": result.artifacts["logs_path"]}

            benchmarks_status.append(benchmark_status)

        # Determine overall status
        status_counts = self._count_results_by_status(results)
        expected_runs = len(job_request.benchmarks)
        overall_state = self._determine_system_state(status_counts, expected_runs)

        status = Status(
            state=overall_state,
            message="",
            benchmarks=benchmarks_status,
        )

        # Build results section
        results_section = None
        if results:
            total_evaluations = len(job_request.benchmarks)
            completed_evaluations = len(
                [r for r in results if r.status == EvaluationStatus.COMPLETED]
            )
            failed_evaluations = len(
                [r for r in results if r.status == EvaluationStatus.FAILED]
            )

            # Build benchmark results
            benchmark_results = []
            for result in results:
                if result.status == EvaluationStatus.COMPLETED:
                    benchmark_result = EvaluationJobBenchmarkResult(
                        name=result.benchmark_name or result.benchmark_id,
                        metrics=result.metrics,
                        artifacts=result.artifacts,
                        mlflow_run_id=result.mlflow_run_id,
                    )
                    benchmark_results.append(benchmark_result)

            # Get aggregated metrics
            aggregated_metrics = await self._aggregate_metrics(results)

            results_section = EvaluationJobResults(
                total_evaluations=total_evaluations,
                completed_evaluations=completed_evaluations,
                failed_evaluations=failed_evaluations,
                benchmarks=benchmark_results,
                aggregated_metrics=aggregated_metrics,
                mlflow_experiment_url=experiment_url,
            )

        # Build model section with proper structure
        model = Model(
            url=job_request.model.url,
            name=job_request.model.name,
        )

        # Build benchmarks array with proper structure
        benchmarks = []
        for benchmark in job_request.benchmarks:
            benchmark_spec = EvaluationJobBenchmarkSpec(
                name=benchmark.name
                or benchmark.id,  # Use id as fallback if name is None
                id=benchmark.id,
                provider_id=benchmark.provider_id,
                config=benchmark.config,
            )
            benchmarks.append(benchmark_spec)

        # Build experiment section - only if experiment exists
        experiment = None
        if job_request.experiment:
            experiment = ExperimentConfig(
                name=job_request.experiment.name,
                tags=job_request.experiment.tags,
            )

        # Create the response
        response = EvaluationJobResource(
            resource=resource,
            status=status,
            results=results_section,
            model=model,
            benchmarks=benchmarks,
            experiment=experiment,
            timeout_minutes=job_request.timeout_minutes,
            retry_attempts=job_request.retry_attempts,
            callback_url=job_request.callback_url,
        )

        self.logger.info(
            "Built evaluation job resource response",
            request_id=str(request_id),
            overall_status=overall_state,
            completed_results=len(
                [r for r in results if r.status == EvaluationStatus.COMPLETED]
            ),
        )

        return response

    def _count_results_by_status(
        self, results: list[EvaluationResult]
    ) -> dict[EvaluationStatus, int]:
        """Count results by their status."""
        counts: dict[EvaluationStatus, int] = {}
        for result in results:
            status = result.status
            counts[status] = counts.get(status, 0) + 1
        return counts

    def _determine_system_state(
        self, status_counts: dict[EvaluationStatus, int], expected_runs: int
    ) -> Literal["pending", "running", "completed", "failed", "cancelled"]:
        """Determine the overall system state for the response."""
        if not status_counts:
            return "pending"

        if status_counts.get(EvaluationStatus.RUNNING, 0) > 0:
            return "running"

        if status_counts.get(EvaluationStatus.PENDING, 0) > 0:
            return "pending"

        if status_counts.get(EvaluationStatus.FAILED, 0) > 0:
            return "failed"

        if status_counts.get(EvaluationStatus.CANCELLED, 0) >= expected_runs > 0:
            return "cancelled"

        completed = status_counts.get(EvaluationStatus.COMPLETED, 0)
        if expected_runs and completed >= expected_runs:
            return "completed"

        return "pending"

    def _map_status_to_state(self, status: EvaluationStatus) -> str:
        """Map internal evaluation status to API-facing state string."""
        if status == EvaluationStatus.COMPLETED:
            return "completed"
        if status == EvaluationStatus.FAILED:
            return "failed"
        if status == EvaluationStatus.CANCELLED:
            return "cancelled"
        return cast(str, status.value)

    def _build_runs(
        self,
        benchmarks: list[EvaluationJobBenchmarkConfig],
        results: list[EvaluationResult],
    ) -> list[RunStatus]:
        """Build run status objects using user-provided benchmarks and results."""
        result_lookup: dict[str, EvaluationResult] = {
            result.benchmark_id: result for result in results
        }

        runs: list[RunStatus] = []
        for benchmark in benchmarks:
            benchmark_id = benchmark.id or ""
            result = result_lookup.get(benchmark_id)
            result_state = (
                self._map_status_to_state(result.status) if result else "pending"
            )
            message = result.error_message if result and result.error_message else ""
            logs_path = result.artifacts.get("logs_path") if result else None

            runs.append(
                RunStatus(
                    name=benchmark_id,
                    state=result_state,
                    message=message,
                    logs_path=logs_path,
                )
            )

        return runs

    def _determine_completed_at(
        self, results: list[EvaluationResult], system_state: str
    ) -> datetime | None:
        """Calculate completion timestamp if the request reached a terminal state."""
        if system_state not in {"success", "failed", "cancelled"}:
            return None

        timestamps = [result.completed_at for result in results if result.completed_at]
        if timestamps:
            return max(timestamps)

        return get_utc_now()

    async def _aggregate_metrics(
        self, results: list[EvaluationResult]
    ) -> dict[str, float | int | str]:
        """Aggregate metrics across all evaluation results."""
        if not results:
            return {}

        # Only aggregate metrics from completed evaluations
        completed_results = [
            r for r in results if r.status == EvaluationStatus.COMPLETED
        ]

        if not completed_results:
            return {"status": "no_completed_evaluations"}

        self.logger.debug(
            "Aggregating metrics",
            total_results=len(results),
            completed_results=len(completed_results),
        )

        # Collect all metric names
        all_metric_names: set[str] = set()
        for result in completed_results:
            all_metric_names.update(result.metrics.keys())

        aggregated: dict[str, float | int | str] = {}

        # Aggregate each metric
        for metric_name in all_metric_names:
            values = []
            for result in completed_results:
                if metric_name in result.metrics:
                    value = result.metrics[metric_name]
                    if isinstance(value, int | float):
                        values.append(float(value))

            if values:
                # Calculate statistics
                aggregated[f"{metric_name}_mean"] = statistics.mean(values)
                aggregated[f"{metric_name}_median"] = statistics.median(values)
                aggregated[f"{metric_name}_min"] = min(values)
                aggregated[f"{metric_name}_max"] = max(values)
                aggregated[f"{metric_name}_std"] = (
                    statistics.stdev(values) if len(values) > 1 else 0.0
                )
                aggregated[f"{metric_name}_count"] = len(values)

        # Add summary statistics
        aggregated["total_evaluations"] = len(results)
        aggregated["completed_evaluations"] = len(completed_results)
        aggregated["success_rate"] = (
            len(completed_results) / len(results) if results else 0.0
        )

        # Calculate average duration
        durations = [
            r.duration_seconds
            for r in completed_results
            if r.duration_seconds is not None
        ]
        if durations:
            aggregated["avg_duration_seconds"] = statistics.mean(durations)
            aggregated["total_duration_seconds"] = sum(durations)

        # Provider and benchmark statistics
        provider_counts: dict[str, int] = {}
        benchmark_counts: dict[str, int] = {}
        for result in completed_results:
            provider_counts[result.provider_id] = (
                provider_counts.get(result.provider_id, 0) + 1
            )
            benchmark_name = result.benchmark_name or result.benchmark_id
            benchmark_counts[benchmark_name] = (
                benchmark_counts.get(benchmark_name, 0) + 1
            )

        aggregated["providers_used"] = len(provider_counts)
        aggregated["benchmarks_used"] = len(benchmark_counts)
        most_used_provider: str | None = (
            max(provider_counts, key=lambda x: provider_counts[x])
            if provider_counts
            else None
        )
        aggregated["most_used_provider"] = (
            most_used_provider if most_used_provider else "none"
        )
        most_used_benchmark: str | None = (
            max(benchmark_counts, key=lambda x: benchmark_counts[x])
            if benchmark_counts
            else None
        )
        aggregated["most_used_benchmark"] = (
            most_used_benchmark if most_used_benchmark else "none"
        )

        return aggregated

    async def build_summary_response(
        self, request: EvaluationRequest, results: list[EvaluationResult]
    ) -> dict[str, Any]:
        """Build a summary response with key insights."""
        aggregated_metrics = await self._aggregate_metrics(results)
        status_counts = self._count_results_by_status(results)

        # Generate insights
        insights = []

        # Success rate insight
        success_rate_val = aggregated_metrics.get("success_rate", 0.0)
        success_rate = (
            float(success_rate_val)
            if isinstance(success_rate_val, int | float)
            else 0.0
        )
        if success_rate >= 0.9:
            insights.append(
                "Excellent success rate - all evaluations completed successfully"
            )
        elif success_rate >= 0.7:
            insights.append(
                "Good success rate - most evaluations completed successfully"
            )
        else:
            insights.append(
                "Some evaluations failed - review error messages for details"
            )

        # Performance insights
        if "avg_duration_seconds" in aggregated_metrics:
            avg_duration_val = aggregated_metrics["avg_duration_seconds"]
            avg_duration = (
                float(avg_duration_val)
                if isinstance(avg_duration_val, int | float)
                else 0.0
            )
            if avg_duration < 60:
                insights.append("Fast evaluation execution - under 1 minute average")
            elif avg_duration < 300:
                insights.append("Moderate evaluation execution time")
            else:
                insights.append(
                    "Long evaluation execution time - consider optimization"
                )

        # Provider insights
        providers_used_val = aggregated_metrics.get("providers_used", 0)
        providers_used = (
            int(providers_used_val)
            if isinstance(providers_used_val, int | float)
            else 0
        )
        if providers_used > 1:
            insights.append(
                f"Multi-provider evaluation across {providers_used} providers"
            )

        return {
            "request_id": str(request.request_id),
            "summary": {
                "total_evaluations": len(results),
                "success_rate": f"{success_rate:.1%}",
                "avg_duration": f"{aggregated_metrics.get('avg_duration_seconds', 0):.1f}s",
                "providers_used": aggregated_metrics.get("providers_used", 0),
                "benchmarks_used": aggregated_metrics.get("benchmarks_used", 0),
            },
            "status_breakdown": {
                status.value: count for status, count in status_counts.items()
            },
            "insights": insights,
            "top_metrics": self._extract_top_metrics(aggregated_metrics),
        }

    def _extract_top_metrics(
        self, aggregated_metrics: dict[str, Any]
    ) -> dict[str, Any]:
        """Extract the most important metrics for summary display."""
        top_metrics = {}

        # Define important metric patterns
        important_patterns = [
            "accuracy_mean",
            "f1_score_mean",
            "bleu_score_mean",
            "perplexity_mean",
            "throughput_tokens_per_second_mean",
            "latency_p50_ms_mean",
            "error_rate_mean",
        ]

        for pattern in important_patterns:
            if pattern in aggregated_metrics:
                # Clean up the name for display
                display_name = pattern.replace("_mean", "").replace("_", " ").title()
                top_metrics[display_name] = aggregated_metrics[pattern]

        return top_metrics
