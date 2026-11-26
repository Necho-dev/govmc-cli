"""Build an installable Python wheel for the current host using the Go toolchain."""

import argparse
import base64
import csv
from dataclasses import dataclass
import hashlib
import io
import os
from pathlib import Path
import re
from urllib.parse import urlsplit
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
from native import build_binary, native_target, valid_version

TARGETS = {
    "darwin-arm64": "macosx_15_0_arm64",
    "darwin-amd64": "macosx_15_0_x86_64",
    "linux-arm64": "manylinux2014_aarch64",
    "linux-amd64": "manylinux2014_x86_64",
    "windows-amd64": "win_amd64",
}



@dataclass(frozen=True)
class Member:
    data: bytes
    mode: int = 0o644


def write_archive(path: Path, files: dict[str, Member]) -> None:
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for name, member in files.items():
            entry = zipfile.ZipInfo(name)
            entry.create_system = 3
            entry.external_attr = (0o100000 | member.mode) << 16
            archive.writestr(entry, member.data, compress_type=zipfile.ZIP_DEFLATED)


def readme_description(version: str) -> str:
    """Use the project README, with release-pinned links that work on PyPI."""
    repository = "https://github.com/Necho-dev/govmc-cli"

    def absolute(target: str, image: bool = False) -> str:
        if not target or target.startswith(("#", "/")) or urlsplit(target).scheme:
            return target
        path = target.removeprefix("./")
        if image:
            return f"https://raw.githubusercontent.com/Necho-dev/govmc-cli/v{version}/{path}"
        return f"{repository}/blob/v{version}/{path}"

    text = (ROOT / "README.md").read_text(encoding="utf-8")
    text = re.sub(r'(!?\[[^\]\n]*\]\()([^\s)]+)(\))',
                  lambda m: m[1] + absolute(m[2], m[1].startswith("!")) + m[3], text)
    return re.sub(r'(\b(src|href)=["\'])([^"\']+)(["\'])',
                  lambda m: m[1] + absolute(m[3], m[2] == "src") + m[4], text)


def write_wheel(out: Path, name: str, version: str, tag: str, files: dict[str, Member],
                dependencies: str = "", entry_points: str = "") -> None:
    stem = f"{name.replace('-', '_')}-{version}"
    dist = f"{stem}.dist-info"
    description = ("Go CLI, TUI, MCP and local VMware management workbench"
                   if name == "govmc-cli" else "Optional WebMKS resources for govmc")
    if name == "govmc-cli":
        long_description = readme_description(version)
    else:
        long_description = (
            "# GoVMC Web resources\n\nOptional WebMKS resources for `govmc-cli[web]`. "
            "Install only from a channel with the required SDK redistribution authorization.\n"
        )
    files[f"{dist}/METADATA"] = Member((
        f"Metadata-Version: 2.3\nName: {name}\nVersion: {version}\n"
        f"Summary: {description}\nRequires-Python: >=3.9\n"
        "License: MIT\n"
        "Classifier: License :: OSI Approved :: MIT License\n"
        "Description-Content-Type: text/markdown\n"
        f"Project-URL: Source, https://github.com/Necho-dev/govmc-cli\n"
        f"{dependencies}\n{long_description}"
    ).encode())
    files[f"{dist}/LICENSE"] = Member((ROOT / "LICENSE").read_bytes())
    files[f"{dist}/WHEEL"] = Member((
        "Wheel-Version: 1.0\nGenerator: govmc-release\n"
        f"Root-Is-Purelib: {'true' if tag == 'py3-none-any' else 'false'}\nTag: {tag}\n"
    ).encode())
    if entry_points:
        files[f"{dist}/entry_points.txt"] = Member(entry_points.encode())
    record = io.StringIO(newline="")
    rows = csv.writer(record)
    for filename, member in files.items():
        digest = base64.urlsafe_b64encode(hashlib.sha256(member.data).digest()).decode().rstrip("=")
        rows.writerow((filename, "sha256=" + digest, len(member.data)))
    rows.writerow((f"{dist}/RECORD", "", ""))
    files[f"{dist}/RECORD"] = Member(record.getvalue().encode())
    write_archive(out / f"{stem}-{tag}.whl", files)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="release version (X.Y.Z or X.Y.ZrcN)")
    parser.add_argument("--target", choices=TARGETS, help="override target; defaults to the current Go host")
    parser.add_argument("--binary", type=Path, help="package an existing native executable instead of rebuilding")
    parser.add_argument("--out", type=Path, default=ROOT / "dist")
    parser.add_argument("--authorized-web", action="store_true",
                        help="build SDK-containing artifacts for an authorized distribution channel")
    options = parser.parse_args()
    if not valid_version(options.version):
        parser.error("version must be X.Y.Z or X.Y.ZrcN")
    if options.authorized_web:
        parser.error("--authorized-web was removed: use govmc wmks install --file SDK.zip")
    if not options.target:
        options.target = native_target()
    options.out.mkdir(parents=True, exist_ok=True)
    if options.target:
        binary = "govmc.exe" if options.target.startswith("windows-") else "govmc"
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / binary
            executable = Member(options.binary.read_bytes() if options.binary else build_binary(output, options.target, options.version), 0o755)
            write_wheel(options.out, "govmc-cli", options.version, "py3-none-" + TARGETS[options.target], {
                "govmc_cli/__init__.py": Member(b""),
                "govmc_cli/launcher.py": Member((ROOT / "build/python/launcher.py").read_bytes()),
                f"govmc_cli/{binary}": executable,
            },
                entry_points="[console_scripts]\ngovmc = govmc_cli.launcher:main\n")



if __name__ == "__main__":
    main()
