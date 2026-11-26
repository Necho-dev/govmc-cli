"""Check the actual wheel metadata without requiring a Go compilation."""
from email import policy
from email.parser import BytesParser
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[2]


class MetadataTest(unittest.TestCase):
    def test_wheel_includes_full_readme_and_public_links(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            binary = work / "govmc"
            binary.write_bytes(b"metadata-only fixture")
            subprocess.run([
                sys.executable, str(ROOT / "build/wheel.py"),
                "--version", "0.1.1", "--target", "linux-amd64",
                "--binary", str(binary), "--out", str(work),
            ], check=True, cwd=work)
            with zipfile.ZipFile(next(work.glob("*.whl"))) as wheel:
                metadata = BytesParser(policy=policy.default).parsebytes(
                    wheel.read(next(n for n in wheel.namelist() if n.endswith("/METADATA"))))

            self.assertEqual(metadata["Description-Content-Type"], "text/markdown")
            description = metadata.get_payload(decode=True).decode("utf-8")
            readme = (ROOT / "README.md").read_text(encoding="utf-8")
            # Every README heading and fenced example must survive packaging.
            for line in readme.splitlines():
                if line.startswith(("#", "```")):
                    self.assertIn(line, description)
            self.assertIn("pipx install govmc-cli", description)
            self.assertIn("govmc wmks install", description)
            self.assertIn("https://raw.githubusercontent.com/Necho-dev/govmc-cli/v0.1.1/assets/demos/cli.gif", description)
            self.assertIn("https://raw.githubusercontent.com/Necho-dev/govmc-cli/v0.1.1/assets/screenshots/web/console.png", description)
            self.assertIn("https://github.com/Necho-dev/govmc-cli/blob/v0.1.1/README.zh.md", description)
            self.assertIn("https://github.com/Necho-dev/govmc-cli/blob/v0.1.1/LICENSE", description)
            self.assertIn("https://github.com/Necho-dev/govmc-cli/blob/v0.1.1/skills/esxi-govc/SKILL.md", description)
            self.assertIn("https://go.dev/", description)
            self.assertNotIn('src="assets/', description)
            self.assertNotIn("](skills/", description)


if __name__ == "__main__":
    unittest.main()
