#!/usr/bin/env bash
# Build Coral.app and Coral.dmg for macOS distribution.
#
# Prerequisites:
#   pip install py2app rumps
#   brew install create-dmg   (optional, for .dmg creation)
#
# Usage:
#   scripts/build_macos.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

APP_NAME="Coral"
ICON_PNG="src/coral/static/coral.png"
ICON_ICNS="Coral.icns"

echo "=== Building ${APP_NAME}.app ==="

# Step 1: Generate .icns from coral.png if needed
if [ ! -f "$ICON_ICNS" ]; then
    echo "→ Generating ${ICON_ICNS} from ${ICON_PNG}..."
    if command -v sips &>/dev/null && command -v iconutil &>/dev/null; then
        ICONSET_DIR="${APP_NAME}.iconset"
        mkdir -p "$ICONSET_DIR"
        for size in 16 32 64 128 256 512; do
            sips -z $size $size "$ICON_PNG" --out "${ICONSET_DIR}/icon_${size}x${size}.png" &>/dev/null
        done
        # Retina variants
        for size in 32 64 128 256 512 1024; do
            half=$((size / 2))
            sips -z $size $size "$ICON_PNG" --out "${ICONSET_DIR}/icon_${half}x${half}@2x.png" &>/dev/null
        done
        iconutil -c icns "$ICONSET_DIR" -o "$ICON_ICNS"
        rm -rf "$ICONSET_DIR"
        echo "  ✓ Created ${ICON_ICNS}"
    else
        echo "  ⚠ sips/iconutil not available — skipping .icns generation"
        echo "    The app will build without a custom icon."
    fi
fi

# Step 2: Clean previous builds
echo "→ Cleaning previous builds..."
rm -rf build dist

# Step 3: Run py2app
echo "→ Running py2app..."
python3 setup_app.py py2app

echo "  ✓ Built dist/${APP_NAME}.app"

# Step 4: Create .dmg (optional)
if command -v create-dmg &>/dev/null; then
    DMG_NAME="${APP_NAME}.dmg"
    echo "→ Creating ${DMG_NAME}..."
    rm -f "dist/${DMG_NAME}"
    create-dmg \
        --volname "$APP_NAME" \
        --volicon "$ICON_ICNS" \
        --window-pos 200 120 \
        --window-size 600 400 \
        --icon-size 100 \
        --icon "${APP_NAME}.app" 175 190 \
        --app-drop-link 425 190 \
        --no-internet-enable \
        "dist/${DMG_NAME}" \
        "dist/${APP_NAME}.app"
    echo "  ✓ Created dist/${DMG_NAME}"
else
    echo "  ⚠ create-dmg not found — skipping .dmg creation"
    echo "    Install with: brew install create-dmg"
fi

echo ""
echo "=== Build complete ==="
echo "  App: dist/${APP_NAME}.app"
[ -f "dist/${APP_NAME}.dmg" ] && echo "  DMG: dist/${APP_NAME}.dmg"
