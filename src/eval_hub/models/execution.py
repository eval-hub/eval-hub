"""Execution mode models for eval-hub."""

from enum import Enum


class ExecutionMode(str, Enum):
    """Evaluation execution modes.

    Defines the available execution modes for running evaluations:
    - KFP: Kubeflow Pipelines (primary mode for new frameworks)
    - K8S_CR: Kubernetes Custom Resources (for platform-optimized workloads)
    - NATIVE: Native Python execution (fallback mode)
    """

    KFP = "kubeflow-pipeline"
    K8S_CR = "kubernetes-cr"
    NATIVE = "native"

    def __str__(self) -> str:
        """Return the string value of the execution mode."""
        return self.value
