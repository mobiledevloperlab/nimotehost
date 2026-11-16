"""Verify that the release gate rejects newer and missing glibc requirements."""
import os
import pathlib
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / 'scripts/check-linux-glibc.py'


class GlibcGateTests(unittest.TestCase):
    def check_output(self, output):
        with tempfile.TemporaryDirectory() as directory:
            readelf = pathlib.Path(directory) / 'readelf'
            readelf.write_text('#!/bin/sh\nprintf "%s\\n" "$FAKE_READELF"\n')
            readelf.chmod(0o755)
            env = {**os.environ, 'PATH': directory + os.pathsep + os.environ['PATH'],
                   'FAKE_READELF': output}
            return subprocess.run(['python3', str(SCRIPT), 'fixture', '2.35'],
                                  env=env, capture_output=True, text=True)

    def test_baseline_and_newer_symbols(self):
        self.assertEqual(self.check_output('Name: GLIBC_2.35\nName: GLIBC_2.17').returncode, 0)
        self.assertNotEqual(self.check_output('Name: GLIBC_2.36').returncode, 0)
        self.assertNotEqual(self.check_output('no version information').returncode, 0)


if __name__ == '__main__':
    unittest.main()
