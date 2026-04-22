import json
import os
import logging
from datetime import UTC, datetime


from evalhub.adapter import (
    EvaluationResult,
    FrameworkAdapter,
    JobCallbacks,
    JobPhase,
    JobResults,
    JobSpec,
    JobStatus,
    JobStatusUpdate,
    MessageInfo,
)
from evalhub.adapter.callbacks import DefaultCallbacks

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)

logger = logging.getLogger(__name__)


class LocalTestAdapter(FrameworkAdapter):
    def run_benchmark_job(self, config: JobSpec, callbacks: JobCallbacks) -> JobResults:
        logger.info(
            "Running benchmark job %s for benchmark %s", config.id, config.benchmark_id
        )
        return JobResults(
            id=config.id,
            benchmark_id=config.benchmark_id,
            benchmark_index=config.benchmark_index,
            model_name=config.model.name,
            results=[
                EvaluationResult(
                    metric_name="accuracy",
                    metric_value=0.7,
                ),
            ],
            overall_score=0.7,
            num_examples_evaluated=30,
            duration_seconds=1.0,
            completed_at=datetime.now(UTC),
            evaluation_metadata={
                "framework": "local_test_adapter",
                "framework_version": "1.0.0",
                "num_few_shot": config.parameters.get("num_few_shot"),
                "random_seed": config.parameters.get("random_seed"),
                "parameters": config.parameters,
            },
        )


def main() -> None:
    logger.info("Starting local adapter test")

    try:
        job_spec_path = os.environ["EVALHUB_JOB_SPEC_PATH"]
        adapter = LocalTestAdapter(job_spec_path=job_spec_path)
        logger.debug(
            "Loaded job spec:\n%s", json.dumps(adapter.job_spec.model_dump(), indent=2)
        )

        callbacks = DefaultCallbacks.from_adapter(adapter)

        # status update: running
        callbacks.report_status(
            JobStatusUpdate(
                status=JobStatus.RUNNING,
                phase=JobPhase.INITIALIZING,
                progress=0.0,
                message=MessageInfo(
                    message="Initialising Test Adapter for benchmark",
                    message_code="initializing",
                ),
            )
        )
        results = adapter.run_benchmark_job(adapter.job_spec, callbacks)

        # status update: completed
        callbacks.report_results(results)
        logger.info("EVALUATION COMPLETE")

    except Exception as e:
        logger.error("Evaluation failed: %s", e, exc_info=True)
        raise


if __name__ == "__main__":
    main()
