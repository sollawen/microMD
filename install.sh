#!/bin/sh
# microNeo one-line installer
# Usage: curl -fsSL https://raw.githubusercontent.com/sollawen/microNeo/master/install.sh | sh

set -e

REPO="sollawen/microNeo"
INSTALL_DIR="/usr/local/bin"

# Detect OS
case "$(uname -s)" in
    Linux*)     OS="linux" ;;
    Darwin*)    OS="macos" ;;
    CYGWIN*|MINGW*|MSYS*) OS="windows" ;;
    *)          echo "Unsupported OS: $(uname -s)" && exit 1 ;;
esac

# Detect Arch
case "$(uname -m)" in
    x86_64)     ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    i386|i686)  ARCH="386" ;;
    *)          echo "Unsupported architecture: $(uname -m)" && exit 1 ;;
esac

# Get latest version
VERSION=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')

if [ -z "$VERSION" ]; then
    echo "Failed to fetch latest version"
    exit 1
fi

# Construct download URL
case "${OS}" in
    macos)
        if [ "$ARCH" = "arm64" ]; then
            EXT="tar.gz"
            FILENAME="microneo-${VERSION}-macos-arm64.tar.gz"
        else
            EXT="tar.gz"
            FILENAME="microneo-${VERSION}-osx.tar.gz"
        fi
        ;;
    linux)
        if [ "$ARCH" = "arm64" ]; then
            EXT="tar.gz"
            FILENAME="microneo-${VERSION}-linux-arm64.tar.gz"
        else
            EXT="tar.gz"
            FILENAME="microneo-${VERSION}-linux64.tar.gz"
        fi
        ;;
    windows)
        EXT="zip"
        if [ "$ARCH" = "arm64" ]; then
            FILENAME="microneo-${VERSION}-win-arm64.zip"
        else
            FILENAME="microneo-${VERSION}-win64.zip"
        fi
        ;;
esac

URL="https://github.com/${REPO}/releases/download/v${VERSION}/${FILENAME}"
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

echo "Downloading microNeo v${VERSION} for ${OS}-${ARCH}..."
curl -L# "$URL" -o "microneo.${EXT}"

echo "Installing..."
case "${EXT}" in
    tar.gz)  tar -xzf "microneo.${EXT}" ;;
    zip)     unzip -q "microneo.${EXT}" ;;
esac

# Find binary (tar/zip extracts into microneo-VERSION/ subdirectory)
BINARY=$(find . -name microneo -type f | head -1)
if [ -z "$BINARY" ]; then
    echo "Error: microneo binary not found in archive"
    exit 1
fi

# Move binary to install dir
if [ -w "$INSTALL_DIR" ]; then
    mv "$BINARY" "$INSTALL_DIR/microneo"
else
    echo "Need sudo to install to $INSTALL_DIR"
    sudo mv "$BINARY" "$INSTALL_DIR/microneo"
fi

cd /
rm -rf "$TEMP_DIR"

echo "microNeo v${VERSION} installed to ${INSTALL_DIR}/microneo"
echo "Run 'microneo --version' to verify"