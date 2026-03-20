#!/bin/bash
set -e

VERSION="0.1.0"
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
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "  Detected: ${OS}/${ARCH}"

# For now, build from source (binary releases will come later)
if ! command -v go &> /dev/null; then
    echo "  Go is required to build Cyntr. Install from https://go.dev/dl/"
    exit 1
fi

echo "  Building from source..."
TMPDIR=$(mktemp -d)
cd "$TMPDIR"
git clone --depth 1 --branch v${VERSION} https://github.com/${REPO}.git cyntr 2>/dev/null
cd cyntr
go build -o cyntr ./cmd/cyntr

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
echo "  ✓ Cyntr v${VERSION} installed to ${INSTALL_DIR}/cyntr"
echo ""
echo "  Get started:"
echo "    cyntr init      # Setup wizard"
echo "    cyntr start     # Launch server"
echo "    cyntr doctor    # Check configuration"
echo ""
