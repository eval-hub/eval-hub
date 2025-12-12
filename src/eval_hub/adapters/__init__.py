"""Schema adapters for evaluation frameworks.

This module provides the adapter layer for transforming between eval-hub's
internal representation and framework-specific formats. Adapters enable
integration with various evaluation frameworks via Kubeflow Pipelines (KFP).
"""

from .base import SchemaAdapter
from .frameworks.garak import GarakAdapter
from .frameworks.lighteval import LightevalAdapter
from .registry import AdapterRegistry

# Auto-register built-in adapters
AdapterRegistry.register("lighteval", LightevalAdapter)
AdapterRegistry.register("garak", GarakAdapter)

__all__ = ["SchemaAdapter", "AdapterRegistry", "LightevalAdapter", "GarakAdapter"]
