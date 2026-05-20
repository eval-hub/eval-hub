"""Tests for evalhub_server.get_binary_path binary resolution."""

from unittest.mock import patch

import pytest

from evalhub_server import get_binary_path


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="x86_64")
@patch("evalhub_server.platform.system", return_value="Linux")
def test_returns_platform_specific_binary(mock_system, mock_machine, tmp_path):
    binaries = tmp_path / "binaries"
    binaries.mkdir()
    specific = binaries / "eval-hub-linux-amd64"
    specific.touch()

    with patch("evalhub_server.Path") as mock_cls:
        mock_cls.return_value.parent = tmp_path
        result = get_binary_path()

    assert result == str(specific)


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="x86_64")
@patch("evalhub_server.platform.system", return_value="Linux")
def test_falls_back_to_generic_on_linux(mock_system, mock_machine, tmp_path):
    binaries = tmp_path / "binaries"
    binaries.mkdir()
    generic = binaries / "eval-hub"
    generic.touch()

    with patch("evalhub_server.Path") as mock_cls:
        mock_cls.return_value.parent = tmp_path
        result = get_binary_path()

    assert result == str(generic)


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="arm64")
@patch("evalhub_server.platform.system", return_value="Darwin")
def test_falls_back_to_generic_on_macos(mock_system, mock_machine, tmp_path):
    binaries = tmp_path / "binaries"
    binaries.mkdir()
    generic = binaries / "eval-hub"
    generic.touch()

    with patch("evalhub_server.Path") as mock_cls:
        mock_cls.return_value.parent = tmp_path
        result = get_binary_path()

    assert result == str(generic)


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="AMD64")
@patch("evalhub_server.platform.system", return_value="Windows")
def test_falls_back_to_generic_exe_on_windows(mock_system, mock_machine, tmp_path):
    binaries = tmp_path / "binaries"
    binaries.mkdir()
    generic = binaries / "eval-hub.exe"
    generic.touch()

    with patch("evalhub_server.Path") as mock_cls:
        mock_cls.return_value.parent = tmp_path
        result = get_binary_path()

    assert result == str(generic)


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="x86_64")
@patch("evalhub_server.platform.system", return_value="Linux")
def test_prefers_platform_specific_over_generic(mock_system, mock_machine, tmp_path):
    binaries = tmp_path / "binaries"
    binaries.mkdir()
    specific = binaries / "eval-hub-linux-amd64"
    specific.touch()
    generic = binaries / "eval-hub"
    generic.touch()

    with patch("evalhub_server.Path") as mock_cls:
        mock_cls.return_value.parent = tmp_path
        result = get_binary_path()

    assert result == str(specific)


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="x86_64")
@patch("evalhub_server.platform.system", return_value="Linux")
def test_raises_when_no_binary_found(mock_system, mock_machine, tmp_path):
    binaries = tmp_path / "binaries"
    binaries.mkdir()

    with patch("evalhub_server.Path") as mock_cls:
        mock_cls.return_value.parent = tmp_path
        with pytest.raises(FileNotFoundError, match="Binary not found"):
            get_binary_path()


@pytest.mark.unit
@patch("evalhub_server.platform.machine", return_value="x86_64")
@patch("evalhub_server.platform.system", return_value="FooOS")
def test_raises_for_unsupported_platform(mock_system, mock_machine):
    with pytest.raises(RuntimeError, match="Unsupported platform"):
        get_binary_path()
