"""Response builder service for aggregating evaluation results."""

import statistics
from typing import Any

from ..core.config import Settings
from ..core.logging import get_logger
from ..models.evaluation import (
    EvaluationRequest,
    EvaluationResponse,
    EvaluationResult,
    EvaluationStatus,
)
from ..utils import utcnow


class ResponseBuilder:
    """Service for building and aggregating evaluation responses."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self.logger = get_logger(__name__)

    async def build_response(
        self,
        request: EvaluationRequest,
        results: list[EvaluationResult],
        experiment_url: str | None = None,
    ) -> EvaluationResponse:
        """Build a comprehensive response from evaluation results."""
        self.logger.info(
            "Building evaluation response",
            request_id=str(request.request_id),
            result_count=len(results),
        )

        # Calculate status counts
        status_counts = self._count_results_by_status(results)
        total_evaluations = len(results)
        completed_evaluations = status_counts.get(EvaluationStatus.COMPLETED, 0)
        failed_evaluations = status_counts.get(EvaluationStatus.FAILED, 0)

        # Determine overall status
        overall_status = self._determine_overall_status(
            status_counts, total_evaluations
        )

        # Aggregate metrics
        aggregated_metrics = await self._aggregate_metrics(results)

        response = EvaluationResponse(
            id=request.request_id,
            status=overall_status,
            total_evaluations=total_evaluations,
            completed_evaluations=completed_evaluations,
            failed_evaluations=failed_evaluations,
            results=results,
            aggregated_metrics=aggregated_metrics,
            experiment_url=experiment_url,
            created_at=request.created_at,
            updated_at=utcnow(),
        )

        self.logger.info(
            "Built evaluation response",
            request_id=str(request.request_id),
            overall_status=overall_status,
            completed_count=completed_evaluations,
            failed_count=failed_evaluations,
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

    def _determine_overall_status(
        self, status_counts: dict[EvaluationStatus, int], total_evaluations: int
    ) -> EvaluationStatus:
        """Determine the overall status based on individual result statuses."""
        if not status_counts:
            return EvaluationStatus.PENDING

        # If any are still running, overall status is running
        if status_counts.get(EvaluationStatus.RUNNING, 0) > 0:
            return EvaluationStatus.RUNNING

        # If any are pending, overall status is pending
        if status_counts.get(EvaluationStatus.PENDING, 0) > 0:
            return EvaluationStatus.PENDING

        # If all are completed, overall status is completed
        if status_counts.get(EvaluationStatus.COMPLETED, 0) == total_evaluations:
            return EvaluationStatus.COMPLETED

        # If any failed, check if all failed or partial
        failed_count = status_counts.get(EvaluationStatus.FAILED, 0)
        completed_count = status_counts.get(EvaluationStatus.COMPLETED, 0)

        if failed_count > 0 and completed_count == 0:
            return EvaluationStatus.FAILED
        elif failed_count > 0 and completed_count > 0:
            # Partial completion - consider as completed with some failures
            return EvaluationStatus.COMPLETED
        else:
            return EvaluationStatus.COMPLETED

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
