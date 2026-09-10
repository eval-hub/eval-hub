#!/usr/bin/env python3
"""Tests for Hugging Face init download timeout enforcement."""

from __future__ import annotations

import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from unittest.mock import patch

import hf_download


class TestDownloadProcessTimeout(unittest.TestCase):
    def test_terminates_stalled_thread_pool_promptly(self) -> None:
        stall_seconds = 30.0
        timeout_seconds = 0.5

        def stalled_execute_download(*_args, **_kwargs) -> str:
            with ThreadPoolExecutor(max_workers=8) as executor:
                futures = [executor.submit(time.sleep, stall_seconds) for _ in range(8)]
                for future in futures:
                    future.result()
            return "deadbeef"

        with patch.object(hf_download, "_execute_download", side_effect=stalled_execute_download):
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
                )
            elapsed = time.monotonic() - start

        self.assertIn("download exceeded timeout", str(raised.exception))
        self.assertLess(elapsed, timeout_seconds + 2.0)

    def test_multiple_stalled_downloads_exit_promptly(self) -> None:
        timeout_seconds = 0.4
        stall_seconds = 20.0

        def stalled_execute_download(*_args, **_kwargs) -> str:
            with ThreadPoolExecutor(max_workers=8) as executor:
                futures = [executor.submit(time.sleep, stall_seconds) for _ in range(8)]
                for future in futures:
                    future.result()
            return "deadbeef"

        with patch.object(hf_download, "_execute_download", side_effect=stalled_execute_download):
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
                    )
                elapsed = time.monotonic() - start
                self.assertLess(
                    elapsed,
                    timeout_seconds + 2.0,
                    f"attempt {attempt} took {elapsed:.2f}s",
                )

    def test_propagates_success_from_child(self) -> None:
        with patch.object(hf_download, "_execute_download", return_value="abc123"):
            status, commit_sha, error_message = hf_download._run_download_with_timeout(
                timeout_seconds=5.0,
                repo_id="fake/repo",
                revision=None,
                sub_path="",
                endpoint=None,
                token=None,
                hf_cache_dir=hf_download.DEFAULT_HF_CACHE_DIR,
            )

        self.assertEqual(status, "ok")
        self.assertEqual(commit_sha, "abc123")
        self.assertEqual(error_message, "")

    def test_propagates_worker_error_from_child(self) -> None:
        def failing_execute_download(*_args, **_kwargs) -> str:
            raise RuntimeError("boom")

        with patch.object(hf_download, "_execute_download", side_effect=failing_execute_download):
            status, _commit_sha, error_message = hf_download._run_download_with_timeout(
                timeout_seconds=5.0,
                repo_id="fake/repo",
                revision=None,
                sub_path="",
                endpoint=None,
                token=None,
                hf_cache_dir=hf_download.DEFAULT_HF_CACHE_DIR,
            )

        self.assertEqual(status, "error")
        self.assertEqual(error_message, "boom")


if __name__ == "__main__":
    unittest.main()
