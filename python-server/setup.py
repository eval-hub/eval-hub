import os
from pathlib import Path

from setuptools import setup
from wheel.bdist_wheel import bdist_wheel


class PlatformSpecificWheel(bdist_wheel):
    """Force wheel to be platform-specific.

    Supports platform override via WHEEL_PLATFORM environment variable
    for cross-platform wheel building in CI.
    """

    def finalize_options(self):
        bdist_wheel.finalize_options(self)
        self.root_is_pure = False

    def get_tag(self):
        python, abi, plat = bdist_wheel.get_tag(self)
        platform_override = os.environ.get("WHEEL_PLATFORM")
        if platform_override:
            plat = platform_override
        python, abi = "py3", "none"
        return python, abi, plat


def _find_files(subdir, exclude=None):
    d = Path(__file__).parent / subdir
    if not d.exists():
        return []
    return [os.path.join(subdir, f.name) for f in d.iterdir() if f.name != exclude]


wheel_platform = os.environ.get("WHEEL_PLATFORM", "")
bin_dir = "Scripts" if "win" in wheel_platform else "bin"

setup(
    cmdclass={"bdist_wheel": PlatformSpecificWheel},
    data_files=[
        (bin_dir, _find_files("binaries", exclude=".gitkeep") + _find_files("shims"))
    ],
)
