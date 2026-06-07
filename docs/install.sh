#!/bin/sh
# Cyntr installer — downloads the prebuilt single binary (no Go required).
#
#   curl -fsSL https://cyntr.dev/install.sh | sh
#
# Options (environment variables):
#   INSTALL_DIR=$HOME/.local/bin   where to install (default: /usr/local/bin)
#   CYNTR_VERSION=v1.3.0           pin a version (default: latest release)
#
# On Windows use PowerShell:
#   iwr -useb https://raw.githubusercontent.com/surya-koritala/cyntr/main/scripts/install.ps1 | iex
set -eu

REPO="surya-koritala/cyntr"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

fail() { echo "  ✗ $1" >&2; exit 1; }

# --- detect platform ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "unsupported architecture: $ARCH" ;;
esac
case "$OS" in
    linux|darwin) ;;
    *) fail "unsupported OS: $OS — on Windows use scripts/install.ps1" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"

# --- resolve version (pinned or latest release) ---
VERSION="${CYNTR_VERSION:-}"
if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -1)
fi
[ -n "$VERSION" ] || fail "could not determine the latest version (set CYNTR_VERSION to override)"

ASSET="cyntr_${OS}_${ARCH}"
BASE="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

echo ""
echo "  Installing Cyntr ${VERSION} (${OS}/${ARCH})"
echo ""

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# --- download binary (errors are surfaced, not hidden) ---
if ! curl -fSL --progress-bar "$BASE" -o "$TMP/cyntr"; then
    echo "" >&2
    echo "  No prebuilt binary for ${OS}/${ARCH} at ${VERSION}." >&2
    echo "  Build from source instead (requires Go 1.26+):" >&2
    echo "    git clone https://github.com/${REPO}.git && cd cyntr && go build -o cyntr ./cmd/cyntr" >&2
    exit 1
fi

# --- verify checksum (best effort: skip if the .sha256 sidecar is absent) ---
if curl -fsSL "${BASE}.sha256" -o "$TMP/cyntr.sha256" 2>/dev/null; then
    EXPECTED=$(cut -d' ' -f1 "$TMP/cyntr.sha256")
    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL=$(sha256sum "$TMP/cyntr" | cut -d' ' -f1)
    else
        ACTUAL=$(shasum -a 256 "$TMP/cyntr" | cut -d' ' -f1)
    fi
    [ "$EXPECTED" = "$ACTUAL" ] || fail "checksum verification failed (expected $EXPECTED, got $ACTUAL)"
    echo "  ✓ checksum verified"
fi

chmod +x "$TMP/cyntr"

# --- install ---
if [ -d "$INSTALL_DIR" ] || mkdir -p "$INSTALL_DIR" 2>/dev/null; then :; else
    fail "cannot create ${INSTALL_DIR} (set INSTALL_DIR to a writable path)"
fi
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP/cyntr" "$INSTALL_DIR/cyntr"
else
    echo "  Need elevated permission to write ${INSTALL_DIR}"
    sudo mv "$TMP/cyntr" "$INSTALL_DIR/cyntr"
fi

echo ""
echo "  ✓ installed to ${INSTALL_DIR}/cyntr"
"${INSTALL_DIR}/cyntr" version 2>/dev/null || true
echo ""
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "  Note: ${INSTALL_DIR} is not on your PATH — add it to use 'cyntr' directly." ;;
esac
echo "  Next:  cyntr init     # 5-step wizard: provider, channel, security, policy, agent"
echo "         cyntr start    # gateway + dashboard at http://localhost:7700"
echo ""
