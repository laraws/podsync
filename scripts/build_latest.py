#!/usr/bin/env python3
"""Build and push latest for the host architecture, or explicitly selected platforms."""

import argparse
from pathlib import Path
import platform
import shlex
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
IMAGE = "ghcr.io/laraws/podsync:latest"


def host_platform():
    machine = platform.machine().lower()
    architectures = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}
    if machine not in architectures:
        raise ValueError(f"unsupported host architecture: {machine}; specify --platform explicitly")
    return "linux/" + architectures[machine]


def build_platforms(value):
    platforms = value.split(",")
    if any(item not in {"linux/arm64", "linux/amd64"} for item in platforms):
        raise argparse.ArgumentTypeError("use linux/arm64, linux/amd64, or linux/arm64,linux/amd64")
    return ",".join(dict.fromkeys(platforms))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", type=build_platforms, help="comma-separated target platforms (default: host architecture)")
    parser.add_argument("--dry-run", action="store_true", help="show the command without building or pushing")
    args = parser.parse_args()
    try:
        target_platforms = args.platform or host_platform()
    except ValueError as error:
        parser.error(str(error))
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip():
        commit += "-dirty"
    command = [
        "docker", "buildx", "build",
        "--platform", target_platforms,
        "--build-arg", "TAG=latest",
        "--build-arg", "COMMIT=" + commit,
        "--label", "org.opencontainers.image.source=https://github.com/laraws/podsync",
        "--label", "org.opencontainers.image.revision=" + commit,
        "--tag", IMAGE, "--push", ".",
    ]
    print(shlex.join(command), flush=True)
    if args.dry_run:
        return
    started = time.perf_counter()
    subprocess.run(command, cwd=ROOT, check=True)
    elapsed = time.perf_counter() - started
    print(f"发布成功：{IMAGE}；耗时 {int(elapsed // 60)} 分 {elapsed % 60:.1f} 秒。")


if __name__ == "__main__":
    try:
        main()
    except (OSError, subprocess.CalledProcessError) as error:
        print(f"失败：{error}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        sys.exit(130)
