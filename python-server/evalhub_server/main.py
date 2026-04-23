"""Entry point for the eval-hub-server command."""

import subprocess
import sys

from evalhub_server import __version__, get_binary_path


def main(args=None):
    """
    Entry point for the eval-hub-server command.

    Runs the eval-hub binary, passing through command-line arguments.

    Args:
        args: Optional list of command-line arguments to pass to the binary.
              If None, defaults to sys.argv[1:].
    """
    # Use provided args or fall back to sys.argv[1:].
    # args=None means the caller did not supply a list (e.g. setuptools entry point
    # always calls main() with no arguments). In that case read sys.argv at runtime:
    #
    # - eval-hub-server --local ...     → main() called with args=None → uses sys.argv[1:]
    # - python main.py --local ...      → main(sys.argv[1:]) called explicitly → args=['--local', ...]
    # - main(['--local', ...]) in tests → args=['--local', ...] used as-is
    if args is None:
        args = sys.argv[1:]

    # Get the path to the binary
    binary_path = get_binary_path()

    if "--version" in args or "-V" in args:
        print(f"eval-hub-server {__version__}")
        sys.exit(0)

    # Pass all command-line arguments to the binary
    result = subprocess.run([binary_path] + args)
    sys.exit(result.returncode)


if __name__ == "__main__":
    main(sys.argv[1:])
