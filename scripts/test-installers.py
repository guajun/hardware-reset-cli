"""Exercise installers against local fixture assets; no GitHub downloads or PATH changes."""
from __future__ import annotations

from contextlib import contextmanager
from functools import partial
import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import threading
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]
CONTENT = b"fixture executable; never executed\n"


class QuietHandler(SimpleHTTPRequestHandler):
    def log_message(self, *_args):
        pass


@contextmanager
def fixture():
    with tempfile.TemporaryDirectory(prefix="hwreset-install-test-") as name:
        root = Path(name)
        assets = root / "assets"
        assets.mkdir()
        windows = assets / "hardware-reset-cli-0.1.0-windows-amd64.zip"
        with zipfile.ZipFile(windows, "w") as archive:
            archive.writestr("hardware-reset-cli.exe", CONTENT)
        linux = assets / "hardware-reset-cli-0.1.0-linux-amd64.tar.gz"
        with tarfile.open(linux, "w:gz") as archive:
            info = tarfile.TarInfo("hardware-reset-cli")
            info.size, info.mode = len(CONTENT), 0o755
            archive.addfile(info, io.BytesIO(CONTENT))
        (assets / "SHA256SUMS").write_text("".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in (windows, linux)), encoding="ascii")
        server = ThreadingHTTPServer(("127.0.0.1", 0), partial(QuietHandler, directory=str(assets)))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            yield root, f"http://127.0.0.1:{server.server_port}"
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


class Installers(unittest.TestCase):
    def test_powershell_installs_and_rejects_corruption(self):
        executable = shutil.which("powershell") or shutil.which("pwsh")
        if not executable or os.name != "nt":
            self.skipTest("Windows PowerShell installer requires Windows")
        with fixture() as (root, base):
            target = root / "installed"
            env = {**os.environ, "PROCESSOR_ARCHITECTURE": "AMD64", "PROCESSOR_ARCHITEW6432": "AMD64"}
            command = [executable, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(ROOT / "install/install.ps1"),
                       "-Version", "v0.1.0", "-InstallDir", str(target), "-ReleaseBaseUrl", base, "-NoPathUpdate"]
            for _ in range(2):
                result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            binary = target / "hardware-reset-cli.exe"
            self.assertEqual(binary.read_bytes(), CONTENT)
            (root / "assets/SHA256SUMS").write_text("0" * 64 + "  hardware-reset-cli-0.1.0-windows-amd64.zip\n", encoding="ascii")
            failed = subprocess.run(command, env=env, capture_output=True, text=True, timeout=30)
            self.assertNotEqual(failed.returncode, 0)
            self.assertIn("checksum mismatch", failed.stdout + failed.stderr)
            self.assertEqual(binary.read_bytes(), CONTENT)

    def test_shell_installs_and_rejects_corruption(self):
        executable = shutil.which("bash")
        if os.name == "nt":
            git_bash = Path(r"C:\Program Files\Git\bin\bash.exe")
            executable = str(git_bash) if git_bash.is_file() else executable
        if not executable:
            self.skipTest("bash not available")
        with fixture() as (root, base):
            tools = root / "tools"
            tools.mkdir()
            uname = tools / "uname"
            uname.write_text('#!/bin/sh\ncase "$1" in -s) echo Linux;; -m) echo x86_64;; esac\n', encoding="ascii")
            uname.chmod(0o755)
            target = root / "installed"
            env = {**os.environ, "HWRESET_VERSION": "v0.1.0", "HWRESET_INSTALL_DIR": target.as_posix(),
                   "HWRESET_RELEASE_BASE_URL": base, "HWRESET_TEST_TOOLS": tools.as_posix(),
                   "HWRESET_TEST_SCRIPT": (ROOT / "install/install.sh").as_posix()}
            command = [executable, "-c", 'PATH="$HWRESET_TEST_TOOLS:$PATH" sh "$HWRESET_TEST_SCRIPT"']
            if os.name == "nt":
                command = [executable, "-c", 'test_tools=$(cygpath -u "$HWRESET_TEST_TOOLS"); PATH="$test_tools:$PATH" sh "$HWRESET_TEST_SCRIPT"']
            for _ in range(2):
                result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            binary = target / "hardware-reset-cli"
            self.assertEqual(binary.read_bytes(), CONTENT)
            (root / "assets/SHA256SUMS").write_text("0" * 64 + "  hardware-reset-cli-0.1.0-linux-amd64.tar.gz\n", encoding="ascii")
            failed = subprocess.run(command, env=env, capture_output=True, text=True, timeout=30)
            self.assertNotEqual(failed.returncode, 0)
            self.assertIn("checksum mismatch", failed.stdout + failed.stderr)
            self.assertEqual(binary.read_bytes(), CONTENT)


if __name__ == "__main__":
    unittest.main()
