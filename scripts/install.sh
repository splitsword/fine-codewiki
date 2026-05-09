#!/bin/sh
set -e

REPO="splitsword/fine-codewiki"
BINARY="codewiki"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux*) OS="linux" ;;
  darwin*) OS="darwin" ;;
  msys*|mingw*|cygwin*) OS="windows" ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  i386|i686) ARCH="386" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

if [ "$OS" = "windows" ]; then
  BINARY="${BINARY}.exe"
fi

# Determine install directory
if [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
  INSTALL_DIR="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ] && [ -w "$HOME/.local/bin" ]; then
  INSTALL_DIR="$HOME/.local/bin"
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

echo "Installing ${BINARY} for ${OS}/${ARCH}..."

# Fetch latest release tag from GitHub API
LATEST=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST" ] || [ "$LATEST" = "null" ]; then
  echo "Failed to fetch latest release. Falling back to go install..."
  go install "github.com/${REPO}/cmd/codewiki@latest"
  echo "Installed via go install."
  exit 0
fi

echo "Latest release: ${LATEST}"

# Download asset
if [ "$OS" = "windows" ]; then
  ASSET_NAME="${BINARY}-${LATEST}-${OS}-${ARCH}.zip"
else
  ASSET_NAME="${BINARY}-${LATEST}-${OS}-${ARCH}.tar.gz"
fi
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET_NAME}"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading ${ASSET_NAME}..."
if ! curl -sL -o "$TMP_DIR/${ASSET_NAME}" "$DOWNLOAD_URL"; then
  echo "Download failed. Falling back to go install..."
  go install "github.com/${REPO}/cmd/codewiki@latest"
  echo "Installed via go install."
  exit 0
fi

# Extract
echo "Extracting..."
if [ "$OS" = "windows" ]; then
  unzip -q "$TMP_DIR/${ASSET_NAME}" -d "$TMP_DIR"
else
  tar -xzf "$TMP_DIR/${ASSET_NAME}" -C "$TMP_DIR"
fi

# Install
mv "$TMP_DIR/${BINARY}" "$INSTALL_DIR/${BINARY}"
chmod +x "$INSTALL_DIR/${BINARY}"

echo "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"

# Check if install dir is in PATH
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Warning: ${INSTALL_DIR} is not in your PATH. Add it to your shell profile." ;;
esac
