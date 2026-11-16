#!/usr/bin/env python3
"""Reject ELF binaries requiring glibc symbols newer than the build baseline."""
import re
import os
import subprocess
import sys


def main():
    if len(sys.argv) != 3 or not re.fullmatch(r"\d+\.\d+", sys.argv[2]):
        raise SystemExit("Usage: check-linux-glibc.py ELF MAX_GLIBC (e.g. 2.35)")
    result = subprocess.run(["readelf", "--version-info", "--wide", sys.argv[1]],
                            check=True, capture_output=True, text=True,
                            env={**os.environ, "LC_ALL": "C"})
    versions = {tuple(map(int, match.split("."))) for match in
                re.findall(r"Name: GLIBC_(\d+\.\d+(?:\.\d+)?)\b", result.stdout)}
    if not versions:
        raise SystemExit("No GLIBC version requirements found in ELF")
    maximum = tuple(map(int, sys.argv[2].split(".")))
    actual = max(versions)
    print("Highest required glibc: " + ".".join(map(str, actual)))
    if actual > maximum:
        raise SystemExit("glibc requirement exceeds baseline " + sys.argv[2])


if __name__ == "__main__":
    main()
