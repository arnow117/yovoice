#!/usr/bin/env python3
"""生成验证包内仅转发参数的 POSIX 可执行包装器。"""

import argparse
import shlex
import stat
from pathlib import Path


def write_wrapper(destination: Path, executable: Path) -> None:
    quoted_executable = shlex.quote(str(executable))
    destination.write_text(f'#!/bin/sh\nexec {quoted_executable} "$@"\n')
    destination.chmod(destination.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def main() -> None:
    parser = argparse.ArgumentParser(description="write a literal POSIX executable wrapper")
    parser.add_argument("destination", type=Path)
    parser.add_argument("executable", type=Path)
    arguments = parser.parse_args()
    write_wrapper(arguments.destination, arguments.executable)


if __name__ == "__main__":
    main()
