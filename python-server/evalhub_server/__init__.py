"""eval-hub server binary provider."""

import platform
from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as _pkg_version
from pathlib import Path

try:
    __version__ = _pkg_version("eval-hub-server")
except PackageNotFoundError:
    __version__ = "0.0.0"


def get_binary_path():
    """
    Get the path to the platform-specific eval-hub binary.

    Returns:
        str: Absolute path to the binary

    Raises:
        FileNotFoundError: If binary for current platform is not found
        RuntimeError: If platform is not supported
    """
    system = platform.system().lower()
    machine = platform.machine().lower()

    # Determine binary name
    if system == "windows":
        binary_name = "eval-hub-windows-amd64.exe"
    elif system == "darwin":
        binary_name = (
            "eval-hub-darwin-arm64" if machine == "arm64" else "eval-hub-darwin-amd64"
        )
    elif system == "linux":
        if "aarch64" in machine or "arm64" in machine:
            binary_name = "eval-hub-linux-arm64"
        else:
            binary_name = "eval-hub-linux-amd64"
    else:
        raise RuntimeError(f"Unsupported platform: {system} {machine}")

    # Find binary in package
    package_dir = Path(__file__).parent
    binary_path = package_dir / "binaries" / binary_name

    if not binary_path.exists():
        raise FileNotFoundError(
            f"Binary not found: {binary_path}\n"
            f"This package may not support your platform ({system} {machine})"
        )

    return str(binary_path)


__all__ = ["__version__", "get_binary_path"]
