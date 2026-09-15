#!/usr/bin/env python3
"""Installer regressions using a real executable; no model download required."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

INSTALLER = Path(__file__).resolve().with_name("install.sh")


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        (self.source / "assets").mkdir(parents=True)
        shutil.copy(shutil.which("sleep"), self.source / "chiedi")
        (self.source / "assets/manifest.json").write_text('{"version": 1}')
        self.prefix = self.root / "installed"

    def install(self, prefix=None, path=None):
        env = dict(os.environ, PREFIX=str(prefix or self.prefix))
        if path:
            env["PATH"] = str(path) + os.pathsep + env["PATH"]
        return subprocess.run(["bash", str(INSTALLER), str(self.source)],
                              cwd=self.root, env=env, capture_output=True, text=True)

    def test_relative_prefix_with_spaces(self):
        result = self.install("relative install")
        self.assertEqual(result.returncode, 0, result.stderr)
        binary = self.root / "relative install/bin/chiedi"
        self.assertTrue(binary.is_file())
        self.assertTrue(Path(os.readlink(binary)).is_absolute())
        subprocess.run([str(binary), "0"], check=True)

    def check_running_upgrade(self, legacy):
        if legacy:
            release = self.prefix / "lib/chiedi"
            shutil.copytree(self.source, release)
            (self.prefix / "bin").mkdir()
            (self.prefix / "bin/chiedi").symlink_to(release / "chiedi")
        else:
            result = self.install()
            self.assertEqual(result.returncode, 0, result.stderr)
        binary = self.prefix / "bin/chiedi"
        old = binary.resolve()
        process = subprocess.Popen([str(binary), "60"])
        try:
            (self.source / "assets/manifest.json").write_text('{"version": 2}')
            result = self.install()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIsNone(process.poll(), "upgrade terminated the active process")
            self.assertNotEqual(binary.resolve(), old)
            self.assertEqual((old.parent / "assets/manifest.json").read_text(), '{"version": 1}')
            self.assertEqual((binary.resolve().parent / "assets/manifest.json").read_text(), '{"version": 2}')
            subprocess.run([str(binary), "0"], check=True)
        finally:
            process.terminate()
            process.wait()

    def test_upgrade_running_release(self):
        self.check_running_upgrade(False)

    def test_upgrade_running_legacy_installation(self):
        self.check_running_upgrade(True)

    def test_failed_asset_copy_preserves_installation(self):
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        binary = self.prefix / "bin/chiedi"
        old = binary.resolve()
        releases = set((self.prefix / "lib/chiedi/releases").iterdir())
        fake_bin = self.root / "fake-bin"
        fake_bin.mkdir()
        fake_cp = fake_bin / "cp"
        real_cp = shutil.which("cp")
        fake_cp.write_text('#!/bin/sh\nif [ "$1" = "-R" ]; then exit 19; fi\nexec "' + real_cp + '" "$@"\n')
        fake_cp.chmod(0o755)
        result = self.install(path=fake_bin)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(binary.resolve(), old)
        self.assertEqual(set((self.prefix / "lib/chiedi/releases").iterdir()), releases)
        subprocess.run([str(binary), "0"], check=True)


if __name__ == "__main__":
    unittest.main()
