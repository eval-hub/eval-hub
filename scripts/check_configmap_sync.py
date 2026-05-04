#!/usr/bin/env python3
"""Check that operator ConfigMaps stay in sync with local config files.

Compares the YAML content embedded in Kubernetes ConfigMaps from the
trustyai-service-operator repo against local collection/provider configs.
Values containing Kustomize substitution variables like $(var) are skipped.
"""

import re
import sys
from pathlib import Path

import requests
import yaml

OPERATOR_REPO = "trustyai-explainability/trustyai-service-operator"
CONFIGMAP_PATH = "config/configmaps/evalhub"
GITHUB_API = (
    f"https://api.github.com/repos/{OPERATOR_REPO}/contents/{CONFIGMAP_PATH}?ref=main"
)
RAW_BASE = f"https://raw.githubusercontent.com/{OPERATOR_REPO}/main/{CONFIGMAP_PATH}"

REPO_ROOT = Path(__file__).resolve().parent.parent
KUSTOMIZE_VAR = re.compile(r"^\$\(.*\)$")

IGNORED_PATHS = {
    "runtime.local.command",
}


def fetch_remote_files():
    """Return list of (filename, type) for collection/provider ConfigMaps."""
    resp = requests.get(GITHUB_API, timeout=30)
    resp.raise_for_status()
    files = []
    for entry in resp.json():
        name = entry["name"]
        if name.startswith("collection-") and name.endswith(".yaml"):
            files.append((name, "collections"))
        elif name.startswith("provider-") and name.endswith(".yaml"):
            files.append((name, "providers"))
    return files


def fetch_configmap_content(filename):
    """Fetch a remote ConfigMap and return its embedded YAML data as a dict."""
    resp = requests.get(f"{RAW_BASE}/{filename}", timeout=30)
    resp.raise_for_status()
    configmap = yaml.safe_load(resp.text)
    data = configmap.get("data", {})
    if not data:
        return None, None
    local_filename = next(iter(data))
    content = yaml.safe_load(data[local_filename])
    return local_filename, content


def load_local(config_type, filename):
    """Load a local config file as a parsed YAML dict."""
    path = REPO_ROOT / "config" / config_type / filename
    if not path.exists():
        return None
    return yaml.safe_load(path.read_text())


def diff_yaml(remote, local, path=""):
    """Recursively compare two parsed YAML structures.

    Yields human-readable diff strings. Skips remote leaf values that
    are Kustomize substitution variables like $(image-name).
    """
    if path in IGNORED_PATHS:
        return

    if isinstance(remote, dict) and isinstance(local, dict):
        all_keys = set(remote) | set(local)
        for key in sorted(all_keys):
            child_path = f"{path}.{key}" if path else key
            if key not in local:
                yield f"  + {child_path}: only in remote"
            elif key not in remote:
                yield f"  - {child_path}: only in local"
            else:
                yield from diff_yaml(remote[key], local[key], child_path)
    elif isinstance(remote, list) and isinstance(local, list):
        for i in range(max(len(remote), len(local))):
            child_path = f"{path}[{i}]"
            if i >= len(local):
                yield f"  + {child_path}: only in remote"
            elif i >= len(remote):
                yield f"  - {child_path}: only in local"
            else:
                yield from diff_yaml(remote[i], local[i], child_path)
    else:
        # leaf content check
        if isinstance(remote, str) and KUSTOMIZE_VAR.match(remote):
            # if trustyai-service-operator leaf is a string and it's a Kustomize variable, skip.
            return
        if remote != local:
            yield f"  ~ {path}: remote={remote!r}  local={local!r}"


def main():
    print(f"Fetching ConfigMap listing from {OPERATOR_REPO}...")
    files = fetch_remote_files()
    print(f"Found {len(files)} ConfigMap(s) to check.\n")

    errors = []

    for filename, config_type in files:
        local_filename, remote_content = fetch_configmap_content(filename)
        if remote_content is None:
            errors.append(f"{filename}: could not extract data from ConfigMap")
            continue

        local_content = load_local(config_type, local_filename)
        if local_content is None:
            errors.append(
                f"{filename}: local file config/{config_type}/{local_filename} not found"
            )
            continue

        diffs = list(diff_yaml(remote_content, local_content))
        if diffs:
            errors.append(f"{filename} vs config/{config_type}/{local_filename}:")
            errors.extend(diffs)
        else:
            print(f"OK  {filename} <-> config/{config_type}/{local_filename}")

    if errors:
        print("\nDrift detected:\n")
        for line in errors:
            print(line)
        sys.exit(1)

    print("\nAll ConfigMaps in sync.")


if __name__ == "__main__":
    main()
