#!/usr/bin/env bash
# Tag and push a new patch release of Coral.
#
# Usage:
#   scripts/release.sh [--dry-run] [--yes|-y]
#
# What it does:
#   1. Finds the latest bare version tag (e.g. v1.2.1)
#   2. Bumps the patch version (v1.2.1 -> v1.2.2)
#   3. Pushes the current branch to origin
#   4. Creates and pushes two tags:
#      - v<new> (prod release)
#      - v<new>-forDropbox (beta/Dropbox release)

set -euo pipefail

DRY_RUN=false
AUTO_YES=false

for arg in "$@"; do
    case "$arg" in
        --dry-run)  DRY_RUN=true ;;
        --yes|-y)   AUTO_YES=true ;;
        *)          echo "Unknown flag: $arg"; exit 1 ;;
    esac
done

# ── Find latest bare version tag ────────────────────────────────────
LATEST_TAG=$(git tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)

if [ -z "$LATEST_TAG" ]; then
    echo "Error: no version tags found (expected vX.Y.Z format)"
    exit 1
fi

echo "Latest tag: $LATEST_TAG"

# ── Bump patch version ──────────────────────────────────────────────
VERSION="${LATEST_TAG#v}"
IFS='.' read -r MAJOR MINOR PATCH <<< "$VERSION"
NEW_PATCH=$((PATCH + 1))
NEW_VERSION="v${MAJOR}.${MINOR}.${NEW_PATCH}"
NEW_VERSION_DROPBOX="${NEW_VERSION}-forDropbox"
BRANCH=$(git rev-parse --abbrev-ref HEAD)

echo "New version: $NEW_VERSION"
echo ""
echo "Plan:"
echo "  1. Push branch '$BRANCH' to origin"
echo "  2. Tag: $NEW_VERSION (prod)"
echo "  3. Tag: $NEW_VERSION_DROPBOX (beta/Dropbox)"
echo ""

if $DRY_RUN; then
    echo "[dry-run] No changes made."
    exit 0
fi

# ── Confirm ─────────────────────────────────────────────────────────
if ! $AUTO_YES; then
    read -rp "Proceed? [y/N] " REPLY
    if [[ ! "$REPLY" =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 0
    fi
fi

# ── Push branch ─────────────────────────────────────────────────────
echo "-> Pushing branch '$BRANCH' to origin..."
git push origin "$BRANCH"

# ── Create and push tags ────────────────────────────────────────────
echo "-> Creating tag $NEW_VERSION..."
git tag "$NEW_VERSION"

echo "-> Creating tag $NEW_VERSION_DROPBOX..."
git tag "$NEW_VERSION_DROPBOX"

echo "-> Pushing tags..."
git push origin "$NEW_VERSION" "$NEW_VERSION_DROPBOX"

# ── Summary ─────────────────────────────────────────────────────────
echo ""
echo "Done! Release summary:"
echo "  Branch: $BRANCH"
echo "  Prod tag:    $NEW_VERSION"
echo "  Dropbox tag: $NEW_VERSION_DROPBOX"
