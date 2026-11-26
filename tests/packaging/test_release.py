"""Release validation must reject incomplete notes before any publication."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('preflight', ROOT / 'build/release/preflight.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)

class ReleaseTest(unittest.TestCase):
    def notes(self, root, version='1.2.3', body='- Ready'):
        (root / 'govmc.go').write_text(f'package govmc\nvar Version = "{version}"\n')
        for name in ('CHANGELOG.md', 'CHANGELOG.zh.md'):
            (root / name).write_text(f'## [Unreleased]\n\n## [{version}]\n\n{body}\n')

    def test_tag_and_bilingual_notes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.notes(root)
            version, notes = release.prepare(root, 'v1.2.3', 'owner/repo')
            self.assertEqual(version, '1.2.3')
            self.assertIn('/blob/v1.2.3/CHANGELOG.zh.md', notes)
            with self.assertRaises(ValueError):
                release.prepare(root, 'v1.2.4', 'owner/repo')
            for tag in ('1.2.3', 'v1.2', 'v1.2.3-dev', 'v01.2.3'):
                with self.assertRaises(ValueError):
                    release.prepare(root, tag, 'owner/repo')
            (root / 'CHANGELOG.zh.md').write_text('## [1.2.2]\n- Old\n')
            with self.assertRaises(ValueError):
                release.prepare(root, 'v1.2.3', 'owner/repo')
            self.notes(root, body='TODO')
            with self.assertRaises(ValueError):
                release.prepare(root, 'v1.2.3', 'owner/repo')
            self.notes(root, version='1.2.3rc1')
            self.assertEqual(release.prepare(root, 'v1.2.3rc1', 'owner/repo')[0], '1.2.3rc1')

    def test_pypi_duplicate_and_network_errors_fail_closed(self):
        with patch.object(release, 'urlopen'):
            with self.assertRaises(ValueError):
                release.ensure_available('1.2.3')
        with patch.object(release, 'urlopen', side_effect=HTTPError('url', 404, '', None, None)) as request:
            release.ensure_available('1.2.3')
            request.assert_called_once_with('https://pypi.org/pypi/govmc-cli/1.2.3/json', timeout=30)
        for error in (HTTPError('url', 500, '', None, None), URLError('offline')):
            with patch.object(release, 'urlopen', side_effect=error):
                with self.assertRaises(type(error)):
                    release.ensure_available('1.2.3')
