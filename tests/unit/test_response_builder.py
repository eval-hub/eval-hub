"""Unit tests for ResponseBuilder to improve coverage."""

from datetime import datetime
from uuid import uuid4

import pytest

from eval_hub.core.config import Settings
from eval_hub.models.evaluation import (
    EvaluationJobBenchmarkConfig,
    EvaluationJobRequest,
    EvaluationResult,
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
def sample_request():
    """Create a simple evaluation request."""
    model = Model(url="http://test-server:8000", name="test-model")
    benchmarks = [
        EvaluationJobBenchmarkConfig(
            name="Test Benchmark",
            id="test_benchmark",
            provider_id="lm_evaluation_harness",
            config={"num_fewshot": 1},
        )
    ]
    experiment = ExperimentConfig(name="Test Experiment")
    return EvaluationJobRequest(
        model=model.model_dump(),
        benchmarks=[bench.model_dump() for bench in benchmarks],
        experiment=experiment.model_dump(),
        timeout_minutes=45,
        retry_attempts=2,
    )


def create_evaluation_result(
    status: EvaluationStatus,
    benchmark_id: str = "test_benchmark",
    evaluation_id=None,
    duration_seconds=None,
    error_message: str | None = None,
):
    """Helper to create evaluation results with different statuses."""
    return EvaluationResult(
        evaluation_id=evaluation_id or uuid4(),
        provider_id="test_provider",
        benchmark_id=benchmark_id,
        status=status,
        metrics={"accuracy": 0.85},
        artifacts={"results": "/path/to/results"},
        error_message=error_message,
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
            create_evaluation_result(EvaluationStatus.FAILED),
            create_evaluation_result(EvaluationStatus.RUNNING),
            create_evaluation_result(EvaluationStatus.PENDING),
        ]

        counts = response_builder._count_results_by_status(results)

        assert counts[EvaluationStatus.COMPLETED] == 1
        assert counts[EvaluationStatus.FAILED] == 1
        assert counts[EvaluationStatus.RUNNING] == 1
        assert counts[EvaluationStatus.PENDING] == 1

    def test_determine_system_state_running(self, response_builder):
        """Overall state should be running when any evaluation is running."""
        status_counts = {
            EvaluationStatus.COMPLETED: 1,
            EvaluationStatus.RUNNING: 1,
        }
        state = response_builder._determine_system_state(status_counts, expected_runs=2)
        assert state == "running"

    def test_determine_system_state_failed(self, response_builder):
        """Any failure should surface a failed system state."""
        status_counts = {EvaluationStatus.FAILED: 1, EvaluationStatus.COMPLETED: 1}
        state = response_builder._determine_system_state(status_counts, expected_runs=2)
        assert state == "failed"


class TestResponseBuilderResponseShape:
    """Test ResponseBuilder output shape and content."""

    async def test_build_response_pending_preserves_request(
        self, response_builder, sample_request
    ):
        """Pending response should echo user-supplied fields."""
        request_id = uuid4()
        experiment_url = "http://test-mlflow:5000/experiments/1"

        response = await response_builder.build_response(
            request_id, sample_request, [], experiment_url
        )

        assert response.system.id == request_id
        assert response.system.status.state == "pending"
        assert response.results is None
        assert response.model.name == sample_request.model.name
        assert len(response.benchmarks) == len(sample_request.benchmarks)
        assert response.timeout_minutes == sample_request.timeout_minutes
        assert response.retry_attempts == sample_request.retry_attempts

    async def test_build_response_success_populates_results(
        self, response_builder, sample_request
    ):
        """Successful response should include results and aggregated metrics."""
        request_id = uuid4()
        results = [
            create_evaluation_result(
                EvaluationStatus.COMPLETED,
                benchmark_id=sample_request.benchmarks[0].id,
                duration_seconds=12.5,
            )
        ]
        experiment_url = "http://test-mlflow:5000/experiments/1"

        response = await response_builder.build_response(
            request_id, sample_request, results, experiment_url
        )

        assert response.system.status.state == "completed"
        assert response.evaluation_results == results
        assert len(response.evaluation_results) == 1
        assert response.evaluation_results[0].status == EvaluationStatus.COMPLETED
        assert response.evaluation_results[0].duration_seconds == 12.5

    async def test_build_response_failure_hides_results(
        self, response_builder, sample_request
    ):
        """Failed response should surface status and omit results block."""
        request_id = uuid4()
        results = [
            create_evaluation_result(
                EvaluationStatus.FAILED,
                benchmark_id=sample_request.benchmarks[0].id,
                error_message="backend error",
            )
        ]

        response = await response_builder.build_response(
            request_id, sample_request, results, experiment_url=None
        )

        assert response.system.status.state == "failed"
        assert response.results is None
        assert response.system.status.runs[0].state == "failed"
        assert "backend error" in (
            response.system.status.message or response.system.status.runs[0].message
        )
