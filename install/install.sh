#!/usr/bin/env sh
set -eu

repo=guajun/hardware-reset-cli
version="${HWRESET_VERSION:-}"
install_dir="${HWRESET_INSTALL_DIR:-$HOME/.local/bin}"
base_url="${HWRESET_RELEASE_BASE_URL:-}"
require() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required command: $1" >&2; exit 1; }; }
require curl
require tar
require awk
require sed

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo 'Supported systems: Linux and macOS. Use install.ps1 on Windows.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo 'Supported architectures: amd64 and arm64.' >&2; exit 1 ;;
esac
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
fi
version="${version#v}"
case "$version" in
  ''|*[!0-9A-Za-z.-]*) echo 'Invalid release version.' >&2; exit 1 ;;
esac
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$'; then
  echo 'Expected a release version such as 0.1.0 or v0.1.0.' >&2; exit 1
fi
if [ -z "$base_url" ]; then base_url="https://github.com/$repo/releases/download/v$version"; fi
asset="hardware-reset-cli-$version-$os-$arch.tar.gz"
temporary="$(mktemp -d)"
cleanup() { rm -rf "$temporary"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
curl -fsSL "$base_url/$asset" -o "$temporary/$asset"
curl -fsSL "$base_url/SHA256SUMS" -o "$temporary/SHA256SUMS"
expected="$(awk -v name="$asset" '$2 == name { print $1 }' "$temporary/SHA256SUMS")"
if [ "${#expected}" -ne 64 ]; then echo 'Missing or invalid archive checksum.' >&2; exit 1; fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$temporary/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$temporary/$asset" | awk '{print $1}')"
else
  echo 'Missing SHA-256 tool: install sha256sum or shasum.' >&2; exit 1
fi
if [ "$actual" != "$expected" ]; then echo 'Archive checksum mismatch; nothing installed.' >&2; exit 1; fi
# Extract only the named executable; archive paths cannot escape the temporary directory.
tar -xzf "$temporary/$asset" -C "$temporary" hardware-reset-cli
if [ ! -f "$temporary/hardware-reset-cli" ] || [ -L "$temporary/hardware-reset-cli" ]; then
  echo 'Archive contains no regular hardware-reset-cli executable.' >&2; exit 1
fi
mkdir -p "$install_dir"
install -m 0755 "$temporary/hardware-reset-cli" "$install_dir/hardware-reset-cli"
printf 'Installed hardware-reset-cli %s to %s\n' "$version" "$install_dir/hardware-reset-cli"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to PATH, or invoke the installed executable by its full path.\n' "$install_dir" ;;
esac
