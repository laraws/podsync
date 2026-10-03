#!/usr/bin/env python3
"""Test HEAD, push a release tag, and wait for GitHub Actions."""

import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time


ROOT = Path(__file__).resolve().parent.parent


def run(*command, show_output=False):
    result = subprocess.run(
        command, cwd=ROOT, check=True, text=True, capture_output=not show_output
    )
    return result.stdout.strip() if not show_output else ""


def version_number(tag):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("Version must be vMAJOR.MINOR.PATCH without leading zeros.")
    return tuple(int(part) for part in tag[1:].split("."))


def require_clean_tree():
    if run("git", "status", "--porcelain"):
        raise ValueError("Commit or stash working tree changes before publishing.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", nargs="?", help="default: increment the latest remote patch version")
    parser.add_argument("--dry-run", action="store_true", help="preview without tests, tags, or pushes")
    args = parser.parse_args()
    for tool in ("git", "gh", "go"):
        if not shutil.which(tool):
            raise ValueError(f"Required command missing: {tool}")
    run("gh", "auth", "status")
    remote = run("git", "remote", "get-url", "origin")
    repo = run("gh", "repo", "view", remote, "--json", "nameWithOwner", "--jq", ".nameWithOwner")
    commit = run("git", "rev-parse", "HEAD")
    remote_tags = run("git", "ls-remote", "--tags", "--refs", "origin")
    tags = {line.split()[1].removeprefix("refs/tags/") for line in remote_tags.splitlines()}
    stable_versions = []
    for tag in tags:
        try:
            stable_versions.append(version_number(tag))
        except ValueError:
            pass  # Ignore prerelease and non-version tags.
    latest = max(stable_versions, default=(0, 0, 0))
    version = args.version or f"v{latest[0]}.{latest[1]}.{latest[2] + 1}"
    if version_number(version) <= latest:
        raise ValueError(f"Version must be newer than v{'.'.join(map(str, latest))}.")
    if version in tags or run("git", "tag", "--list", version):
        raise ValueError(f"Tag already exists: {version}")

    print(f"Repository: {repo}\nCommit: {commit}\nVersion: {version}", flush=True)
    print(f"Images: ghcr.io/{repo}:{version} and ghcr.io/{repo}:latest", flush=True)
    if args.dry_run:
        if run("git", "status", "--porcelain"):
            print("Working tree has uncommitted changes; commit them before publishing.")
        print("Dry run: no tests, tags, or pushes performed.")
        return

    require_clean_tree()
    run("go", "test", "./...", show_output=True)
    if run("git", "rev-parse", "HEAD") != commit:
        raise ValueError("HEAD changed during tests; release aborted.")
    require_clean_tree()
    run("git", "tag", "-a", version, commit, "-m", f"Release {version}")
    try:
        run("git", "push", "origin", f"refs/tags/{version}", show_output=True)
    except subprocess.CalledProcessError:
        raise ValueError(f"Push failed; local tag retained. Retry: git push origin refs/tags/{version}") from None

    for _ in range(24):
        runs = json.loads(run(
            "gh", "run", "list", "--repo", repo, "--workflow", "release.yml",
            "--branch", version, "--commit", commit, "--event", "push",
            "--limit", "1", "--json", "databaseId",
        ))
        if runs:
            run_id = str(runs[0]["databaseId"])
            break
        time.sleep(5)
    else:
        raise ValueError(f"Tag pushed, but no Release run found yet. Check https://github.com/{repo}/actions")
    print(f"Actions: https://github.com/{repo}/actions/runs/{run_id}", flush=True)
    run("gh", "run", "watch", run_id, "--repo", repo, "--exit-status", "--interval", "30", show_output=True)
    print(f"Release complete: https://github.com/{repo}/releases/tag/{version}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.CalledProcessError) as error:
        detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) and error.stderr else str(error)
        print(f"Error: {detail}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("Stopped waiting. Any tag already pushed remains published.", file=sys.stderr)
        sys.exit(130)
