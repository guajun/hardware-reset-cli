"""Package already-built native CLI/firmware artifacts; never builds or flashes hardware."""

from __future__ import annotations

import argparse
import hashlib
import json
import platform
import re
import subprocess
import sys
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def archive(path: Path, files: list[tuple[Path, str]]) -> None:
    for source, _ in files:
        if not source.is_file():
            raise FileNotFoundError(f"Build artifact not found: {source}")
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as output:
        for source, name in files:
            output.write(source, name)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="0.1.0")
    parser.add_argument("--configuration", choices=("Debug", "Release"), default="Release")
    parser.add_argument("--kind", choices=("all", "cli", "firmware"), default="all")
    parser.add_argument("--cli-binary", type=Path)
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / "packages")
    options = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", options.version):
        parser.error("version may contain only ASCII letters, digits, dot, underscore and hyphen")
    options.output.mkdir(parents=True, exist_ok=True)
    docs = [(ROOT / "README.md", "README.md")]
    docs += [(p, f"docs/{p.name}") for p in sorted((ROOT / "docs").glob("*.md"))]
    docs += [(p, p.relative_to(ROOT).as_posix()) for p in sorted((ROOT / "third_party" / "licenses").glob("*.txt"))]
    for name in ("LICENSE", "THIRD_PARTY_NOTICES.md"):
        if (ROOT / name).is_file():
            docs.append((ROOT / name, name))
    names: list[Path] = []
    if options.kind in ("all", "cli"):
        system = {"win32": "windows", "darwin": "darwin", "linux": "linux"}.get(sys.platform)
        if system is None:
            parser.error(f"unsupported native platform: {sys.platform}")
        machine = platform.machine().lower()
        arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(machine)
        if arch is None:
            parser.error(f"unsupported native architecture: {machine}")
        executable = "hardware-reset-cli" + (".exe" if system == "windows" else "")
        binary = options.cli_binary or ROOT / "dist" / executable
        package = options.output / f"hardware-reset-cli-{options.version}-{system}-{arch}.zip"
        archive(package, [(binary, executable), *docs])
        names.append(package)
    if options.kind in ("all", "firmware"):
        build = ROOT / "firmware" / "build" / options.configuration
        files = [(build / f"hardware-reset-cli.{ext}", f"hardware-reset-cli.{ext}") for ext in ("elf", "bin", "hex", "map")]
        # Include the original bundled vendor notices with firmware redistributions.
        for base in (ROOT / "firmware" / "Drivers", ROOT / "firmware" / "Middlewares"):
            for source in sorted(base.rglob("*")):
                if source.is_file() and source.name.lower().startswith(("license", "copying", "notice")):
                    files.append((source, source.relative_to(ROOT).as_posix()))
        package = options.output / f"hardware-reset-firmware-{options.version}-{options.configuration.lower()}.zip"
        archive(package, [*files, *docs])
        names.append(package)
    checksums = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in names)
    (options.output / "SHA256SUMS").write_text(checksums, encoding="ascii")
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=False)
    metadata = {"version": options.version, "revision": revision.stdout.strip() if revision.returncode == 0 else None,
                "configuration": options.configuration, "artifacts": [path.name for path in names]}
    (options.output / "build-info.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    for path in names:
        print(path)


if __name__ == "__main__":
    try:
        main()
    except (FileNotFoundError, OSError) as error:
        sys.exit(str(error))
