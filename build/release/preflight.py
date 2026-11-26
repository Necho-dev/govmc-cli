"""Validate a release tag and matching bilingual notes before building or uploading."""
import argparse
from pathlib import Path
import re
import sys
from urllib.error import HTTPError
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'build'))
from native import valid_version


def prepare(root: Path, tag: str, repository: str) -> tuple[str, str]:
    version = tag.removeprefix('v')
    if tag != 'v' + version or not valid_version(version):
        raise ValueError('tag must be vX.Y.Z or vX.Y.ZrcN')
    source = (root / 'govmc.go').read_text()
    match = re.search(r'var Version = "([^"]+)"', source)
    if not match or match[1] != version:
        raise ValueError('tag must match Version in govmc.go')
    catalogs = [(root / name).read_text() for name in ('CHANGELOG.md', 'CHANGELOG.zh.md')]
    versions = [re.findall(r'^## \[([^]]+)\]', text, re.M) for text in catalogs]
    if versions[0] != versions[1]:
        raise ValueError('English and Chinese changelog version headings differ')
    notes = []
    for text in catalogs:
        match = re.search(r'^## \[' + re.escape(version) + r'\][^\n]*\n(.*?)(?=^## |\Z)', text, re.M | re.S)
        if not match or not re.search(r'^- \S', match[1], re.M):
            raise ValueError(f'complete bilingual release notes required for {version}')
        notes.append(match[1].strip())
    return version, notes[0] + f'\n\n[简体中文](https://github.com/{repository}/blob/{tag}/CHANGELOG.zh.md)\n'


def ensure_available(version: str) -> None:
    try:
        with urlopen(f'https://pypi.org/pypi/govmc-cli/{version}/json', timeout=30):
            pass
    except HTTPError as error:
        if error.code == 404:
            return
        raise
    raise ValueError(f'govmc-cli {version} already exists on PyPI')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tag', required=True)
    parser.add_argument('--repository', required=True)
    parser.add_argument('--notes', type=Path, required=True)
    parser.add_argument('--check-pypi', action='store_true')
    args = parser.parse_args()
    version, notes = prepare(ROOT, args.tag, args.repository)
    if args.check_pypi:
        ensure_available(version)
    args.notes.write_text(notes)
    print(version)

if __name__ == '__main__':
    main()
