"""Local runtime fixture for deterministic results, OCI exports, and requested failures."""

import json
import os
import logging
from pathlib import Path
from tempfile import TemporaryDirectory
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
    OCIArtifactSpec,
    configure_telemetry,
)
from evalhub.adapter.callbacks import DefaultCallbacks

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)

logger = logging.getLogger(__name__)


class LocalTestAdapter(FrameworkAdapter):
    """Exercise runtime callbacks without calling a model or benchmark framework."""

    def run_benchmark_job(self, config: JobSpec, callbacks: JobCallbacks) -> JobResults:
        """Return fixed metrics and optional OCI output, or raise when test_fail is set."""
        logger.info(
            "Running benchmark job %s for benchmark %s", config.id, config.benchmark_id
        )
        if config.parameters.get("test_fail"):
            callbacks.report_status(JobStatusUpdate(status=JobStatus.FAILED))
            raise RuntimeError("Requested local test adapter failure")
        results = JobResults(
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
                "framework_version": "1.0.X",  # this is just a test so it can be anything
                "num_few_shot": config.parameters.get("num_few_shot"),
                "random_seed": config.parameters.get("random_seed"),
                "parameters": config.parameters,
            },
        )
        if config.exports and config.exports.oci:
            coordinates = config.exports.oci.coordinates.model_copy(deep=True)
            coordinates.oci_host = coordinates.oci_host.removeprefix(
                "http://"
            ).removeprefix("https://")
            with TemporaryDirectory(prefix="evalhub-local-results-") as directory:
                output = Path(directory)
                (output / "results.json").write_text(results.model_dump_json())
                results.oci_artifact = callbacks.create_oci_artifact(
                    OCIArtifactSpec(files_path=output, coordinates=coordinates)
                )
        return results


def main() -> None:
    """Load the job spec and report traced status and results through SDK callbacks."""
    # Bridge Python logging to OTEL so adapter logs are exported as structured
    # log records (with trace/span correlation). No-op when OTEL env vars are
    # absent, so this is safe for local runs without a collector.
    configure_telemetry()

    logger.info("Starting local adapter test")

    try:
        job_spec_path = os.environ["EVALHUB_JOB_SPEC_PATH"]
        adapter = LocalTestAdapter(job_spec_path=job_spec_path)
        if (
            adapter.job_spec.exports
            and adapter.job_spec.exports.oci
            and adapter.job_spec.exports.oci.coordinates.oci_host.startswith("http://")
        ):
            adapter.settings.oci_insecure = True
        logger.debug(
            "Loaded job spec:\n%s", json.dumps(adapter.job_spec.model_dump(), indent=2)
        )

        # from_adapter builds an EvalTracer via EvalTracer.from_job_spec, which
        # sets the job-level log context (job_id, benchmark_id, ...) attached to
        # every OTEL log record.
        callbacks = DefaultCallbacks.from_adapter(adapter)

        # Wrap the run in the root evaluation span so exported logs carry the
        # trace_id/span_id for log-trace correlation.
        with callbacks.tracer.evaluation_run():
            # status update: running
            callbacks.report_status(
                JobStatusUpdate(
                    status=JobStatus.RUNNING,
                    phase=JobPhase.INITIALIZING,
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
