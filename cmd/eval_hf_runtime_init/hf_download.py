#!/usr/bin/env python3
"""Download Hugging Face Hub repository content into /test_data for evaluation jobs."""

from __future__ import annotations

import logging
import math
import multiprocessing
import os
import re
import shutil
import sys
from pathlib import Path
from typing import TYPE_CHECKING
from urllib.parse import urlparse

if TYPE_CHECKING:
    from multiprocessing.context import BaseContext

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
_PROCESS_TERMINATE_GRACE_SECONDS = 5.0

logger = logging.getLogger(__name__)

# (status, commit_sha, error_message)
DownloadResult = tuple[str, str, str]


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


def _multiprocessing_context() -> BaseContext:
    try:
        return multiprocessing.get_context("fork")
    except ValueError:
        return multiprocessing.get_context("spawn")


def _terminate_stuck_process(process: multiprocessing.Process) -> None:
    process.terminate()
    process.join(_PROCESS_TERMINATE_GRACE_SECONDS)
    if process.is_alive():
        process.kill()
        process.join()


def _worker_error_result(err: BaseException) -> DownloadResult:
    try:
        from huggingface_hub.utils import GatedRepoError, RepositoryNotFoundError
    except ModuleNotFoundError:
        return ("error", "", str(err))

    if isinstance(err, GatedRepoError):
        return ("gated", "", "")
    if isinstance(err, RepositoryNotFoundError):
        return ("not_found", "", str(err))
    return ("error", "", str(err))


def _execute_download(
    repo_id: str,
    revision: str | None,
    sub_path: str,
    endpoint: str | None,
    token: str | None,
    hf_cache_dir: Path,
) -> str:
    from huggingface_hub import HfApi, snapshot_download

    api = HfApi(endpoint=endpoint, token=token)

    logger.info("resolving revision for repo_id=%s revision=%s", repo_id, revision or "default")
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
        "endpoint": endpoint,
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

    return commit_sha


def _download_process_worker(
    repo_id: str,
    revision: str | None,
    sub_path: str,
    endpoint: str | None,
    token: str | None,
    hf_cache_dir: str,
    timeout_seconds: float,
    result_queue: multiprocessing.Queue,
) -> None:
    try:
        _configure_logging()
        _configure_hf_hub_timeouts(timeout_seconds)
        commit_sha = _execute_download(
            repo_id=repo_id,
            revision=revision,
            sub_path=sub_path,
            endpoint=endpoint,
            token=token,
            hf_cache_dir=Path(hf_cache_dir),
        )
        result_queue.put(("ok", commit_sha, ""))
    except Exception as err:  # noqa: BLE001
        result_queue.put(_worker_error_result(err))


def _run_download_with_timeout(
    timeout_seconds: float,
    repo_id: str,
    revision: str | None,
    sub_path: str,
    endpoint: str | None,
    token: str | None,
    hf_cache_dir: Path,
) -> DownloadResult:
    ctx = _multiprocessing_context()
    result_queue = ctx.Queue()
    process = ctx.Process(
        target=_download_process_worker,
        args=(
            repo_id,
            revision,
            sub_path,
            endpoint,
            token,
            str(hf_cache_dir),
            timeout_seconds,
            result_queue,
        ),
    )
    process.start()
    process.join(timeout_seconds)
    if process.is_alive():
        _terminate_stuck_process(process)
        raise DownloadTimeoutError(f"download exceeded timeout of {timeout_seconds:g}s")

    if result_queue.empty():
        exit_code = process.exitcode
        raise RuntimeError(f"download worker exited without reporting a result (exit code {exit_code})")

    return result_queue.get()


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

    try:
        status, commit_sha, error_message = _run_download_with_timeout(
            timeout_seconds=timeout_seconds,
            repo_id=repo_id,
            revision=revision,
            sub_path=sub_path,
            endpoint=endpoint,
            token=_read_token(),
            hf_cache_dir=hf_cache_dir,
        )
    except DownloadTimeoutError as err:
        return _fail(str(err))
    except Exception as err:  # noqa: BLE001
        return _fail(f"hf download failed: {err}")

    if status == "ok":
        _write_metadata(commit_sha)
        logger.info("hf source ready dest=%s commit_sha=%s", DEST_DIR, commit_sha)
        return 0
    if status == "gated":
        return _fail(
            f"repository {repo_id} is gated; provide secret_ref with a Hugging Face token"
        )
    if status == "not_found":
        return _fail(f"repository not found: {error_message}")
    return _fail(f"hf download failed: {error_message}")


if __name__ == "__main__":
    sys.exit(main())
