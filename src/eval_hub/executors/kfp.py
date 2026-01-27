"""Kubeflow Pipelines executor for running evaluations."""

import asyncio
import json
import os
import tempfile
from collections.abc import Callable
from pathlib import Path
from typing import Any

from ..adapters.registry import AdapterRegistry
from ..core.exceptions import BackendError
from ..core.logging import get_logger
from ..models.evaluation import (
    EvaluationResult,
    EvaluationStatus,
)
from ..utils import utcnow
from .base import ExecutionContext
from .tracked import TrackedExecutor


class KFPExecutor(TrackedExecutor):
    """Executor for running evaluations using Kubeflow Pipelines.

    This executor uses schema adapters to transform eval-hub requests into
    KFP pipeline components, executes them on a Kubeflow Pipelines cluster,
    and transforms the results back to eval-hub format.

    Attributes:
        kfp_endpoint: KFP API endpoint URL
        namespace: Kubernetes namespace for pipeline execution
        experiment_name: KFP experiment name for organizing runs
        adapter_registry: Registry for schema adapters
        poll_interval_seconds: How often to check pipeline status (default: 10)
        enable_caching: Whether to enable KFP caching (default: True)
    """

    def __init__(self, backend_config: dict[str, Any]):
        """Initialize the KFP executor.

        Args:
            backend_config: Configuration dictionary containing:
                - kfp_endpoint: KFP API endpoint URL (required)
                - namespace: Kubernetes namespace (default: "kubeflow")
                - experiment_name: KFP experiment name (default: "eval-hub")
                - framework: Framework name for adapter lookup (required)
                - poll_interval_seconds: Status polling interval (default: 10)
                - enable_caching: Enable KFP caching (default: True)
                - pipeline_root: Root path for pipeline artifacts (optional)
        """
        self.logger = get_logger(__name__)

        # KFP configuration (set before super().__init__ as _validate_config needs them)
        self.kfp_endpoint = backend_config.get("kfp_endpoint")
        self.namespace = backend_config.get("namespace", "kubeflow")
        self.experiment_name = backend_config.get("experiment_name", "eval-hub")
        self.framework = backend_config.get("framework")
        self.poll_interval_seconds = backend_config.get("poll_interval_seconds", 10)
        self.enable_caching = backend_config.get("enable_caching", True)
        self.pipeline_root = backend_config.get("pipeline_root")

        # Initialize adapter registry
        self.adapter_registry = AdapterRegistry()

        # KFP client (lazy initialization)
        self._kfp_client = None

        # Call parent __init__ after setting required attributes
        super().__init__(backend_config)

    def _validate_config(self) -> None:
        """Validate KFP executor configuration.

        Raises:
            ValueError: If required configuration is missing or invalid
        """
        if not self.kfp_endpoint:
            raise ValueError("kfp_endpoint is required in backend configuration")

        if not self.framework:
            raise ValueError("framework is required in backend configuration")

        # Verify adapter is registered for this framework
        if self.framework:
            try:
                self.adapter_registry.get_adapter(self.framework)
            except ValueError as e:
                raise ValueError(
                    f"No adapter registered for framework '{self.framework}': {e}"
                ) from e
        else:
            # framework is None, will fail validation
            pass

    @classmethod
    def get_backend_type(cls) -> str:
        """Get the backend type identifier.

        Returns:
            str: Backend type identifier
        """
        return "kubeflow-pipelines"

    def _get_kfp_token(self) -> str:
        """Get the token for the KFP client.

        Returns:
            str: Token for the KFP client
        """
        import os

        token = os.environ.get("KFP_TOKEN") or self._get_token()

        if not token:
            raise BackendError(
                "KFP_TOKEN environment variable must be set for KFP client"
            )
        return token

    def _get_token(self) -> str:
        """Get authentication token from kubernetes config."""
        try:
            from kubernetes.client.configuration import (  # type: ignore[import-untyped]
                Configuration,
            )
            from kubernetes.client.exceptions import (  # type: ignore[import-untyped]
                ApiException,
            )
            from kubernetes.config.config_exception import (  # type: ignore[import-untyped]
                ConfigException,
            )
            from kubernetes.config.kube_config import (  # type: ignore[import-untyped]
                load_kube_config,
            )

            config = Configuration()

            load_kube_config(client_configuration=config)
            token: str = config.api_key["authorization"].split(" ")[-1]
            return token
        except (KeyError, ConfigException) as e:
            raise ApiException(
                401, "Unauthorized, try running command like `oc login` first"
            ) from e
        except ImportError as e:
            raise BackendError(
                "kubernetes client is not installed. Install with: pip install kubernetes"
            ) from e

    def _get_kfp_client(self) -> Any:
        """Get or create KFP client instance.

        Returns:
            KFP client instance

        Raises:
            BackendError: If KFP client cannot be created
        """
        if self._kfp_client is None:
            try:
                import kfp

                self._kfp_client = kfp.Client(
                    host=self.kfp_endpoint,
                    namespace=self.namespace,
                    existing_token=self._get_kfp_token(),
                )
                self.logger.info(
                    "Created KFP client",
                    endpoint=self.kfp_endpoint,
                    namespace=self.namespace,
                )
            except ImportError as e:
                raise BackendError(
                    "kfp package is required for KFP executor. "
                    "Install with: pip install kfp"
                ) from e
            except Exception as e:
                raise BackendError(f"Failed to create KFP client: {e}") from e

        return self._kfp_client

    async def health_check(self) -> bool:
        """Check if KFP is available and accessible.

        Returns:
            bool: True if KFP is healthy, False otherwise
        """
        try:
            client = self._get_kfp_client()
            # Try to list experiments as a health check
            await asyncio.to_thread(
                client.list_experiments, page_size=1, namespace=self.namespace
            )
            self.logger.info("KFP health check passed", endpoint=self.kfp_endpoint)
            return True
        except Exception as e:
            self.logger.error(
                "KFP health check failed", endpoint=self.kfp_endpoint, error=str(e)
            )
            return False

    async def execute_benchmark(
        self,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> EvaluationResult:
        """Execute a benchmark evaluation using KFP.

        Args:
            context: Execution context with evaluation parameters
            progress_callback: Optional callback for progress updates

        Returns:
            EvaluationResult: Result of the evaluation

        Raises:
            BackendError: If execution fails
            TimeoutError: If execution times out
        """
        self.logger.info(
            "Starting KFP pipeline execution",
            evaluation_id=str(context.evaluation_id),
            framework=self.framework,
            benchmark=context.benchmark_spec.name,
        )

        # Start MLflow tracking
        run_id = await self._track_start(context, self.framework or "kfp")

        try:
            # Get the schema adapter for this framework
            assert self.framework is not None, "Framework must be configured"
            adapter = self.adapter_registry.get_adapter(self.framework)

            # Validate backend configuration with adapter
            if not adapter.validate_config(self.backend_config):
                raise BackendError(
                    f"Backend configuration validation failed for {self.framework}"
                )

            # Transform execution context to KFP arguments
            kfp_args = adapter.transform_to_kfp_args(context, self.backend_config)

            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    0.1,
                    "Transformed context to KFP arguments",
                )

            # Get KFP component specification
            component_spec = adapter.get_kfp_component_spec()

            # Create and submit pipeline
            run_result = await self._create_and_run_pipeline(
                context, component_spec, kfp_args, progress_callback
            )

            if progress_callback:
                progress_callback(
                    str(context.evaluation_id), 0.9, "Parsing pipeline outputs"
                )

            # Parse KFP outputs to eval-hub result
            evaluation_result = adapter.parse_kfp_output(
                run_result["artifacts"], context
            )

            self.logger.info(
                "KFP pipeline execution completed",
                evaluation_id=str(context.evaluation_id),
                status=evaluation_result.status,
            )

            if progress_callback:
                progress_callback(
                    str(context.evaluation_id), 1.0, "Evaluation completed"
                )

            # Attach MLflow tracking and log completion
            final_result = self._with_tracking(evaluation_result, run_id)
            await self._track_complete(final_result, context)

            return final_result

        except Exception as e:
            self.logger.error(
                "KFP pipeline execution failed",
                evaluation_id=str(context.evaluation_id),
                error=str(e),
            )

            # Use common error tracking
            failed_run_id = await self._track_failure(
                context, self.framework or "kfp", str(e), run_id
            )

            # Return failed result
            completed_at = utcnow()
            duration = (
                (completed_at - context.started_at).total_seconds()
                if context.started_at
                else None
            )

            return EvaluationResult(
                evaluation_id=context.evaluation_id,
                provider_id=self.get_backend_type(),
                benchmark_id=context.benchmark_spec.name,
                benchmark_name=context.benchmark_spec.name,
                status=EvaluationStatus.FAILED,
                started_at=context.started_at,
                completed_at=completed_at,
                duration_seconds=duration,
                metrics={},
                mlflow_run_id=failed_run_id,
                error_message=str(e),
            )

    async def _create_and_run_pipeline(
        self,
        context: ExecutionContext,
        component_spec: dict[str, Any],
        kfp_args: dict[str, Any],
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> dict[str, Any]:
        """Create and execute a KFP pipeline.

        Args:
            context: Execution context
            component_spec: KFP component specification
            kfp_args: Arguments for the KFP component
            progress_callback: Optional progress callback

        Returns:
            dict containing pipeline run results and artifacts

        Raises:
            BackendError: If pipeline creation or execution fails
            TimeoutError: If pipeline execution times out
        """
        try:
            import kfp
            from kfp import dsl, kubernetes

            client = self._get_kfp_client()

            # Get S3 credentials secret name (same secret KFP uses for artifacts)
            s3_credentials_secret = self.backend_config.get("s3_credentials_secret")

            # Create a simple pipeline with the component
            @dsl.pipeline(
                name=f"eval-{context.benchmark_spec.name}",
                description=f"Evaluation pipeline for {context.model_name}",
            )
            def evaluation_pipeline() -> None:
                """KFP pipeline for evaluation execution."""
                # Load component from spec
                component_op = kfp.components.load_component_from_text(
                    json.dumps(component_spec)
                )
                # Create component instance with arguments
                task = component_op(**kfp_args)

                # Inject S3 credentials from Kubernetes secret
                kubernetes.use_secret_as_env(
                    task,
                    secret_name=s3_credentials_secret,
                    secret_key_to_env={
                        "AWS_ACCESS_KEY_ID": "AWS_ACCESS_KEY_ID",
                        "AWS_SECRET_ACCESS_KEY": "AWS_SECRET_ACCESS_KEY",
                        "AWS_DEFAULT_REGION": "AWS_DEFAULT_REGION",
                        "AWS_S3_BUCKET": "AWS_S3_BUCKET",
                        "AWS_S3_ENDPOINT": "AWS_S3_ENDPOINT",
                    },
                )

            if progress_callback:
                progress_callback(
                    str(context.evaluation_id), 0.2, "Created pipeline definition"
                )

            # Compile pipeline
            pipeline_file = f"/tmp/pipeline_{context.evaluation_id}.yaml"  # noqa: S108
            kfp.compiler.Compiler().compile(
                pipeline_func=evaluation_pipeline, package_path=pipeline_file
            )

            if progress_callback:
                progress_callback(str(context.evaluation_id), 0.3, "Compiled pipeline")

            # Create or get experiment
            experiment = await asyncio.to_thread(
                client.create_experiment,
                name=self.experiment_name,
                namespace=self.namespace,
            )

            if progress_callback:
                progress_callback(
                    str(context.evaluation_id), 0.4, "Created/retrieved experiment"
                )

            # Submit pipeline run
            run_name = f"eval-{context.evaluation_id}"
            run = await asyncio.to_thread(
                client.run_pipeline,
                experiment_id=experiment.experiment_id,
                job_name=run_name,
                pipeline_package_path=pipeline_file,
                enable_caching=self.enable_caching,
            )

            self.logger.info(
                "Submitted KFP pipeline run",
                run_id=run.run_id,
                run_name=run_name,
                experiment_id=experiment.experiment_id,
            )

            if progress_callback:
                progress_callback(
                    str(context.evaluation_id),
                    0.5,
                    f"Submitted pipeline run {run.run_id}",
                )

            # Monitor pipeline execution
            result = await self._monitor_pipeline_run(
                client, run.run_id, context, progress_callback
            )

            # Cleanup pipeline file
            Path(pipeline_file).unlink(missing_ok=True)

            return result

        except ImportError as e:
            raise BackendError(
                "kfp package is required. Install with: pip install kfp"
            ) from e
        except Exception as e:
            raise BackendError(f"Failed to create/run pipeline: {e}") from e

    async def _monitor_pipeline_run(
        self,
        client: Any,
        run_id: str,
        context: ExecutionContext,
        progress_callback: Callable[[str, float, str], None] | None = None,
    ) -> dict[str, Any]:
        """Monitor a KFP pipeline run until completion.

        Args:
            client: KFP client instance
            run_id: Pipeline run ID
            context: Execution context
            progress_callback: Optional progress callback

        Returns:
            dict containing run results and artifacts

        Raises:
            BackendError: If monitoring fails
            TimeoutError: If execution exceeds timeout
        """
        import time

        start_time = time.time()
        timeout_seconds = context.timeout_minutes * 60
        last_status = None
        progress = 0.5

        while True:
            # Check timeout
            elapsed = time.time() - start_time
            if elapsed > timeout_seconds:
                raise TimeoutError(
                    f"Pipeline execution exceeded timeout of {context.timeout_minutes} minutes"
                )

            # Get run status
            run_detail = await asyncio.to_thread(client.get_run, run_id=run_id)
            status = run_detail.state

            # Log status changes
            if status != last_status:
                self.logger.info(
                    "Pipeline status changed",
                    run_id=run_id,
                    status=status,
                    elapsed_seconds=int(elapsed),
                )
                last_status = status

            # Update progress (50% to 85% during execution)
            if progress < 0.85:
                progress = min(0.85, 0.5 + (elapsed / timeout_seconds) * 0.35)
                if progress_callback:
                    progress_callback(
                        str(context.evaluation_id),
                        progress,
                        f"Pipeline running: {status}",
                    )

            # Check if completed
            if status.lower() in ["succeeded", "failed", "error", "skipped"]:
                if status.lower() != "succeeded":
                    error_msg = getattr(run_detail.run, "error", "Unknown error")
                    raise BackendError(f"Pipeline execution failed: {error_msg}")

                # artifacts = await self._extract_pipeline_artifacts(client, run_id)
                ## Download artifacts from S3
                artifacts = await self._download_artifacts_from_s3(context)

                return {
                    "status": status,
                    "run_id": run_id,
                    "artifacts": artifacts,
                    "duration_seconds": int(elapsed),
                }

            # Wait before next poll
            await asyncio.sleep(self.poll_interval_seconds)

    async def _extract_pipeline_artifacts(
        self, client: Any, run_id: str
    ) -> dict[str, str]:
        """Extract artifact paths from a completed pipeline run.

        Args:
            client: KFP client instance
            run_id: Pipeline run ID

        Returns:
            dict mapping artifact names to file paths
        """
        try:
            # Get run details with artifacts
            run_detail = await asyncio.to_thread(client.get_run, run_id=run_id)

            artifacts = {}

            # Extract output artifacts from pipeline manifest
            if hasattr(run_detail, "pipeline_runtime") and hasattr(
                run_detail.pipeline_runtime, "workflow_manifest"
            ):
                import yaml

                manifest = yaml.safe_load(run_detail.pipeline_runtime.workflow_manifest)

                # Find artifacts in workflow status
                if "status" in manifest and "outputs" in manifest["status"]:
                    for output in manifest["status"]["outputs"].get("artifacts", []):
                        name = output.get("name", "")
                        path = output.get("s3", {}).get("key") or output.get("path", "")
                        if name and path:
                            artifacts[name] = path

            self.logger.info(
                "Extracted pipeline artifacts",
                run_id=run_id,
                artifact_count=len(artifacts),
            )

            return artifacts

        except Exception as e:
            self.logger.warning(
                "Failed to extract pipeline artifacts", run_id=run_id, error=str(e)
            )
            return {}

    async def _download_artifacts_from_s3(
        self, context: ExecutionContext
    ) -> dict[str, str]:
        """Download artifacts from S3.

        The garak component uploads results to a predictable S3 location:
        s3://{bucket}/{prefix}/{job_id}/{artifact_name}.json

        Args:
            context: Execution context with evaluation_id and config

        Returns:
            dict mapping artifact names to local temp file paths

        Raises:
            BackendError: If S3 download fails
        """
        try:
            import boto3  # type: ignore[import-not-found]
            from botocore.exceptions import (  # type: ignore[import-not-found]
                ClientError,
            )
        except ImportError as e:
            raise BackendError(
                "boto3 is required for S3 artifact download. "
                "Install with: pip install 'eval-hub[kfp]'"
            ) from e

        # Get S3 configuration
        s3_bucket = self.backend_config.get("s3_bucket")
        s3_prefix = self.backend_config.get("s3_prefix", "")
        job_id = str(context.evaluation_id)

        if not s3_bucket:
            raise BackendError(
                "s3_bucket must be configured in backend_config for garak provider"
            )

        # Create S3 client
        endpoint_url = os.environ.get("AWS_S3_ENDPOINT")
        aws_access_key_id = os.environ.get("AWS_ACCESS_KEY_ID")
        aws_secret_access_key = os.environ.get("AWS_SECRET_ACCESS_KEY")
        region_name = os.environ.get("AWS_DEFAULT_REGION", "us-east-1")

        if not aws_access_key_id or not aws_secret_access_key:
            raise BackendError(
                "AWS credentials required. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY"
            )

        try:
            s3_client = boto3.client(
                "s3",
                endpoint_url=endpoint_url,
                aws_access_key_id=aws_access_key_id,
                aws_secret_access_key=aws_secret_access_key,
                region_name=region_name,
            )

            # Build S3 keys
            s3_prefix_clean = s3_prefix.rstrip("/").strip() if s3_prefix else ""

            if s3_prefix_clean:
                metrics_key = f"{s3_prefix_clean}/{job_id}/metrics.json"
                results_key = f"{s3_prefix_clean}/{job_id}/results.json"
            else:
                metrics_key = f"{job_id}/metrics.json"
                results_key = f"{job_id}/results.json"

            self.logger.info(
                "Downloading artifacts from S3",
                bucket=s3_bucket,
                metrics_key=metrics_key,
                results_key=results_key,
            )

            # Download to temp files
            artifacts = {}
            temp_dir = tempfile.mkdtemp(prefix=f"garak_artifacts_{job_id}_")

            # Download metrics
            try:
                metrics_path = os.path.join(temp_dir, "metrics.json")
                response = s3_client.get_object(Bucket=s3_bucket, Key=metrics_key)
                with open(metrics_path, "wb") as f:
                    f.write(response["Body"].read())
                artifacts["output_metrics"] = metrics_path
                self.logger.info("Downloaded metrics artifact", path=metrics_path)
            except ClientError as e:
                error_code = e.response.get("Error", {}).get("Code", "Unknown")
                self.logger.error(
                    "Failed to download metrics",
                    bucket=s3_bucket,
                    key=metrics_key,
                    error=error_code,
                )
                raise BackendError(
                    f"Failed to download metrics from s3://{s3_bucket}/{metrics_key}: {error_code}"
                ) from e

            # Download results
            try:
                results_path = os.path.join(temp_dir, "results.json")
                response = s3_client.get_object(Bucket=s3_bucket, Key=results_key)
                with open(results_path, "wb") as f:
                    f.write(response["Body"].read())
                artifacts["output_results"] = results_path
                self.logger.info("Downloaded results artifact", path=results_path)
            except ClientError as e:
                error_code = e.response.get("Error", {}).get("Code", "Unknown")
                self.logger.error(
                    "Failed to download results",
                    bucket=s3_bucket,
                    key=results_key,
                    error=error_code,
                )
                raise BackendError(
                    f"Failed to download results from s3://{s3_bucket}/{results_key}: {error_code}"
                ) from e

            self.logger.info(
                "Successfully downloaded artifacts from S3",
                artifact_count=len(artifacts),
                temp_dir=temp_dir,
            )

            return artifacts

        except ClientError as e:
            error_code = e.response.get("Error", {}).get("Code", "Unknown")
            raise BackendError(
                f"S3 client error: {error_code}. "
                f"Check bucket={s3_bucket}, endpoint={endpoint_url}"
            ) from e
        except Exception as e:
            raise BackendError(f"Failed to download artifacts from S3: {e}") from e

    def get_recommended_timeout_minutes(self) -> int:
        """Get the recommended timeout for KFP execution.

        Returns:
            int: Recommended timeout in minutes (120 minutes / 2 hours)
        """
        return 120

    def supports_parallel_execution(self) -> bool:
        """Check if KFP executor supports parallel execution.

        Returns:
            bool: True (KFP supports parallel pipeline execution)
        """
        return True
