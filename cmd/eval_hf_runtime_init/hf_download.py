#!/usr/bin/env python3
"""Download Hugging Face Hub repository content into /test_data for evaluation jobs."""

from __future__ import annotations

import logging
import math
import os
import re
import shutil
import signal
import sys
from pathlib import Path
from urllib.parse import urlparse

DEST_DIR = Path("/test_data")
METADATA_DIR = Path("/run/init-metadata")
METADATA_FILE = METADATA_DIR / ".git-metadata"
SECRET_DIR = Path("/var/run/secrets/test-data")
TOKEN_KEY = "token"

ENV_REPO_ID = "TEST_DATA_HF_REPO_ID"
ENV_REVISION = "TEST_DATA_HF_REVISION"
ENV_SUB_PATH = "TEST_DATA_HF_SUBPATH"
ENV_DOWNLOAD_TIMEOUT = "TEST_DATA_DOWNLOAD_TIMEOUT"
ENV_HF_ENDPOINT = "HF_ENDPOINT"

DEFAULT_TIMEOUT_SECONDS = 600
# OpenShift/Kubernetes init containers often run as a random UID without $HOME;
# huggingface_hub otherwise tries to write under /.cache and fails with EACCES.
DEFAULT_HF_CACHE_DIR = Path("/tmp/huggingface")
# Kubelet default; surfaced in ContainerStatus.state.terminated.message for the operator.
DEFAULT_TERMINATION_MESSAGE_PATH = Path("/dev/termination-log")
# Kubelet reads at most 4 KiB from the termination message file.
_MAX_TERMINATION_MESSAGE_BYTES = 4096

logger = logging.getLogger(__name__)


class DownloadTimeoutError(TimeoutError):
    """Raised when the Hugging Face download exceeds the configured timeout."""


def _configure_logging() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )


_GO_DURATION_TOKEN_RE = re.compile(r"(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)")
_GO_DURATION_MULTIPLIERS = {
    "ns": 1e-9,
    "us": 1e-6,
    "µs": 1e-6,
    "ms": 1e-3,
    "s": 1.0,
    "m": 60.0,
    "h": 3600.0,
}


def _parse_go_duration_seconds(raw: str) -> float:
    """Parse Go time.ParseDuration strings (for example 10m0s, 15m, 600s)."""
    total = 0.0
    pos = 0
    for match in _GO_DURATION_TOKEN_RE.finditer(raw):
        if match.start() != pos:
            raise ValueError(f"invalid duration: {raw!r}")
        pos = match.end()
        unit = match.group(2)
        if unit == "ns":
            raise ValueError("nanosecond durations are not supported")
        if unit in {"us", "µs"}:
            raise ValueError("microsecond durations are not supported")
        total += float(match.group(1)) * _GO_DURATION_MULTIPLIERS[unit]
    if pos != len(raw) or pos == 0:
        raise ValueError(f"invalid duration: {raw!r}")
    return total


def _parse_timeout_seconds() -> float:
    raw = os.environ.get(ENV_DOWNLOAD_TIMEOUT, "").strip()
    if not raw:
        return float(DEFAULT_TIMEOUT_SECONDS)
    try:
        value = _parse_go_duration_seconds(raw)
    except ValueError as exc:
        raise ValueError(f"invalid {ENV_DOWNLOAD_TIMEOUT}: {exc}") from exc
    if value <= 0:
        raise ValueError(f"invalid {ENV_DOWNLOAD_TIMEOUT}: must be a positive duration")
    return value


def _configure_hf_hub_timeouts(seconds: float) -> None:
    # huggingface_hub expects integer-second timeout env vars.
    hub_timeout = str(math.ceil(seconds))
    os.environ["HF_HUB_ETAG_TIMEOUT"] = hub_timeout
    os.environ["HF_HUB_DOWNLOAD_TIMEOUT"] = hub_timeout


def _configure_hf_cache() -> Path:
    cache_root = Path(os.environ.get("HF_HOME", "").strip() or DEFAULT_HF_CACHE_DIR)
    hub_cache = cache_root / "hub"
    hub_cache.mkdir(parents=True, exist_ok=True)
    os.environ["HF_HOME"] = str(cache_root)
    os.environ["HUGGINGFACE_HUB_CACHE"] = str(hub_cache)
    return hub_cache


def _validate_hf_endpoint(endpoint: str) -> str | None:
    endpoint = endpoint.strip()
    if not endpoint:
        return None
    parsed = urlparse(endpoint)
    if parsed.scheme != "https":
        raise ValueError(f"{ENV_HF_ENDPOINT} must use https scheme")
    if not parsed.hostname:
        raise ValueError(f"{ENV_HF_ENDPOINT} must include a hostname")
    return endpoint


def _read_token() -> str | None:
    token_path = SECRET_DIR / TOKEN_KEY
    if not token_path.is_file():
        return None
    token = token_path.read_text(encoding="utf-8").strip()
    return token or None


def _write_metadata(commit_sha: str) -> None:
    METADATA_DIR.mkdir(parents=True, exist_ok=True)
    METADATA_FILE.write_text(commit_sha + "\n", encoding="utf-8")


def _copy_tree(src: Path, dst: Path) -> None:
    if not src.is_dir():
        raise FileNotFoundError(f"source path {src} is not a directory")
    dst.mkdir(parents=True, exist_ok=True)
    for item in src.iterdir():
        target = dst / item.name
        if item.is_dir():
            shutil.copytree(item, target, dirs_exist_ok=True)
        else:
            shutil.copy2(item, target)


def _validate_sub_path(sub_path: str) -> str:
    clean = Path(sub_path)
    if clean.is_absolute() or ".." in clean.parts:
        raise ValueError(f"sub_path escapes repository root: {sub_path!r}")
    return sub_path.strip().strip("/")


def _sub_path_allow_patterns(sub_path: str) -> list[str]:
    normalized = _validate_sub_path(sub_path)
    return [normalized, f"{normalized}/**"]


def _stage_sub_path(download_root: Path, sub_path: str, dest: Path) -> None:
    clean = Path(_validate_sub_path(sub_path))
    source = download_root / clean
    if not source.exists():
        raise FileNotFoundError(f"sub_path {sub_path!r} not found in repository")
    if source.is_dir():
        _copy_tree(source, dest)
    else:
        dest.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, dest / source.name)


def _clear_dest_dir(dest: Path) -> None:
    """Remove prior contents under dest while preserving the mount directory itself."""
    dest.mkdir(parents=True, exist_ok=True)
    for item in dest.iterdir():
        if item.is_dir():
            shutil.rmtree(item)
        else:
            item.unlink()


def _dest_has_data(root: Path) -> bool:
    """Return True when root contains at least one staged entry."""
    if not root.is_dir():
        return False
    return any(root.iterdir())


def _install_timeout(seconds: float) -> None:
    if seconds <= 0:
        return

    def _handle_timeout(signum, frame):  # noqa: ARG001
        raise DownloadTimeoutError(f"download exceeded timeout of {seconds:g}s")

    signal.signal(signal.SIGALRM, _handle_timeout)
    signal.setitimer(signal.ITIMER_REAL, seconds)


def _clear_timeout() -> None:
    signal.setitimer(signal.ITIMER_REAL, 0)


def _termination_message_path() -> Path:
    raw = os.environ.get("TERMINATION_MESSAGE_PATH", "").strip()
    if raw:
        return Path(raw)
    return DEFAULT_TERMINATION_MESSAGE_PATH


def _write_termination_message(message: str) -> None:
    text = message.strip()
    if not text:
        return
    encoded = text.encode("utf-8")
    if len(encoded) > _MAX_TERMINATION_MESSAGE_BYTES:
        encoded = encoded[:_MAX_TERMINATION_MESSAGE_BYTES]
        text = encoded.decode("utf-8", errors="ignore")
    path = _termination_message_path()
    try:
        path.write_text(text + "\n", encoding="utf-8")
    except OSError as err:
        logger.warning("failed to write termination message to %s: %s", path, err)


def _fail(message: str) -> int:
    logger.error("%s", message)
    _write_termination_message(message)
    return 1


def main() -> int:
    _configure_logging()
    hf_cache_dir = _configure_hf_cache()

    repo_id = os.environ.get(ENV_REPO_ID, "").strip()
    if not repo_id:
        return _fail(f"{ENV_REPO_ID} is required")

    revision = os.environ.get(ENV_REVISION, "").strip() or None
    sub_path = os.environ.get(ENV_SUB_PATH, "").strip()

    try:
        endpoint = _validate_hf_endpoint(os.environ.get(ENV_HF_ENDPOINT, ""))
        timeout_seconds = _parse_timeout_seconds()
    except ValueError as err:
        return _fail(str(err))

    if endpoint:
        os.environ["HF_ENDPOINT"] = endpoint

    _configure_hf_hub_timeouts(timeout_seconds)
    from huggingface_hub import HfApi, snapshot_download
    from huggingface_hub.utils import GatedRepoError, RepositoryNotFoundError

    token = _read_token()
    api = HfApi(endpoint=endpoint or None, token=token)

    logger.info("resolving revision for repo_id=%s revision=%s", repo_id, revision or "default")
    _install_timeout(timeout_seconds)
    commit_sha = ""
    try:
        info = api.repo_info(repo_id=repo_id, revision=revision, repo_type="dataset")
        commit_sha = info.sha
        if not commit_sha:
            raise RuntimeError(f"could not resolve commit SHA for {repo_id}")

        logger.info("downloading repository repo_id=%s revision=%s", repo_id, commit_sha)
        snapshot_kwargs = {
            "repo_id": repo_id,
            "repo_type": "dataset",
            "revision": commit_sha,
            "token": token,
            "endpoint": endpoint or None,
        }
        _clear_dest_dir(DEST_DIR)
        if sub_path:
            download_root = Path(
                snapshot_download(
                    **snapshot_kwargs,
                    cache_dir=str(hf_cache_dir),
                    allow_patterns=_sub_path_allow_patterns(sub_path),
                )
            )
            _stage_sub_path(download_root, sub_path, DEST_DIR)
        else:
            # Download into /tmp cache and copy into DEST_DIR. Using local_dir=DEST_DIR
            # on OpenShift writes staging files under /test_data/.cache; permission
            # errors there can leave DEST_DIR empty after metadata cleanup.
            download_root = Path(
                snapshot_download(
                    **snapshot_kwargs,
                    cache_dir=str(hf_cache_dir),
                )
            )
            _copy_tree(download_root, DEST_DIR)

        if not _dest_has_data(DEST_DIR):
            raise RuntimeError(f"no files were staged under {DEST_DIR}")

        _write_metadata(commit_sha)
        logger.info("hf source ready dest=%s commit_sha=%s", DEST_DIR, commit_sha)
    except DownloadTimeoutError as err:
        return _fail(str(err))
    except GatedRepoError:
        return _fail(
            f"repository {repo_id} is gated; provide secret_ref with a Hugging Face token"
        )
    except RepositoryNotFoundError as err:
        return _fail(f"repository not found: {err}")
    except Exception as err:  # noqa: BLE001
        return _fail(f"hf download failed: {err}")
    finally:
        _clear_timeout()

    return 0


if __name__ == "__main__":
    sys.exit(main())
