"""Generate a standalone request using the collection's HLE configuration."""

import copy
import json
from pathlib import Path

import yaml

EXAMPLE = Path(__file__).resolve().parent
ROOT = EXAMPLE.parents[1]

collection = yaml.safe_load(
    (ROOT / "config/collections/knowledge-reasoning-v1.yaml").read_text()
)
hle = next(b for b in collection["benchmarks"] if b["id"] == "inspect/hle")
request = json.loads((EXAMPLE / "request.json").read_text())
benchmark = request["benchmarks"][0]
parameters = copy.deepcopy(hle["parameters"])
parameters.update(benchmark["parameters"])
benchmark["parameters"] = parameters
benchmark["primary_score"] = copy.deepcopy(hle["primary_score"])
print(json.dumps(request, indent=2))
