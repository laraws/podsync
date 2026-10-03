import contextlib
import io
import subprocess
import unittest
from unittest.mock import patch

import build_latest


class BuildLatestTests(unittest.TestCase):
    def invoke(self, arguments=(), dirty=False, failure=None, machine="aarch64"):
        output = io.StringIO()
        with patch("sys.argv", ["build_latest.py", *arguments]), \
             patch.object(build_latest.platform, "machine", return_value=machine), \
             patch.object(build_latest.subprocess, "check_output", side_effect=["abc123\n", " M main.go" if dirty else ""]), \
             patch.object(build_latest.subprocess, "run", side_effect=failure) as run, \
             contextlib.redirect_stdout(output):
            if failure:
                with self.assertRaises(subprocess.CalledProcessError):
                    build_latest.main()
            else:
                build_latest.main()
        return run, output.getvalue()

    def test_build_and_push(self):
        run, output = self.invoke()
        command = run.call_args.args[0]
        self.assertEqual(command[:3], ["docker", "buildx", "build"])
        self.assertEqual(command[command.index("--platform") + 1], "linux/arm64")
        self.assertEqual(command[command.index("--tag") + 1], build_latest.IMAGE)
        self.assertIn("--push", command)
        self.assertEqual(run.call_args.kwargs, {"cwd": build_latest.ROOT, "check": True})
        self.assertIn("发布成功", output)

    def test_host_architecture_default(self):
        for machine, expected in [("aarch64", "linux/arm64"), ("arm64", "linux/arm64"), ("x86_64", "linux/amd64"), ("AMD64", "linux/amd64")]:
            with self.subTest(machine=machine):
                run, _ = self.invoke(machine=machine)
                command = run.call_args.args[0]
                self.assertEqual(command[command.index("--platform") + 1], expected)

    def test_explicit_platforms(self):
        for platforms in ["linux/arm64,linux/amd64", "linux/amd64", "linux/arm64"]:
            with self.subTest(platforms=platforms):
                run, _ = self.invoke(["--platform", platforms], machine="unknown")
                command = run.call_args.args[0]
                self.assertEqual(command[command.index("--platform") + 1], platforms)

    def test_invalid_platforms_do_not_build(self):
        for platforms in ["", "linux/386", "linux/arm64,", "all"]:
            with self.subTest(platforms=platforms), contextlib.redirect_stderr(io.StringIO()), \
                 patch("sys.argv", ["build_latest.py", "--platform", platforms]), \
                 patch.object(build_latest.subprocess, "run") as run:
                with self.assertRaises(SystemExit) as error:
                    build_latest.main()
                self.assertEqual(error.exception.code, 2)
                run.assert_not_called()

    def test_unknown_host_requires_explicit_platform(self):
        with contextlib.redirect_stderr(io.StringIO()), \
             patch("sys.argv", ["build_latest.py"]), \
             patch.object(build_latest.platform, "machine", return_value="unknown"), \
             patch.object(build_latest.subprocess, "check_output") as git:
            with self.assertRaises(SystemExit) as error:
                build_latest.main()
            self.assertEqual(error.exception.code, 2)
            git.assert_not_called()

    def test_dry_run(self):
        run, output = self.invoke(["--dry-run"])
        run.assert_not_called()
        self.assertIn("docker buildx build", output)
        self.assertNotIn("发布成功", output)

    def test_dirty_revision(self):
        run, _ = self.invoke(dirty=True)
        self.assertIn("COMMIT=abc123-dirty", run.call_args.args[0])

    def test_failure_is_not_success(self):
        _, output = self.invoke(failure=subprocess.CalledProcessError(1, "docker"))
        self.assertNotIn("发布成功", output)


if __name__ == "__main__":
    unittest.main()
