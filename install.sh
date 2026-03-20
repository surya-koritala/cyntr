#!/bin/bash
set -e

VERSION="0.3.0"
REPO="surya-koritala/cyntr"

echo ""
echo "  ╔═══════════════════════════════════════╗"
echo "  ║    Installing Cyntr v${VERSION}              ║"
echo "  ╚═══════════════════════════════════════╝"
echo ""

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "  Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "  Detected: ${OS}/${ARCH}"

# Check for Go
if ! command -v go &> /dev/null; then
    echo ""
    echo "  Go is required to build Cyntr."
    echo "  Install from: https://go.dev/dl/"
    echo ""
    exit 1
fi

echo "  Building from source..."
TMPDIR=$(mktemp -d)
cd "$TMPDIR"
git clone --depth 1 --branch v${VERSION} https://github.com/${REPO}.git cyntr 2>/dev/null
cd cyntr
go build -o cyntr ./cmd/cyntr 2>/dev/null

# Install
INSTALL_DIR="/usr/local/bin"
if [ -w "$INSTALL_DIR" ]; then
    cp cyntr "$INSTALL_DIR/cyntr"
else
    echo "  Need sudo to install to ${INSTALL_DIR}"
    sudo cp cyntr "$INSTALL_DIR/cyntr"
fi

# Cleanup
cd /
rm -rf "$TMPDIR"

echo ""
echo "  ✓ Cyntr v${VERSION} installed"
echo ""

# Auto-run setup wizard
echo "  Starting setup wizard..."
echo ""
cyntr init
