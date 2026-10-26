#!/bin/sh
# Downloads the prebuilt helm-cost binary that matches this plugin's version
# and the current OS/architecture, verifies its checksum, and installs it
# into ./bin. Run automatically by `helm plugin install` / `helm plugin update`.
set -e

PROJECT_GH="fadedreams/helm-cost"

# Local development escape hatch: HELM_COST_SKIP_DOWNLOAD=1 helm plugin install .
if [ -n "$HELM_COST_SKIP_DOWNLOAD" ]; then
  echo "HELM_COST_SKIP_DOWNLOAD set, skipping binary download."
  exit 0
fi

cd "${HELM_PLUGIN_DIR:-$(dirname "$0")}"

version="$(grep '^version:' plugin.yaml | cut -d '"' -f 2)"
if [ -z "$version" ]; then
  echo "Could not read version from plugin.yaml" >&2
  exit 1
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $os (use WSL on Windows)" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac

archive="helm-cost_${version}_${os}_${arch}.tar.gz"
base="https://github.com/${PROJECT_GH}/releases/download/v${version}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

download() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    echo "curl or wget is required" >&2
    exit 1
  fi
}

echo "Downloading helm-cost v${version} (${os}/${arch})..."
download "${base}/${archive}" "${tmp}/${archive}"
download "${base}/checksums.txt" "${tmp}/checksums.txt"

expected="$(grep " ${archive}\$" "${tmp}/checksums.txt" | cut -d ' ' -f 1)"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp}/${archive}" | cut -d ' ' -f 1)"
else
  actual="$(shasum -a 256 "${tmp}/${archive}" | cut -d ' ' -f 1)"
fi

if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "Checksum verification failed for ${archive}" >&2
  exit 1
fi

mkdir -p bin
tar -xzf "${tmp}/${archive}" -C "$tmp"
mv "${tmp}/helm-cost" bin/helm-cost
chmod +x bin/helm-cost
echo "Installed helm-cost v${version}."
