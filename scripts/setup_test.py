#!/usr/bin/env python3
"""Setup downloader regressions that do not require network access."""
import hashlib
import importlib.util
from pathlib import Path
import ssl
import tempfile
import unittest
from unittest import mock
import urllib.error


SETUP = Path(__file__).resolve().with_name("setup.py")
SPEC = importlib.util.spec_from_file_location("chiedi_setup", SETUP)
setup = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(setup)


class SetupDownloaderTests(unittest.TestCase):
    def test_certificate_failure_uses_curl_fallback(self):
        payload = b"pinned asset"
        expected = hashlib.sha256(payload).hexdigest()

        def fake_run(command, check):
            output = Path(command[command.index("--output") + 1])
            output.write_bytes(payload)
            self.assertTrue(check)

        certificate_failure = urllib.error.URLError(ssl.SSLCertVerificationError(
            1, "CERTIFICATE_VERIFY_FAILED"
        ))
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "asset.bin"
            with mock.patch.object(setup.urllib.request, "urlopen", side_effect=certificate_failure), \
                    mock.patch.object(setup.shutil, "which", return_value="/usr/bin/curl"), \
                    mock.patch.object(setup.subprocess, "run", side_effect=fake_run) as run:
                self.assertEqual(setup.download("https://example.test/asset", target, expected), target)

            self.assertEqual(target.read_bytes(), payload)
            self.assertEqual(run.call_count, 1)
            self.assertIn("curl", run.call_args.args[0][0])


if __name__ == "__main__":
    unittest.main()
