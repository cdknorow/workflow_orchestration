#!/bin/bash
# Build coral-go macOS universal .app bundle + DMG
#
# Usage: ./installers/build-macos.sh [version]
#   Set CORAL_EDITION=forDropbox to build with demo edition limits.
# Output: installers/dist/Coral-<version>.dmg

set -euo pipefail

VERSION="${1:-dev}"
CORAL_EDITION="${CORAL_EDITION:-}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_DIR="$PROJECT_DIR/coral-go"
DIST_DIR="$SCRIPT_DIR/dist"
APP_DIR="$DIST_DIR/Coral.app"

echo "==> Building coral-go for macOS (universal) v${VERSION}"

# Build ldflags — always strip symbols; optionally set Edition for demo builds
LDFLAGS="-s -w"
if [ -n "$CORAL_EDITION" ]; then
    LDFLAGS="$LDFLAGS -X github.com/cdknorow/coral/internal/config.Edition=$CORAL_EDITION"
    echo "==> Edition: $CORAL_EDITION"
fi

rm -rf "$APP_DIR" "$DIST_DIR/coral-arm64" "$DIST_DIR/coral-amd64"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources" "$DIST_DIR"

cd "$GO_DIR"

# Build arm64
echo "==> Compiling coral (arm64)"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$DIST_DIR/coral-arm64" ./cmd/coral/

# Build amd64
echo "==> Compiling coral (amd64)"
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$DIST_DIR/coral-amd64" ./cmd/coral/

# Create universal binary
echo "==> Creating universal binary with lipo"
if command -v lipo &>/dev/null; then
    lipo -create -output "$APP_DIR/Contents/MacOS/coral" "$DIST_DIR/coral-arm64" "$DIST_DIR/coral-amd64"
else
    echo "WARNING: lipo not available (not on macOS). Using arm64 binary only."
    cp "$DIST_DIR/coral-arm64" "$APP_DIR/Contents/MacOS/coral"
fi
chmod +x "$APP_DIR/Contents/MacOS/coral"

# Build all pure-Go companion binaries as universal
echo "==> Compiling companion CLI binaries (universal)"
for cmd in launch-coral coral-board coral-hook-agentic-state coral-hook-message-check coral-hook-task-sync; do
    if [ -d "./cmd/$cmd" ]; then
        echo "    Building $cmd..."
        GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$DIST_DIR/${cmd}-arm64" ./cmd/$cmd/
        GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$DIST_DIR/${cmd}-amd64" ./cmd/$cmd/
        if command -v lipo &>/dev/null; then
            lipo -create -output "$APP_DIR/Contents/MacOS/$cmd" "$DIST_DIR/${cmd}-arm64" "$DIST_DIR/${cmd}-amd64"
            rm -f "$DIST_DIR/${cmd}-arm64" "$DIST_DIR/${cmd}-amd64"
        else
            cp "$DIST_DIR/${cmd}-arm64" "$APP_DIR/Contents/MacOS/$cmd"
            rm -f "$DIST_DIR/${cmd}-arm64" "$DIST_DIR/${cmd}-amd64"
        fi
        chmod +x "$APP_DIR/Contents/MacOS/$cmd"
    fi
done

# Build CGO binaries (tray + webview app — require native platform APIs)
echo "==> Compiling coral-tray + coral-app (CGO required)"
if [[ "$(uname -s)" == "Darwin" ]]; then
    for cmd in coral-tray coral-app; do
        echo "    Building $cmd..."
        GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -tags webview -ldflags="$LDFLAGS" -o "$DIST_DIR/${cmd}-arm64" ./cmd/$cmd/
        GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -tags webview -ldflags="$LDFLAGS" -o "$DIST_DIR/${cmd}-amd64" ./cmd/$cmd/
        lipo -create -output "$APP_DIR/Contents/MacOS/$cmd" "$DIST_DIR/${cmd}-arm64" "$DIST_DIR/${cmd}-amd64"
        rm -f "$DIST_DIR/${cmd}-arm64" "$DIST_DIR/${cmd}-amd64"
        chmod +x "$APP_DIR/Contents/MacOS/$cmd"
    done
else
    echo "WARNING: coral-tray and coral-app require macOS (CGO + Cocoa/WebKit). Skipping on $(uname -s)."
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
    <key>CFBundleExecutable</key><string>coral-tray</string>
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

# CLI install script (creates symlinks in /usr/local/bin)
cp "$SCRIPT_DIR/install-cli.sh" "$APP_DIR/Contents/MacOS/install-cli.sh"
chmod +x "$APP_DIR/Contents/MacOS/install-cli.sh"

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
