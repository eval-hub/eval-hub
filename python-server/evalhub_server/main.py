"""Entry point for the eval-hub-server command."""

import subprocess
import sys

from evalhub_server import __version__, get_binary_path


def main(args=None):
    if args is None:
        args = sys.argv[1:]

    if "--version" in args or "-V" in args:
        print(f"eval-hub-server {__version__}")
        sys.exit(0)

    binary_path = get_binary_path()
    result = subprocess.run([binary_path] + args)
    sys.exit(result.returncode)


if __name__ == "__main__":
    main(sys.argv[1:])
