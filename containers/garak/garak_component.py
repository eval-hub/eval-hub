#!/usr/bin/env python3
"""Garak KFP Component - LLM Security Vulnerability Scanner.

This component runs Garak security scans on LLM endpoints and produces
structured results using KFP's native artifact system.

Usage:
    python garak_component.py \
        --model_url "https://model-endpoint/v1" \
        --model_name "gpt-4" \
        --benchmark_id "quick" \
        --probes '["dan"]' \
        --output_metrics /outputs/metrics \
        --output_results /outputs/results
"""

import argparse
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

from avid_report import Report

# Import reusable parsing utilities
from result_parser import (
    combine_parsed_results,
    parse_aggregated_from_avid_content,
    parse_generations_from_report_content,
)


def parse_args() -> argparse.Namespace:
    """Parse command-line arguments."""
    parser = argparse.ArgumentParser(
        description="Garak LLM Security Scanner KFP Component"
    )

    # Model configuration
    parser.add_argument(
        "--model_url",
        type=str,
        required=True,
        help="Model endpoint URL (OpenAI-compatible API)",
    )
    parser.add_argument(
        "--model_name",
        type=str,
        required=True,
        help="Model identifier/name",
    )
    parser.add_argument(
        "--api_key",
        type=str,
        default="",
        help="API key for model endpoint (optional)",
    )

    # Benchmark configuration
    parser.add_argument(
        "--benchmark_id",
        type=str,
        required=True,
        help="Benchmark identifier (e.g., 'quick', 'owasp_llm_top10')",
    )
    parser.add_argument(
        "--probes",
        type=str,
        default="[]",
        help="JSON array of Garak probes to run",
    )
    parser.add_argument(
        "--taxonomy_filters",
        type=str,
        default="[]",
        help="JSON array of taxonomy filters",
    )

    # Execution parameters
    parser.add_argument(
        "--eval_threshold",
        type=float,
        default=0.5,
        help="Vulnerability threshold (0-1)",
    )
    parser.add_argument(
        "--timeout_seconds",
        type=int,
        default=3600,
        help="Maximum execution time in seconds",
    )
    parser.add_argument(
        "--parallel_attempts",
        type=int,
        default=8,
        help="Number of parallel probe attempts",
    )
    parser.add_argument(
        "--generations",
        type=int,
        default=1,
        help="Number of generations per probe",
    )

    # Output paths (KFP artifacts)
    parser.add_argument(
        "--output_metrics",
        type=str,
        required=True,
        help="Output path for metrics JSON",
    )
    parser.add_argument(
        "--output_results",
        type=str,
        required=True,
        help="Output path for detailed results JSON",
    )

    # S3 configuration (for easy evalhub retrieval)
    parser.add_argument(
        "--s3_bucket",
        type=str,
        required=True,
        help="S3 bucket for storing results",
    )
    parser.add_argument(
        "--s3_prefix",
        type=str,
        default="",
        help="S3 prefix for storing results",
    )
    parser.add_argument(
        "--job_id",
        type=str,
        required=True,
        help="Unique job identifier for S3 storage",
    )

    return parser.parse_args()


def validate_inputs(args: argparse.Namespace) -> tuple[bool, list[str]]:
    """Validate inputs before running scan.

    Args:
        args: Parsed command-line arguments

    Returns:
        Tuple of (is_valid, validation_errors)
    """
    validation_errors = []

    # Check if Garak is installed
    try:
        result = subprocess.run(
            ["garak", "--version"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode == 0:
            version = result.stdout.strip()
            print(f"Garak installed: {version}")
        else:
            validation_errors.append(f"Garak version check failed: {result.stderr}")
    except Exception as e:
        validation_errors.append(f"Error checking Garak installation: {str(e)}")

    # Validate model URL
    if not args.model_url or not args.model_url.startswith(("http://", "https://")):
        validation_errors.append(f"Invalid model URL: {args.model_url}")

    # Validate probe/taxonomy configuration
    probes = json.loads(args.probes) if args.probes else []
    taxonomy_filters = (
        json.loads(args.taxonomy_filters) if args.taxonomy_filters else []
    )

    if not probes and not taxonomy_filters:
        validation_errors.append("Either probes or taxonomy_filters must be specified")

    if probes and taxonomy_filters:
        validation_errors.append("Cannot specify both probes and taxonomy_filters")

    return len(validation_errors) == 0, validation_errors


def build_garak_command(
    args: argparse.Namespace,
    output_dir: str,
) -> list[str]:
    """Build Garak CLI command from arguments.

    Args:
        args: Parsed command-line arguments
        output_dir: Directory for Garak output files

    Returns:
        List of command-line arguments for Garak
    """
    command = ["garak"]

    # Model configuration
    command.extend(
        [
            "--model_type",
            "openai.OpenAICompatible",
            "--model_name",
            args.model_name,
        ]
    )

    # Generator options
    model_endpoint = args.model_url.rstrip("/")
    generator_options = {
        "openai": {
            "OpenAICompatible": {
                "uri": model_endpoint,
                "model": args.model_name,
                "api_key": args.api_key or "DUMMY",
                "suppressed_params": ["n"],
            }
        }
    }
    command.extend(["--generator_options", json.dumps(generator_options)])

    # Execution parameters
    command.extend(["--parallel_attempts", str(args.parallel_attempts)])
    command.extend(["--generations", str(args.generations)])
    command.extend(["--eval_threshold", str(args.eval_threshold)])

    # Output location
    report_prefix = os.path.join(output_dir, "scan")
    command.extend(["--report_prefix", report_prefix])

    # Add probes or taxonomy filters
    probes = json.loads(args.probes) if args.probes else []
    taxonomy_filters = (
        json.loads(args.taxonomy_filters) if args.taxonomy_filters else []
    )

    if taxonomy_filters:
        command.extend(["--probe_tags", ",".join(taxonomy_filters)])
    elif probes:
        command.extend(["--probes", ",".join(probes)])

    return command


def run_garak_scan(
    command: list[str],
    output_dir: str,
    timeout_seconds: int,
    max_retries: int = 3,
) -> tuple[bool, str]:
    """Execute Garak scan with retries.

    Args:
        command: Garak command to execute
        output_dir: Directory for output files
        timeout_seconds: Maximum execution time
        max_retries: Number of retry attempts

    Returns:
        Tuple of (success, stdout)
    """
    env = os.environ.copy()
    log_file = os.path.join(output_dir, "garak.log")
    env["GARAK_LOG_FILE"] = log_file

    for attempt in range(max_retries):
        process = None
        try:
            print(f"Starting Garak scan (attempt {attempt + 1}/{max_retries})")
            print(f"Command: {' '.join(command)}")

            process = subprocess.Popen(
                command,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                env=env,
                preexec_fn=os.setsid,
            )

            try:
                stdout, _ = process.communicate(timeout=timeout_seconds)
                print(f"Garak output:\n{stdout}")

                if process.returncode == 0:
                    print("Garak scan completed successfully")
                    return True, stdout
                else:
                    raise subprocess.CalledProcessError(
                        process.returncode, command, stdout, None
                    )

            except subprocess.TimeoutExpired:
                # Kill the entire process group
                os.killpg(os.getpgid(process.pid), signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(os.getpgid(process.pid), signal.SIGKILL)
                    process.wait()
                raise

        except Exception as e:
            if attempt < max_retries - 1:
                wait_time = min(5 * (2**attempt), 60)
                print(f"Attempt {attempt + 1} failed: {e}. Retrying in {wait_time}s...")
                time.sleep(wait_time)
            else:
                print(f"All {max_retries} attempts failed: {e}")
                return False, str(e)

        finally:
            if process:
                try:
                    process.terminate()
                except Exception:
                    pass

    return False, "All retries exhausted"


def generate_avid_report(output_dir: str) -> str:
    """Generate AVID taxonomy report from Garak's native report.

    Args:
        output_dir: Directory containing scan.report.jsonl

    Returns:
        Content of the generated AVID report, or empty string if failed
    """
    report_file = Path(output_dir) / "scan.report.jsonl"
    avid_file = Path(output_dir) / "scan.avid.jsonl"

    if not report_file.exists():
        print(f"Warning: Report file not found for AVID conversion: {report_file}")
        return ""

    try:
        report = Report(str(report_file)).load().get_evaluations()
        report.export()  # Creates scan.avid.jsonl
        print(f"Successfully generated AVID report: {avid_file}")

        if avid_file.exists():
            with open(avid_file) as f:
                return f.read()
    except Exception as e:
        print(f"Warning: Failed to generate AVID report: {e}")

    return ""


def parse_garak_results(output_dir: str, eval_threshold: float) -> dict[str, Any]:
    """Parse Garak scan results using the proven result_parser utilities.

    This function:
    1. Reads the Garak report.jsonl file
    2. Generates AVID taxonomy report
    3. Parses generations with full prompt/response data
    4. Combines with AVID aggregated statistics

    Args:
        output_dir: Directory containing Garak output files
        eval_threshold: Vulnerability threshold (0-1 scale, scores >= threshold = vulnerable)

    Returns:
        Parsed scan result dictionary with generations, scores, and summary
    """
    report_file = Path(output_dir) / "scan.report.jsonl"

    if not report_file.exists():
        print(f"Warning: Report file not found: {report_file}")
        return {"generations": [], "scores": {}, "summary": {}}

    # Read report content
    with open(report_file) as f:
        report_content = f.read()

    # Generate AVID report for taxonomy information
    print("Generating AVID taxonomy report...")
    avid_content = generate_avid_report(output_dir)

    # Parse generations and score rows from report.jsonl
    print("Parsing generations from report.jsonl...")
    generations, score_rows_by_probe = parse_generations_from_report_content(
        report_content, eval_threshold
    )
    print(f"Parsed {len(generations)} attempts from {len(score_rows_by_probe)} probes")

    # Parse aggregated info from AVID report (includes taxonomy metadata)
    print("Parsing aggregated info from AVID report...")
    aggregated_by_probe = parse_aggregated_from_avid_content(avid_content)
    print(f"Parsed {len(aggregated_by_probe)} probe summaries with AVID taxonomy")

    # Combine parsed results
    print("Combining results...")
    results = combine_parsed_results(
        generations,
        score_rows_by_probe,
        aggregated_by_probe,
        eval_threshold,
    )

    # Calculate summary statistics
    total_attempts = len(results["generations"])
    vulnerable_count = sum(
        1 for g in results["generations"] if g.get("vulnerable", False)
    )

    results["summary"] = {
        "total_attempts": total_attempts,
        "vulnerable_attempts": vulnerable_count,
        "safe_attempts": total_attempts - vulnerable_count,
        "attack_success_rate": (vulnerable_count / total_attempts * 100)
        if total_attempts > 0
        else 0.0,
        "security_score": 100.0
        - ((vulnerable_count / total_attempts * 100) if total_attempts > 0 else 0.0),
        "eval_threshold": eval_threshold,
        "probes_tested": list(results["scores"].keys()),
    }

    return results


def extract_metrics(results: dict[str, Any]) -> dict[str, Any]:
    """Extract high-level summary metrics for KFP Metrics artifact.

    Args:
        results: Parsed scan results

    Returns:
        Metrics dictionary with summary statistics only
    """
    summary = results.get("summary", {})

    # Only overall summary metrics - adapter will extract probe-specific details
    metrics = {
        "total_attempts": summary.get("total_attempts", 0),
        "vulnerable_attempts": summary.get("vulnerable_attempts", 0),
        "attack_success_rate": round(summary.get("attack_success_rate", 0.0), 2),
        "security_score": round(summary.get("security_score", 100.0), 2),
    }

    return metrics


def write_kfp_artifacts(
    metrics: dict[str, Any],
    results: dict[str, Any],
    metrics_path: str,
    results_path: str,
) -> None:
    """Write KFP output artifacts.

    Args:
        metrics: Metrics dictionary
        results: Detailed results
        metrics_path: Path to write metrics artifact
        results_path: Path to write results artifact
    """
    # Ensure output directories exist
    Path(metrics_path).parent.mkdir(parents=True, exist_ok=True)
    Path(results_path).parent.mkdir(parents=True, exist_ok=True)

    # Write metrics artifact
    print(f"Writing metrics to {metrics_path}")
    with open(metrics_path, "w") as f:
        json.dump(metrics, f, indent=2)

    # Write detailed results artifact
    print(f"Writing results to {results_path}")
    with open(results_path, "w") as f:
        json.dump(results, f, indent=2)

    print(f"Artifacts written successfully. {len(metrics)} metrics recorded.")


def upload_to_s3(
    metrics: dict[str, Any],
    results: dict[str, Any],
    s3_bucket: str,
    s3_prefix: str,
    job_id: str,
) -> None:
    """Upload results to S3 for easy evalhub retrieval.

    This uploads to a predictable S3 location so evalhub can download
    results without complex KFP artifact extraction.

    Args:
        metrics: Metrics dictionary
        results: Detailed results
        s3_bucket: S3 bucket name
        s3_prefix: S3 prefix (can be empty)
        job_id: Unique job identifier
    """
    import boto3
    from botocore.exceptions import ClientError

    print(f"Uploading results to S3: s3://{s3_bucket}/{s3_prefix}/{job_id}/")

    try:
        # Create S3 client
        s3_client = boto3.client(
            "s3",
            endpoint_url=os.environ.get("AWS_S3_ENDPOINT"),
            aws_access_key_id=os.environ.get("AWS_ACCESS_KEY_ID"),
            aws_secret_access_key=os.environ.get("AWS_SECRET_ACCESS_KEY"),
            region_name=os.environ.get("AWS_DEFAULT_REGION", "us-east-1"),
        )

        # Build S3 keys
        s3_prefix_clean = s3_prefix.rstrip("/").strip() if s3_prefix else ""

        if s3_prefix_clean:
            metrics_key = f"{s3_prefix_clean}/{job_id}/metrics.json"
            results_key = f"{s3_prefix_clean}/{job_id}/results.json"
        else:
            metrics_key = f"{job_id}/metrics.json"
            results_key = f"{job_id}/results.json"

        # Upload metrics
        print(f"  Uploading metrics to s3://{s3_bucket}/{metrics_key}")
        s3_client.put_object(
            Bucket=s3_bucket,
            Key=metrics_key,
            Body=json.dumps(metrics, indent=2).encode("utf-8"),
            ContentType="application/json",
        )

        # Upload results
        print(f"  Uploading results to s3://{s3_bucket}/{results_key}")
        s3_client.put_object(
            Bucket=s3_bucket,
            Key=results_key,
            Body=json.dumps(results, indent=2).encode("utf-8"),
            ContentType="application/json",
        )

        print("✅ Successfully uploaded results to S3")

    except ClientError as e:
        error_code = e.response.get("Error", {}).get("Code", "Unknown")
        print(f"⚠️  S3 upload failed [{error_code}]: {e}")
        print(f"   Bucket: {s3_bucket}")
        print(f"   Endpoint: {os.environ.get('AWS_S3_ENDPOINT')}")
    except Exception as e:
        print(f"⚠️  S3 upload failed: {e}")
        print("   Note: KFP artifacts are still available.")


def main() -> int:
    """Main entry point for Garak KFP component.

    Returns:
        Exit code (0 for success, 1 for failure)
    """
    try:
        args = parse_args()

        print("Starting Garak scan")
        print(f"  Model: {args.model_name} at {args.model_url}")
        print(f"  Benchmark: {args.benchmark_id}")

        # Validate inputs
        is_valid, validation_errors = validate_inputs(args)
        if not is_valid:
            print(f"Validation failed: {validation_errors}")
            return 1

        # Create temporary output directory for Garak
        output_dir = tempfile.mkdtemp(prefix="garak_")
        print(f"Using output directory: {output_dir}")

        try:
            # Build and run Garak command
            command = build_garak_command(args, output_dir)
            success, stdout = run_garak_scan(
                command=command,
                output_dir=output_dir,
                timeout_seconds=args.timeout_seconds,
            )

            if not success:
                print(f"Garak scan failed: {stdout}")
                return 1

            # Parse results
            results = parse_garak_results(output_dir, args.eval_threshold)

            # Extract metrics
            metrics = extract_metrics(results)

            # Write KFP artifacts
            write_kfp_artifacts(
                metrics=metrics,
                results=results,
                metrics_path=args.output_metrics,
                results_path=args.output_results,
            )

            # Also upload to S3 for easy evalhub retrieval
            upload_to_s3(
                metrics=metrics,
                results=results,
                s3_bucket=args.s3_bucket,
                s3_prefix=args.s3_prefix,
                job_id=args.job_id,
            )

            print("✅ Garak security scan completed successfully")
            return 0

        finally:
            # Clean up temporary directory
            try:
                shutil.rmtree(output_dir)
                print(f"Cleaned up temporary directory: {output_dir}")
            except Exception as e:
                print(f"Warning: Failed to clean up {output_dir}: {e}")

    except Exception as e:
        print(f"❌ Error running Garak scan: {e}", file=sys.stderr)
        import traceback

        traceback.print_exc()
        return 1


if __name__ == "__main__":
    sys.exit(main())
