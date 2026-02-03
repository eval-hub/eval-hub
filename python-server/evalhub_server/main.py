"""Entry point for the eval-hub-server command."""

import subprocess
import sys

from evalhub_server import get_binary_path


def main():
    """
    Entry point for the eval-hub-server command.

    Runs the eval-hub binary, passing through all command-line arguments.
    """
    # Get the path to the binary
    binary_path = get_binary_path()

    # Pass all command-line arguments to the binary
    result = subprocess.run([binary_path] + sys.argv[1:])
    sys.exit(result.returncode)


if __name__ == "__main__":
    main()
