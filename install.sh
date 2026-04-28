#!/usr/bin/env sh
# Yaad installer — curl -fsSL https://raw.githubusercontent.com/GrayCodeAI/yaad/main/install.sh | sh
set -e

REPO="GrayCodeAI/yaad"
BINARY="yaad"
INSTALL_DIR="${YAAD_INSTALL_DIR:-/usr/local/bin}"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release tag
echo "Fetching latest Yaad release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
if [ -z "$TAG" ]; then
  echo "Could not determine latest release. Install manually from:"
  echo "  https://github.com/${REPO}/releases"
  exit 1
fi

FILENAME="${BINARY}_${OS}_${ARCH}"
URL="https://github.com/${REPO}/releases/download/${TAG}/${FILENAME}"

echo "Installing yaad ${TAG} (${OS}/${ARCH})..."
curl -fsSL "$URL" -o "/tmp/${BINARY}"
chmod +x "/tmp/${BINARY}"

# Install (try with sudo if needed)
if [ -w "$INSTALL_DIR" ]; then
  mv "/tmp/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "/tmp/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo ""
echo "✓ yaad installed to ${INSTALL_DIR}/${BINARY}"
echo ""
echo "Get started:"
echo "  cd your-project"
echo "  yaad init"
echo "  yaad setup hawk    # or claude-code, cursor, gemini-cli, ..."
echo "  yaad mcp           # start MCP server"
echo ""
echo "Docs: https://github.com/${REPO}"
