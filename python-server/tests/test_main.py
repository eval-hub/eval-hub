"""Tests for evalhub_server.main entry point."""

import sys
from unittest.mock import MagicMock, patch

import pytest

from evalhub_server import __version__
from evalhub_server.main import main


@pytest.mark.unit
@patch("evalhub_server.main.get_binary_path", return_value="/fake/eval-hub")
@patch("evalhub_server.main.subprocess.run")
@patch("evalhub_server.main.sys.exit")
def test_setuptools_entrypoint_forwards_argv(mock_exit, mock_run, mock_path):
    """Setuptools calls main() with no args; sys.argv flags must be forwarded to the binary.

    This test reproduces the exact invocation path of the installed CLI command:
        eval-hub-server --local --configdir ./config
    where setuptools generates a wrapper that calls main() with no arguments.
    """
    mock_run.return_value = MagicMock(returncode=0)

    with patch.object(
        sys, "argv", ["eval-hub-server", "--local", "--configdir", "/tmp/config"]
    ):
        main()  # no args, exactly as setuptools calls it

    mock_run.assert_called_once_with(
        ["/fake/eval-hub", "--local", "--configdir", "/tmp/config"]
    )
    mock_exit.assert_called_once_with(0)


@pytest.mark.unit
@patch("evalhub_server.main.get_binary_path", return_value="/fake/eval-hub")
@patch("evalhub_server.main.subprocess.run")
@patch("evalhub_server.main.sys.exit")
def test_explicit_args_override_argv(mock_exit, mock_run, mock_path):
    """Explicit args passed to main() are used as-is; sys.argv must be ignored.

    This covers programmatic use (e.g. tests calling main([...]) directly)
    where sys.argv may contain unrelated flags (e.g. pytest's own arguments).
    """
    mock_run.return_value = MagicMock(returncode=0)

    with patch.object(sys, "argv", ["pytest", "--tb=short", "-v"]):
        main(["--local", "--configdir", "/tmp/config"])

    mock_run.assert_called_once_with(
        ["/fake/eval-hub", "--local", "--configdir", "/tmp/config"]
    )
    mock_exit.assert_called_once_with(0)


@pytest.mark.unit
@patch("evalhub_server.main.get_binary_path", return_value="/fake/eval-hub")
@patch("evalhub_server.main.subprocess.run")
@patch("evalhub_server.main.sys.exit")
def test_binary_exit_code_propagated(mock_exit, mock_run, mock_path):
    """Non-zero exit code from the binary is propagated via sys.exit."""
    mock_run.return_value = MagicMock(returncode=1)

    with patch.object(sys, "argv", ["eval-hub-server"]):
        main()

    mock_exit.assert_called_once_with(1)


@pytest.mark.unit
def test_version_flag_prints_version(capsys):
    with pytest.raises(SystemExit) as exc_info:
        main(["--version"])
    assert exc_info.value.code == 0
    captured = capsys.readouterr()
    assert f"eval-hub-server {__version__}" in captured.out


@pytest.mark.unit
def test_version_short_flag(capsys):
    with pytest.raises(SystemExit) as exc_info:
        main(["-V"])
    assert exc_info.value.code == 0
    captured = capsys.readouterr()
    assert f"eval-hub-server {__version__}" in captured.out


@pytest.mark.unit
@patch("evalhub_server.main.get_binary_path")
def test_version_flag_does_not_invoke_binary(mock_path):
    with pytest.raises(SystemExit):
        main(["--version"])
    mock_path.assert_not_called()
