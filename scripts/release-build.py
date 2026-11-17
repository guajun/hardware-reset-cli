"""Build all supported Go release binaries and checksum-verified distribution archives."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="Release version, with or without leading v")
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / "release")
    parser.add_argument("--go", default="go")
    parser.add_argument("--firmware-dir", type=Path)
    options = parser.parse_args()
    version = options.version.removeprefix("v")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?", version):
        parser.error("version must be a semantic version such as v0.1.0")
    output = options.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    docs = [(ROOT / "README.md", "README.md")]
    docs += [(p, p.relative_to(ROOT).as_posix()) for p in sorted((ROOT / "docs").glob("*.md"))]
    docs += [(p, p.relative_to(ROOT).as_posix()) for p in sorted((ROOT / "third_party" / "licenses").glob("*.txt"))]
    for name in ("LICENSE", "THIRD_PARTY_NOTICES.md"):
        if (ROOT / name).is_file():
            docs.append((ROOT / name, name))
    artifacts: list[Path] = []
    for system in ("windows", "linux", "darwin"):
        for arch in ("amd64", "arm64"):
            executable = "hardware-reset-cli" + (".exe" if system == "windows" else "")
            binary = ROOT / "dist" / "release-build" / f"{system}-{arch}" / executable
            binary.parent.mkdir(parents=True, exist_ok=True)
            env = {**os.environ, "GOOS": system, "GOARCH": arch, "CGO_ENABLED": "0"}
            subprocess.run([options.go, "build", "-trimpath", "-ldflags", f"-s -w -X main.version={version}",
                            "-o", str(binary), "./cmd/hardware-reset-cli"], cwd=ROOT / "cli", env=env, check=True)
            files = [(binary, executable), *docs]
            stem = f"hardware-reset-cli-{version}-{system}-{arch}"
            if system == "windows":
                package = output / f"{stem}.zip"
                with zipfile.ZipFile(package, "w", zipfile.ZIP_DEFLATED) as archive:
                    for source, name in files:
                        archive.write(source, name)
            else:
                package = output / f"{stem}.tar.gz"
                with tarfile.open(package, "w:gz") as archive:
                    for source, name in files:
                        info = archive.gettarinfo(str(source), name)
                        info.mode = 0o755 if source == binary else 0o644
                        with source.open("rb") as content:
                            archive.addfile(info, content)
            artifacts.append(package)
            print(package, flush=True)
    if options.firmware_dir:
        firmware = options.firmware_dir.resolve()
        package = output / f"hardware-reset-firmware-{version}.zip"
        files = [(firmware / f"hardware-reset-cli.{ext}", f"hardware-reset-cli.{ext}") for ext in ("elf", "bin", "hex", "map")]
        for base in (ROOT / "firmware" / "Drivers", ROOT / "firmware" / "Middlewares"):
            for source in sorted(base.rglob("*")):
                if source.is_file() and source.name.lower().startswith(("license", "copying", "notice")):
                    files.append((source, source.relative_to(ROOT).as_posix()))
        with zipfile.ZipFile(package, "w", zipfile.ZIP_DEFLATED) as archive:
            for source, name in [*files, *docs]:
                archive.write(source, name)
        artifacts.append(package)
    for name in ("install.ps1", "install.sh"):
        target = output / name
        shutil.copyfile(ROOT / "install" / name, target)
        artifacts.append(target)
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    metadata = output / "build-info.json"
    metadata.write_text(json.dumps({"version": version, "revision": revision, "cgo_enabled": False,
                                    "artifacts": [path.name for path in artifacts]}, indent=2) + "\n", encoding="utf-8")
    artifacts.append(metadata)
    (output / "SHA256SUMS").write_text("".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in artifacts), encoding="ascii")


if __name__ == "__main__":
    main()
