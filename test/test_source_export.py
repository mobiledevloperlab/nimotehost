"""Regression coverage for the public-source archive boundary."""
import pathlib
import shutil
import subprocess
import tarfile
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / 'scripts/export-source.sh'

class HostExportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        (self.root / 'scripts').mkdir()
        shutil.copyfile(SCRIPT, self.root / 'scripts/export-source.sh')
        self.host = self.root
        for name in ['README.zh-CN.md', 'CONTRIBUTING.zh-CN.md', 'SECURITY.zh-CN.md',
                     'CHANGELOG.zh-CN.md', 'THIRD_PARTY_NOTICES.zh-CN.md',
                     'README.md', 'LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md',
                     'CONTRIBUTING.md', 'SECURITY.md', 'CHANGELOG.md', 'Makefile',
                     '.gitignore', 'go.mod', 'go.sum']:
            (self.host / name).write_text('fixture\n')
        for name in ['.github', 'assets', 'cmd', 'internal', 'scripts', 'docs', 'tools', 'third_party', 'test', 'pkg', 'examples', 'plugin-catalog']:
            (self.host / name).mkdir(exist_ok=True)
            (self.host / name / 'fixture.txt').write_text('public\n')
        self.archive = self.root / 'dist/public/nimotehost-source.tar.gz'

    def export(self):
        return subprocess.run(['bash', str(self.root / 'scripts/export-source.sh')],
                              capture_output=True, text=True)

    def test_archive_isolated_and_deterministic(self):
        (self.root / 'private.txt').write_text('private')
        (self.host / 'dist').mkdir()
        (self.host / 'dist/local-binary').write_text('private')
        (self.host / 'test/__pycache__').mkdir()
        (self.host / 'test/__pycache__/local.pyc').write_bytes(b'local bytecode')
        self.assertEqual(self.export().returncode, 0)
        first = self.archive.read_bytes()
        self.assertEqual(self.export().returncode, 0)
        self.assertEqual(first, self.archive.read_bytes())
        with tarfile.open(self.archive) as archive:
            names = archive.getnames()
            self.assertIn('LICENSE', names)
            self.assertFalse(any('__pycache__' in n or n.endswith('.pyc') for n in names))
            self.assertIn('README.zh-CN.md', names)
            self.assertIn('internal/fixture.txt', names)
            for directory in ('pkg', 'examples', 'plugin-catalog'):
                self.assertIn(directory + '/fixture.txt', names)
            self.assertFalse(any('private' in n or n.startswith(('.git/', 'dist/', '../', '/')) for n in names))
            self.assertTrue(all(m.isfile() for m in archive.getmembers()))

    def test_checkout_excludes_untracked_notes(self):
        subprocess.run(['git', 'init', '-q', str(self.host)], check=True)
        subprocess.run(['git', '-C', str(self.host), 'add', '.'], check=True)
        (self.host / 'docs/private.md').write_text('private')
        result = self.export()
        self.assertEqual(result.returncode, 0, result.stderr)
        with tarfile.open(self.archive) as archive:
            self.assertIn('docs/fixture.txt', archive.getnames())
            self.assertNotIn('docs/private.md', archive.getnames())

    def test_invalid_checkout_fails_closed(self):
        (self.host / '.git').write_text('gitdir: missing\n')
        self.assertNotEqual(self.export().returncode, 0)
        self.assertFalse(self.archive.exists())

    def test_symlink_rejected(self):
        (self.host / 'internal/link').symlink_to(self.root / 'scripts')
        self.assertNotEqual(self.export().returncode, 0)
        self.assertFalse(self.archive.exists())

    def test_credentials_rejected_without_echoing_values(self):
        fake_keys = [
            'AKIA' + 'A' * 16,
            'sk-' + 'A' * 40,
            'sk-' + 'proj-' + 'A' * 40,
            'sk-' + 'ant-' + 'A' * 40,
            '-----BEGIN ' + 'ENCRYPTED PRIVATE KEY-----',
        ]
        for fake_key in fake_keys:
            with self.subTest(prefix=fake_key[:8]):
                (self.host / 'docs/secret.txt').write_text(fake_key)
                result = self.export()
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn(fake_key, result.stdout + result.stderr)
                self.assertFalse(self.archive.exists())

    def test_environment_file_rejected(self):
        (self.host / 'docs/.env').write_text('VALUE=fixture')
        self.assertNotEqual(self.export().returncode, 0)

if __name__ == '__main__':
    unittest.main()
