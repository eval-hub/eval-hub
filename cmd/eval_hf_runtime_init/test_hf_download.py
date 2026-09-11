#!/usr/bin/env python3
"""Tests for Hugging Face init download timeout enforcement."""

from __future__ import annotations

import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from functools import partial

import hf_download


def _execute_download_stalled(stall_seconds: float, *_args, **_kwargs) -> str:
    with ThreadPoolExecutor(max_workers=8) as executor:
        futures = [executor.submit(time.sleep, stall_seconds) for _ in range(8)]
        for future in futures:
            future.result()
    return "deadbeef"


def _success_execute_download(*_args, **_kwargs) -> str:
    return "abc123"


def _failing_execute_download(*_args, **_kwargs) -> str:
    raise RuntimeError("boom")


class TestDownloadProcessTimeout(unittest.TestCase):
    def test_terminates_stalled_thread_pool_promptly(self) -> None:
        timeout_seconds = 0.5

        start = time.monotonic()
        with self.assertRaises(hf_download.DownloadTimeoutError) as raised:
            hf_download._run_download_with_timeout(
                timeout_seconds=timeout_seconds,
                repo_id="fake/repo",
                revision=None,
                sub_path="",
                endpoint=None,
                token=None,
                hf_cache_dir=hf_download.DEFAULT_HF_CACHE_DIR,
                execute_download=partial(_execute_download_stalled, 30.0),
            )
        elapsed = time.monotonic() - start

        self.assertIn("download exceeded timeout", str(raised.exception))
        self.assertLess(elapsed, timeout_seconds + 2.0)

    def test_multiple_stalled_downloads_exit_promptly(self) -> None:
        timeout_seconds = 0.4

        for attempt in range(3):
            start = time.monotonic()
            with self.assertRaises(hf_download.DownloadTimeoutError):
                hf_download._run_download_with_timeout(
                    timeout_seconds=timeout_seconds,
                    repo_id=f"fake/repo-{attempt}",
                    revision=None,
                    sub_path="",
                    endpoint=None,
                    token=None,
                    hf_cache_dir=hf_download.DEFAULT_HF_CACHE_DIR,
                    execute_download=partial(_execute_download_stalled, 20.0),
                )
            elapsed = time.monotonic() - start
            self.assertLess(
                elapsed,
                timeout_seconds + 2.0,
                f"attempt {attempt} took {elapsed:.2f}s",
            )

    def test_propagates_success_from_child(self) -> None:
        status, commit_sha, error_message = hf_download._run_download_with_timeout(
            timeout_seconds=5.0,
            repo_id="fake/repo",
            revision=None,
            sub_path="",
            endpoint=None,
            token=None,
            hf_cache_dir=hf_download.DEFAULT_HF_CACHE_DIR,
            execute_download=_success_execute_download,
        )

        self.assertEqual(status, "ok")
        self.assertEqual(commit_sha, "abc123")
        self.assertEqual(error_message, "")

    def test_propagates_worker_error_from_child(self) -> None:
        status, _commit_sha, error_message = hf_download._run_download_with_timeout(
            timeout_seconds=5.0,
            repo_id="fake/repo",
            revision=None,
            sub_path="",
            endpoint=None,
            token=None,
            hf_cache_dir=hf_download.DEFAULT_HF_CACHE_DIR,
            execute_download=_failing_execute_download,
        )

        self.assertEqual(status, "error")
        self.assertEqual(error_message, "boom")


if __name__ == "__main__":
    unittest.main()
