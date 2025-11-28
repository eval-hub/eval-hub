"""Unit tests for ResponseBuilder to improve coverage."""

from datetime import datetime
from uuid import uuid4

import pytest

from eval_hub.core.config import Settings
from eval_hub.models.evaluation import (
    BackendSpec,
    BackendType,
    BenchmarkSpec,
    EvaluationRequest,
    EvaluationResult,
    EvaluationSpec,
    EvaluationStatus,
    ExperimentConfig,
    Model,
)
from eval_hub.services.response_builder import ResponseBuilder


@pytest.fixture
def response_builder():
    """Create ResponseBuilder instance."""
    settings = Settings()
    return ResponseBuilder(settings)


@pytest.fixture
def sample_evaluation_request():
    """Create a sample evaluation request."""
    model = Model(url="http://test-server:8000", name="test-model")
    benchmark = BenchmarkSpec(name="test_benchmark", tasks=["test_task"])
    backend = BackendSpec(
        name="test-backend", type=BackendType.LMEVAL, benchmarks=[benchmark]
    )
    eval_spec = EvaluationSpec(name="Test Evaluation", model=model, backends=[backend])

    return EvaluationRequest(
        request_id=uuid4(),
        evaluations=[eval_spec],
        experiment=ExperimentConfig(name="Test Experiment"),
    )


def create_evaluation_result(
    status: EvaluationStatus, evaluation_id=None, duration_seconds=None
):
    """Helper to create evaluation results with different statuses."""
    return EvaluationResult(
        evaluation_id=evaluation_id or uuid4(),
        provider_id="test_provider",
        benchmark_id="test_benchmark",
        status=status,
        metrics={"accuracy": 0.85},
        artifacts={"results": "/path/to/results"},
        started_at=datetime.utcnow(),
        completed_at=datetime.utcnow()
        if status in [EvaluationStatus.COMPLETED, EvaluationStatus.FAILED]
        else None,
        duration_seconds=duration_seconds,
    )


class TestResponseBuilderStatusLogic:
    """Test ResponseBuilder status determination logic."""

    def test_count_results_by_status_empty(self, response_builder):
        """Test counting results when no results provided."""
        counts = response_builder._count_results_by_status([])
        assert counts == {}

    def test_count_results_by_status_mixed(self, response_builder):
        """Test counting results with mixed statuses."""
        results = [
            create_evaluation_result(EvaluationStatus.COMPLETED),
            create_evaluation_result(EvaluationStatus.COMPLETED),
            create_evaluation_result(EvaluationStatus.FAILED),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]

        counts = response_builder._count_results_by_status(results)

        assert counts[EvaluationStatus.COMPLETED] == 2
        assert counts[EvaluationStatus.FAILED] == 1
        assert counts[EvaluationStatus.RUNNING] == 1
        assert counts[EvaluationStatus.PENDING] == 1

    def test_determine_overall_status_empty(self, response_builder):
        """Test overall status determination with empty status counts."""
        status = response_builder._determine_overall_status({}, 0)
        assert status == EvaluationStatus.PENDING

    def test_determine_overall_status_running(self, response_builder):
        """Test overall status when any evaluations are running."""
        status_counts = {
            EvaluationStatus.COMPLETED: 2,
            EvaluationStatus.RUNNING: 1,
            EvaluationStatus.PENDING: 1,
        }
        status = response_builder._determine_overall_status(status_counts, 4)
        assert status == EvaluationStatus.RUNNING

    def test_determine_overall_status_pending(self, response_builder):
        """Test overall status when any evaluations are pending (but none running)."""
        status_counts = {EvaluationStatus.COMPLETED: 2, EvaluationStatus.PENDING: 1}
        status = response_builder._determine_overall_status(status_counts, 3)
        assert status == EvaluationStatus.PENDING

    def test_determine_overall_status_all_completed(self, response_builder):
        """Test overall status when all evaluations are completed."""
        status_counts = {EvaluationStatus.COMPLETED: 3}
        status = response_builder._determine_overall_status(status_counts, 3)
        assert status == EvaluationStatus.COMPLETED

    def test_determine_overall_status_all_failed(self, response_builder):
        """Test overall status when all evaluations failed."""
        status_counts = {EvaluationStatus.FAILED: 3}
        status = response_builder._determine_overall_status(status_counts, 3)
        assert status == EvaluationStatus.FAILED

    def test_determine_overall_status_partial_failure(self, response_builder):
        """Test overall status with partial failures (some completed, some failed)."""
        status_counts = {EvaluationStatus.COMPLETED: 2, EvaluationStatus.FAILED: 1}
        status = response_builder._determine_overall_status(status_counts, 3)
        # Partial completion should be considered COMPLETED
        assert status == EvaluationStatus.COMPLETED

    def test_determine_overall_status_no_failures_or_completions(
        self, response_builder
    ):
        """Test overall status with cancelled evaluations (edge case)."""
        status_counts = {EvaluationStatus.CANCELLED: 2}
        status = response_builder._determine_overall_status(status_counts, 2)
        # Should default to COMPLETED for any other case
        assert status == EvaluationStatus.COMPLETED

    def test_calculate_progress_percentage_empty(self, response_builder):
        """Test progress calculation with no evaluations."""
        progress = response_builder._calculate_progress_percentage({}, 0)
        assert progress == 0.0

    def test_calculate_progress_percentage_all_completed(self, response_builder):
        """Test progress calculation with all completed."""
        status_counts = {EvaluationStatus.COMPLETED: 5}
        progress = response_builder._calculate_progress_percentage(status_counts, 5)
        assert progress == 100.0

    def test_calculate_progress_percentage_mixed(self, response_builder):
        """Test progress calculation with mixed statuses."""
        status_counts = {
            EvaluationStatus.COMPLETED: 2,  # 100% each = 2.0
            EvaluationStatus.FAILED: 1,  # 100% each = 1.0
            EvaluationStatus.RUNNING: 2,  # 50% each = 1.0
            EvaluationStatus.PENDING: 1,  # 0% each = 0.0
        }
        # Total weight: 2 + 1 + 1 = 4.0 out of 6 total = 66.67%
        progress = response_builder._calculate_progress_percentage(status_counts, 6)
        expected = (4.0 / 6.0) * 100.0
        assert abs(progress - expected) < 0.1

    def test_calculate_progress_percentage_capped_at_100(self, response_builder):
        """Test that progress percentage is capped at 100%."""
        # Edge case: more results than expected total (shouldn't happen but test safety)
        status_counts = {EvaluationStatus.COMPLETED: 5}
        progress = response_builder._calculate_progress_percentage(status_counts, 3)
        assert progress == 100.0

    async def test_build_response_calls_status_methods(
        self, response_builder, sample_evaluation_request
    ):
        """Test that build_response method calls the status calculation methods."""
        results = [
            create_evaluation_result(EvaluationStatus.COMPLETED),
            create_evaluation_result(EvaluationStatus.RUNNING),
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        # This will exercise the status calculation methods
        response = await response_builder.build_response(
            sample_evaluation_request, results, experiment_url
        )

        # Verify the response uses the calculated status
        assert response.status == EvaluationStatus.RUNNING  # Because one is running
        assert response.total_evaluations > 0
        assert response.progress_percentage > 0
        assert response.results == results

    async def test_build_response_completion_estimation_with_completed_results(
        self, response_builder, sample_evaluation_request
    ):
        """Test completion time estimation when some results are completed."""
        # Create mix of results with some completed having duration
        results = [
            create_evaluation_result(
                EvaluationStatus.COMPLETED, duration_seconds=120.0
            ),
            create_evaluation_result(
                EvaluationStatus.COMPLETED, duration_seconds=180.0
            ),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        response = await response_builder.build_response(
            sample_evaluation_request, results, experiment_url
        )

        # Should use average of completed durations for estimation
        assert response.estimated_completion is not None

    async def test_build_response_completion_estimation_no_completed_results(
        self, response_builder, sample_evaluation_request
    ):
        """Test completion time estimation when no results are completed."""
        results = [
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        response = await response_builder.build_response(
            sample_evaluation_request, results, experiment_url
        )

        # Should use default estimation (5 minutes = 300 seconds)
        assert response.estimated_completion is not None

    async def test_build_response_completion_estimation_no_duration_data(
        self, response_builder, sample_evaluation_request
    ):
        """Test completion time estimation when completed results have no duration."""
        # Create completed results without duration_seconds
        results = [
            create_evaluation_result(EvaluationStatus.COMPLETED, duration_seconds=None),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        response = await response_builder.build_response(
            sample_evaluation_request, results, experiment_url
        )

        # Should fallback to default when no duration data available
        assert response.estimated_completion is not None

    async def test_build_response_concurrency_estimation(
        self, response_builder, sample_evaluation_request
    ):
        """Test completion time estimation with concurrency consideration."""
        # Create many pending/running results to test concurrency logic
        results = [
            create_evaluation_result(
                EvaluationStatus.COMPLETED, duration_seconds=100.0
            ),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
            create_evaluation_result(EvaluationStatus.PENDING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        # Mock max_concurrent_evaluations to test concurrency logic
        response_builder.settings.max_concurrent_evaluations = 2

        response = await response_builder.build_response(
            sample_evaluation_request, results, experiment_url
        )

        # Should account for concurrency in time estimation
        assert response.estimated_completion is not None
