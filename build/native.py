"""Build and archive the native GoVMC executable for the current host."""

import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/Necho-dev/govmc-cli"
TARGETS = ("darwin-arm64", "darwin-amd64", "linux-arm64", "linux-amd64", "windows-amd64")


def native_target() -> str:
    result = subprocess.run(["go", "env", "GOOS", "GOARCH"], cwd=ROOT,
                            capture_output=True, text=True, check=True)
    target = "-".join(result.stdout.split())
    if target not in TARGETS:
        raise ValueError(f"unsupported host target: {target}")
    return target


def valid_version(version: str) -> bool:
    return bool(re.fullmatch(r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:rc[1-9]\d*)?", version))


def build_binary(output: Path, target: str, version: str, embed_web: bool = False) -> bytes:
    goos, goarch = target.split("-")
    env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"}
    args = ["go", "build", "-trimpath", "-ldflags", f"-s -w -X {MODULE}.Version={version}"]
    if embed_web:
        raise ValueError("SDK embedding was removed; use govmc wmks install --file SDK.zip")
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([*args, "-o", str(output), "./cmd/govmc"], cwd=ROOT, env=env, check=True)
    return output.read_bytes()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--target", choices=TARGETS, help="defaults to the current Go host")
    parser.add_argument("--out", type=Path, default=ROOT / "dist")
    parser.add_argument("--authorized-web", action="store_true", help="embed SDK assets for an authorized channel")
    options = parser.parse_args()
    if not valid_version(options.version):
        parser.error("version must be X.Y.Z or X.Y.ZrcN")
    if options.authorized_web:
        parser.error("--authorized-web was removed: ordinary Web is included; use govmc wmks install for SDK assets")
    target = options.target or native_target()
    binary = "govmc.exe" if target.startswith("windows-") else "govmc"
    output = options.out.resolve()
    data = build_binary(output / binary, target, options.version, options.authorized_web)
    suffix = "-web" if options.authorized_web else ""
    archive = output / f"govmc-{options.version}-{target}{suffix}.zip"
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
        member = zipfile.ZipInfo(binary)
        member.create_system = 3
        member.external_attr = (0o100000 | 0o755) << 16
        bundle.writestr(member, data, compress_type=zipfile.ZIP_DEFLATED)
        bundle.writestr("LICENSE", (ROOT / "LICENSE").read_bytes())
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_suffix(archive.suffix + ".sha256").write_text(f"{digest}  {archive.name}\n")
    print(archive)


if __name__ == "__main__":
    main()
