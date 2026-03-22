#!/bin/bash
# Build coral-go macOS universal .app bundle + DMG
#
# Usage: ./installers/build-macos.sh [version]
# Output: installers/dist/Coral-<version>.dmg

set -euo pipefail

VERSION="${1:-dev}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_DIR="$PROJECT_DIR/coral-go"
DIST_DIR="$SCRIPT_DIR/dist"
APP_DIR="$DIST_DIR/Coral.app"

echo "==> Building coral-go for macOS (universal) v${VERSION}"

rm -rf "$APP_DIR" "$DIST_DIR/coral-arm64" "$DIST_DIR/coral-amd64"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources" "$DIST_DIR"

cd "$GO_DIR"

# Build arm64
echo "==> Compiling coral (arm64)"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$DIST_DIR/coral-arm64" ./cmd/coral/

# Build amd64
echo "==> Compiling coral (amd64)"
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$DIST_DIR/coral-amd64" ./cmd/coral/

# Create universal binary
echo "==> Creating universal binary with lipo"
if command -v lipo &>/dev/null; then
    lipo -create -output "$APP_DIR/Contents/MacOS/coral" "$DIST_DIR/coral-arm64" "$DIST_DIR/coral-amd64"
else
    echo "WARNING: lipo not available (not on macOS). Using arm64 binary only."
    cp "$DIST_DIR/coral-arm64" "$APP_DIR/Contents/MacOS/coral"
fi
chmod +x "$APP_DIR/Contents/MacOS/coral"

# Build companion binaries (single arch for now — they're CLI tools)
echo "==> Compiling launch-coral"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$APP_DIR/Contents/MacOS/launch-coral" ./cmd/launch-coral/

echo "==> Compiling coral-board"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$APP_DIR/Contents/MacOS/coral-board" ./cmd/coral-board/

# Build tray app (requires CGO for native menu bar integration)
echo "==> Compiling coral-tray (CGO required)"
if [[ "$(uname -s)" == "Darwin" ]]; then
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -ldflags="-s -w" -o "$DIST_DIR/coral-tray-arm64" ./cmd/coral-tray/
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="-s -w" -o "$DIST_DIR/coral-tray-amd64" ./cmd/coral-tray/
    lipo -create -output "$APP_DIR/Contents/MacOS/coral-tray" "$DIST_DIR/coral-tray-arm64" "$DIST_DIR/coral-tray-amd64"
    rm -f "$DIST_DIR/coral-tray-arm64" "$DIST_DIR/coral-tray-amd64"
    chmod +x "$APP_DIR/Contents/MacOS/coral-tray"
else
    echo "WARNING: coral-tray requires macOS (CGO + Cocoa). Skipping on $(uname -s)."
fi

# Info.plist
echo "==> Creating Info.plist"
if [ -f "$PROJECT_DIR/scripts/Info.plist" ]; then
    sed "s/VERSION_PLACEHOLDER/${VERSION}/g" "$PROJECT_DIR/scripts/Info.plist" > "$APP_DIR/Contents/Info.plist"
else
    cat > "$APP_DIR/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>Coral</string>
    <key>CFBundleDisplayName</key><string>Coral</string>
    <key>CFBundleIdentifier</key><string>com.coral.dashboard</string>
    <key>CFBundleVersion</key><string>${VERSION}</string>
    <key>CFBundleShortVersionString</key><string>${VERSION}</string>
    <key>CFBundleExecutable</key><string>coral</string>
    <key>CFBundleIconFile</key><string>AppIcon</string>
    <key>LSMinimumSystemVersion</key><string>11.0</string>
    <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
fi

# App icon
if [ -f "$PROJECT_DIR/icons/coral.icns" ]; then
    cp "$PROJECT_DIR/icons/coral.icns" "$APP_DIR/Contents/Resources/AppIcon.icns"
fi

# Cleanup temp binaries
rm -f "$DIST_DIR/coral-arm64" "$DIST_DIR/coral-amd64"

# Create DMG
echo "==> Creating DMG"
DMG_NAME="Coral-${VERSION}.dmg"
rm -f "$DIST_DIR/$DMG_NAME"
if command -v hdiutil &>/dev/null; then
    hdiutil create -volname "Coral" -srcfolder "$APP_DIR" -ov -format UDZO "$DIST_DIR/$DMG_NAME"
else
    echo "WARNING: hdiutil not available (not on macOS). Creating tar.gz instead."
    cd "$DIST_DIR" && tar czf "Coral-${VERSION}.tar.gz" Coral.app
    DMG_NAME="Coral-${VERSION}.tar.gz"
fi

echo ""
echo "Done! Installer at: $DIST_DIR/$DMG_NAME"
echo "App bundle at: $APP_DIR"
