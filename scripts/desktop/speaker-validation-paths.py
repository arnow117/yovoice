#!/usr/bin/env python3
"""Normalize validation inputs without resolving a configured Python symlink."""

from __future__ import annotations

import os
import sys


def absolute_path(repository: str, value: str) -> str:
    """Anchor relative inputs at the repository while preserving executable symlinks."""
    root = os.path.abspath(os.path.expanduser(repository))
    candidate = os.path.expanduser(value)
    if not os.path.isabs(candidate):
        candidate = os.path.join(root, candidate)
    return os.path.abspath(candidate)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: speaker-validation-paths.py REPOSITORY PATH")
    print(absolute_path(sys.argv[1], sys.argv[2]))
