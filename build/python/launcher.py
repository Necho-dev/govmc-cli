"""Launch the govmc binary installed by the platform wheel."""

import os
from pathlib import Path
import subprocess
import sys


def main() -> int:
    package = Path(__file__).resolve().parent
    binary = package / ("govmc.exe" if os.name == "nt" else "govmc")
    env = os.environ.copy()
    argv = [str(binary), *sys.argv[1:]]
    if os.name != "nt":
        os.execve(binary, argv, env)
    try:
        return subprocess.call(argv, env=env)
    except KeyboardInterrupt:
        return 130
