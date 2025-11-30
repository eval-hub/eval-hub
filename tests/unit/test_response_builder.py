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
        assert response.results == results

    async def test_build_response_excludes_progress_fields(
        self, response_builder, sample_evaluation_request
    ):
        """build_response should not include progress/estimation fields."""
        results = [
            create_evaluation_result(EvaluationStatus.COMPLETED, duration_seconds=None),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        response = await response_builder.build_response(
            sample_evaluation_request, results, experiment_url
        )

        dumped = response.model_dump()
        assert "estimated_completion" not in dumped
        assert "progress_percentage" not in dumped
