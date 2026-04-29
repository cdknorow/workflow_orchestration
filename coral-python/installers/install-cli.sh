#!/bin/bash
# Install Coral CLI tools to PATH by creating symlinks in /usr/local/bin.
# Run this after installing Coral.app to /Applications.
#
# Usage: /Applications/Coral.app/Contents/MacOS/install-cli.sh
#    or: ./install-cli.sh

set -e

APP_BIN="/Applications/Coral.app/Contents/MacOS"

if [ ! -d "$APP_BIN" ]; then
    echo "Error: Coral.app not found in /Applications."
    echo "Please drag Coral.app to /Applications first."
    exit 1
fi

LINK_DIR="/usr/local/bin"
mkdir -p "$LINK_DIR"

TOOLS=(coral coral-board launch-coral coral-hook-agentic-state coral-hook-message-check coral-hook-task-sync)

echo "Installing Coral CLI tools to $LINK_DIR..."
for tool in "${TOOLS[@]}"; do
    if [ -f "$APP_BIN/$tool" ]; then
        ln -sf "$APP_BIN/$tool" "$LINK_DIR/$tool"
        echo "  ✓ $tool"
    fi
done

echo ""
echo "Done! Coral CLI tools are now available on your PATH."
echo "Try: coral --help"
