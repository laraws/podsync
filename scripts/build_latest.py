#!/usr/bin/env python3
"""Build and push ARM64/AMD64 latest on the machine running this script."""

import argparse
from pathlib import Path
import shlex
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
IMAGE = "ghcr.io/laraws/podsync:latest"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="show the command without building or pushing")
    args = parser.parse_args()
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip():
        commit += "-dirty"
    command = [
        "docker", "buildx", "build",
        "--platform", "linux/arm64,linux/amd64",
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
