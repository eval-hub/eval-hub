"""Integration tests — start the real server and hit its REST API."""

import json
import os
import signal
import socket
import subprocess
import time
import urllib.request
from pathlib import Path

import pytest
import yaml

from evalhub_server import get_binary_path

# Resolve paths relative to the repo root (two levels up from this file).
REPO_ROOT = Path(__file__).resolve().parents[2]
CONFIG_DIR = REPO_ROOT / "config"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _free_port() -> int:
    """Return an unused TCP port on localhost."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _get(base_url: str, path: str) -> dict:
    """GET *path* and return parsed JSON."""
    with urllib.request.urlopen(f"{base_url}{path}", timeout=10) as resp:
        return json.loads(resp.read())


def _load_yaml_field(directory: Path, field: str) -> set[str]:
    """Load all YAML files in *directory* and return a set of *field* values."""
    values = set()
    for f in sorted(directory.glob("*.yaml")):
        with open(f) as fh:
            values.add(yaml.safe_load(fh)[field])
    return values


# ---------------------------------------------------------------------------
# Fixture: running the real binary server
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def server_url():
    """Start the eval-hub binary and yield its base URL."""
    port = _free_port()
    binary = get_binary_path()

    env = {**os.environ, "PORT": str(port)}
    proc = subprocess.Popen(
        [binary, "--local", "--configdir", str(CONFIG_DIR)],
        env=env,
    )

    base_url = f"http://127.0.0.1:{port}"
    deadline = time.monotonic() + 15
    ready = False
    while time.monotonic() < deadline:
        try:
            health = _get(base_url, "/api/v1/health")
            providers = _get(base_url, "/api/v1/evaluations/providers")
            if health.get("status") == "healthy" and providers.get("items"):
                ready = True
                break
        except Exception:
            pass
        time.sleep(0.3)
    if not ready:
        proc.kill()
        pytest.fail("Server did not become ready within 15 s")

    yield base_url

    proc.send_signal(signal.SIGTERM)
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

@pytest.mark.integration
def test_health(server_url):
    data = _get(server_url, "/api/v1/health")
    assert "status" in data


@pytest.mark.integration
def test_list_providers(server_url):
    expected_names = _load_yaml_field(CONFIG_DIR / "providers", "name")
    data = _get(server_url, "/api/v1/evaluations/providers")
    actual_names = {item["name"] for item in (data.get("items") or [])}
    assert actual_names == expected_names


@pytest.mark.integration
def test_list_collections(server_url):
    expected_names = _load_yaml_field(CONFIG_DIR / "collections", "name")
    data = _get(server_url, "/api/v1/evaluations/collections")
    actual_names = {item["name"] for item in (data.get("items") or [])}
    assert actual_names == expected_names
