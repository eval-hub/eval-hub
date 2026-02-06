"""Entry point for the eval-hub-server command."""

import subprocess
import sys

from evalhub_server import get_binary_path


def main(args=None):
    """
    Entry point for the eval-hub-server command.

    Runs the eval-hub binary, passing through command-line arguments.

    Args:
        args: Optional list of command-line arguments to pass to the binary.
              If None, defaults to an empty list.
    """
    # Get the path to the binary
    binary_path = get_binary_path()

    # Use provided args or default to empty list
    args = args or []

    # Pass all command-line arguments to the binary
    result = subprocess.run([binary_path] + args)
    sys.exit(result.returncode)


if __name__ == "__main__":
    main(sys.argv[1:])
