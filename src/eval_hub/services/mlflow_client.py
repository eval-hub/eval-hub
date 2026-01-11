"""MLFlow client service for experiment tracking and results storage."""

from typing import Any

import mlflow
from mlflow import MlflowClient

from ..core.config import Settings
from ..core.logging import get_logger
from ..models.evaluation import EvaluationRequest, EvaluationResult, EvaluationSpec
from ..utils import utcnow


class MLFlowClient:
    """Client for interacting with MLFlow tracking server."""

    def __init__(self, settings: Settings):
        self.settings = settings
        self.logger = get_logger(__name__)
        self.client: MlflowClient | None = None
        self._client_initialized = False

        # Set up real MLFlow client (lazy)
        self._setup_mlflow()

    def _setup_mlflow(self) -> None:
        """Set up MLFlow configuration (lazy initialization)."""
        try:
            # Only set MLFlow tracking URI, don't test connection yet
            mlflow.set_tracking_uri(self.settings.mlflow_tracking_uri)

            # Client will be created lazily when first needed
            self.client = None
            self._client_initialized = False

            self.logger.info(
                "MLFlow client configured for lazy initialization",
                tracking_uri=self.settings.mlflow_tracking_uri,
            )
        except Exception as e:
            self.logger.error(
                "Failed to configure MLFlow",
                tracking_uri=self.settings.mlflow_tracking_uri,
                error=str(e),
            )
            self.client = None
            self._client_initialized = False

    def _ensure_client_initialized(self) -> None:
        """Lazily initialize the MLFlow client when first needed."""
        if self._client_initialized:
            return

        try:
            # Create MLFlow client instance
            self.client = MlflowClient()

            # Test connection by listing experiments
            experiments = self.client.search_experiments()
            self.logger.info(
                "MLFlow connection established",
                tracking_uri=self.settings.mlflow_tracking_uri,
                experiments_count=len(experiments),
            )
            self._client_initialized = True
        except Exception as e:
            self.logger.error(
                "Failed to connect to MLFlow",
                tracking_uri=self.settings.mlflow_tracking_uri,
                error=str(e),
            )
            # Create a dummy client that will fail gracefully
            self.client = None
            self._client_initialized = True  # Mark as initialized to avoid retries

    async def create_experiment(self, request: EvaluationRequest) -> str:
        """Create or get an MLFlow experiment for the evaluation request."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            raise RuntimeError("MLFlow client not initialized")

        experiment_name = self._generate_experiment_name(request)

        try:
            # Check if experiment already exists
            experiment = self.client.get_experiment_by_name(experiment_name)
            if experiment:
                experiment_id = experiment.experiment_id
                self.logger.info(
                    "Using existing MLFlow experiment",
                    experiment_name=experiment_name,
                    experiment_id=experiment_id,
                )
                return str(experiment_id)
        except Exception:
            # Experiment doesn't exist, create it
            pass

        try:
            # Create new experiment
            experiment_id = self.client.create_experiment(
                name=experiment_name,
                tags={
                    "request_id": str(request.request_id),
                    "created_at": request.created_at.isoformat(),
                    "evaluation_count": str(len(request.evaluations)),
                    "service_version": self.settings.version,
                },
            )

            self.logger.info(
                "Created new MLFlow experiment",
                experiment_name=experiment_name,
                experiment_id=experiment_id,
            )

            return str(experiment_id)
        except Exception as e:
            self.logger.error(
                "Failed to create MLFlow experiment",
                experiment_name=experiment_name,
                error=str(e),
            )
            raise

    async def start_evaluation_run(
        self,
        experiment_id: str,
        evaluation: EvaluationSpec,
        backend_name: str,
        benchmark_name: str,
    ) -> str:
        """Start an MLFlow run for a specific evaluation."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            raise RuntimeError("MLFlow client not initialized")

        run_name = f"{evaluation.model_url}::{evaluation.model_name}_{backend_name}_{benchmark_name}"

        try:
            # Create run with tags and parameters
            tags = {
                "evaluation_id": str(evaluation.id),
                "model_server_id": evaluation.model_url,
                "model_name": evaluation.model_name,
                "backend_name": backend_name,
                "benchmark_name": benchmark_name,
                "priority": str(evaluation.priority),
                "started_at": utcnow().isoformat(),
            }

            if evaluation.risk_category:
                tags["risk_category"] = evaluation.risk_category.value

            run = self.client.create_run(
                experiment_id=experiment_id,
                tags=tags,
                run_name=run_name,
            )
            run_id = run.info.run_id

            # Log parameters
            params = {
                "model_name": evaluation.model_name,
                "model_url": evaluation.model_url,
                "backend": backend_name,
                "benchmark": benchmark_name,
                "priority": str(evaluation.priority),
            }

            if evaluation.risk_category:
                params["risk_category"] = evaluation.risk_category.value

            for key, value in params.items():
                self.client.log_param(run_id, key, str(value))

            self.logger.info(
                "Started MLFlow run",
                run_id=run_id,
                run_name=run_name,
                evaluation_id=str(evaluation.id),
                experiment_id=experiment_id,
            )

            return str(run_id)
        except Exception as e:
            self.logger.error(
                "Failed to start MLFlow run",
                evaluation_id=str(evaluation.id),
                error=str(e),
            )
            raise

    async def log_evaluation_result(self, result: EvaluationResult) -> None:
        """Log evaluation result to MLFlow."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            self.logger.warning(
                "MLFlow client not initialized, skipping result logging"
            )
            return

        if not result.mlflow_run_id:
            self.logger.warning(
                "No MLFlow run ID found for result",
                evaluation_id=str(result.evaluation_id),
            )
            return

        try:
            # Log metrics if present
            if result.metrics:
                for metric_name, metric_value in result.metrics.items():
                    if isinstance(metric_value, int | float):
                        # Sanitize metric name for MLFlow compatibility
                        # Replace invalid characters with underscores
                        sanitized_name = metric_name.replace(",", "_").replace(" ", "_")

                        self.client.log_metric(
                            run_id=result.mlflow_run_id,
                            key=sanitized_name,
                            value=float(metric_value),
                        )

            # Log additional result metadata as parameters
            result_params = {
                "status": result.status.value,
                "provider_id": result.provider_id,
                "benchmark_id": result.benchmark_id,
                "benchmark_name": result.benchmark_name,
            }

            if result.duration_seconds is not None:
                result_params["duration_seconds"] = str(result.duration_seconds)

            if result.completed_at:
                result_params["completed_at"] = result.completed_at.isoformat()

            if result.error_message:
                result_params["error_message"] = result.error_message

            for key, value in result_params.items():
                self.client.log_param(
                    run_id=result.mlflow_run_id,
                    key=key,
                    value=str(value),
                )

            # Log artifacts if present and accessible
            if result.artifacts:
                for artifact_name, artifact_path in result.artifacts.items():
                    try:
                        if isinstance(artifact_path, str) and artifact_path:
                            self.client.log_artifact(
                                run_id=result.mlflow_run_id,
                                local_path=artifact_path,
                                artifact_path=artifact_name,
                            )
                    except Exception as artifact_error:
                        self.logger.warning(
                            "Failed to log artifact",
                            run_id=result.mlflow_run_id,
                            artifact_name=artifact_name,
                            artifact_path=artifact_path,
                            error=str(artifact_error),
                        )

            # Set run status based on evaluation result
            mlflow_status = (
                "FINISHED" if result.status.value == "completed" else "FAILED"
            )
            self.client.set_terminated(result.mlflow_run_id, mlflow_status)

            self.logger.info(
                "Logged evaluation result to MLFlow",
                run_id=result.mlflow_run_id,
                evaluation_id=str(result.evaluation_id),
                status=result.status.value,
                metrics_count=len(result.metrics or {}),
                artifacts_count=len(result.artifacts or {}),
            )
        except Exception as e:
            self.logger.error(
                "Failed to log evaluation result to MLFlow",
                run_id=result.mlflow_run_id,
                evaluation_id=str(result.evaluation_id),
                error=str(e),
            )
            # Don't raise - the evaluation itself was successful

    async def get_experiment_url(self, experiment_id: str) -> str:
        """Get the URL for viewing an experiment in the MLFlow UI."""
        base_url = self.settings.mlflow_tracking_uri.rstrip("/")
        return f"{base_url}/#/experiments/{experiment_id}"

    async def get_run_url(self, run_id: str) -> str:
        """Get the URL for viewing a run in the MLFlow UI."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            base_url = self.settings.mlflow_tracking_uri.rstrip("/")
            return f"{base_url}/#/experiments/0/runs/{run_id}"

        try:
            # Get run info to find the experiment ID
            run = self.client.get_run(run_id)
            experiment_id = run.info.experiment_id
            base_url = self.settings.mlflow_tracking_uri.rstrip("/")
            return f"{base_url}/#/experiments/{experiment_id}/runs/{run_id}"
        except Exception:
            # Fallback to generic URL
            base_url = self.settings.mlflow_tracking_uri.rstrip("/")
            return f"{base_url}/#/experiments/0/runs/{run_id}"

    async def search_runs(
        self,
        experiment_id: str,
        filter_string: str | None = None,
        max_results: int = 100,
    ) -> list[dict[str, Any]]:
        """Search for runs in an experiment."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            return []

        try:
            runs = self.client.search_runs(
                experiment_ids=[experiment_id],
                filter_string=filter_string or "",
                max_results=max_results,
            )

            # Convert runs to dictionary format
            run_data = []
            for run in runs:
                run_dict = {
                    "run_id": run.info.run_id,
                    "experiment_id": run.info.experiment_id,
                    "status": run.info.status,
                    "start_time": run.info.start_time,
                    "end_time": run.info.end_time,
                    "lifecycle_stage": run.info.lifecycle_stage,
                    "tags": dict(run.data.tags),
                    "params": dict(run.data.params),
                    "metrics": dict(run.data.metrics),
                }
                run_data.append(run_dict)

            return run_data
        except Exception as e:
            self.logger.error(
                "Failed to search runs in experiment",
                experiment_id=experiment_id,
                error=str(e),
            )
            return []

    async def get_run_metrics(self, run_id: str) -> dict[str, float]:
        """Get metrics for a specific run."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            return {}

        try:
            run = self.client.get_run(run_id)
            # Filter to only numeric metrics
            return {
                k: float(v)
                for k, v in run.data.metrics.items()
                if isinstance(v, int | float)
            }
        except Exception as e:
            self.logger.error(
                "Failed to get run metrics",
                run_id=run_id,
                error=str(e),
            )
            return {}

    async def delete_experiment(self, experiment_id: str) -> None:
        """Delete an experiment."""
        # Initialize client lazily when first needed
        self._ensure_client_initialized()

        if not self.client:
            self.logger.warning(
                "MLFlow client not initialized, cannot delete experiment"
            )
            return

        try:
            self.client.delete_experiment(experiment_id)
            self.logger.info(
                "Deleted MLFlow experiment",
                experiment_id=experiment_id,
            )
        except Exception as e:
            self.logger.error(
                "Failed to delete MLFlow experiment",
                experiment_id=experiment_id,
                error=str(e),
            )
            raise

    def _generate_experiment_name(self, request: EvaluationRequest) -> str:
        """Generate a unique experiment name for the request."""
        if request.experiment and request.experiment.name:
            base_name = request.experiment.name
        else:
            # Generate name based on timestamp and request content
            timestamp = request.created_at.strftime("%Y%m%d_%H%M%S")
            model_identifiers = list(
                {f"{eval.model_url}::{eval.model_name}" for eval in request.evaluations}
            )
            if len(model_identifiers) == 1:
                base_name = f"{model_identifiers[0]}_{timestamp}"
            else:
                base_name = f"multi_model_{timestamp}"

        # Add prefix
        return f"{self.settings.mlflow_experiment_prefix}_{base_name}"

    async def aggregate_experiment_metrics(self, experiment_id: str) -> dict[str, Any]:
        """Aggregate metrics across all runs in an experiment."""
        runs = await self.search_runs(experiment_id)

        if not runs:
            return {}

        # Aggregate metrics
        all_metrics: dict[str, list[float]] = {}

        for run in runs:
            metrics = run.get("metrics", {})
            for metric_name, metric_value in metrics.items():
                if isinstance(metric_value, int | float):
                    if metric_name not in all_metrics:
                        all_metrics[metric_name] = []
                    all_metrics[metric_name].append(float(metric_value))

        # Calculate aggregated statistics
        aggregated = {}
        for metric_name, values in all_metrics.items():
            if values:
                aggregated[f"{metric_name}_mean"] = sum(values) / len(values)
                aggregated[f"{metric_name}_min"] = min(values)
                aggregated[f"{metric_name}_max"] = max(values)
                aggregated[f"{metric_name}_count"] = len(values)

        return aggregated
