"""Schema adapters for evaluation frameworks.

This module provides the adapter layer for transforming between eval-hub's
internal representation and framework-specific formats. Adapters enable
integration with various evaluation frameworks via Kubeflow Pipelines (KFP).
"""

from .base import SchemaAdapter
from .registry import AdapterRegistry

__all__ = ["SchemaAdapter", "AdapterRegistry"]
