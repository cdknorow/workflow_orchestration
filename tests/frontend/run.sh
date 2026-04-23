#!/usr/bin/env bash
# Boot an isolated Coral dev server + headless Chrome, run the frontend
# test suite against them, and clean up on exit.
#
# Requires:
#   - node (v18+) with the deps installed via `npm install` in this dir
#   - Google Chrome (macOS: /Applications/Google Chrome.app)
#   - A built dev coral binary (or CORAL_BIN env override)

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

CORAL_PORT="${CORAL_PORT:-8462}"
CDP_PORT="${CDP_PORT:-9222}"
CORAL_HOME="$(mktemp -d /tmp/coral-frontend-test.XXXXXX)"
CHROME_PROFILE="$(mktemp -d /tmp/coral-frontend-chrome.XXXXXX)"

CORAL_BIN="${CORAL_BIN:-}"
if [ -z "$CORAL_BIN" ]; then
    CORAL_BIN="$(mktemp -u /tmp/coral-frontend-test-bin.XXXXXX)"
    echo "Building dev coral binary at $CORAL_BIN..."
    (cd "$ROOT/coral-go" && go build -tags dev -o "$CORAL_BIN" ./cmd/coral/)
fi

# Pick a Chrome binary (macOS path, then PATH fallback).
CHROME="${CHROME:-}"
if [ -z "$CHROME" ]; then
    if [ -x "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" ]; then
        CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
    else
        CHROME="$(command -v google-chrome || command -v chromium || true)"
    fi
fi
if [ -z "$CHROME" ]; then
    echo "ERROR: could not find Chrome. Set CHROME=/path/to/chrome" >&2
    exit 2
fi

CORAL_PID=""
CHROME_PID=""
cleanup() {
    [ -n "$CORAL_PID" ] && kill "$CORAL_PID" 2>/dev/null || true
    [ -n "$CHROME_PID" ] && kill "$CHROME_PID" 2>/dev/null || true
    # Give Chrome a moment to flush its profile before we try to remove it.
    sleep 0.5
    rm -rf "$CORAL_HOME" "$CHROME_PROFILE" 2>/dev/null || true
}
trap cleanup EXIT

echo "Starting Coral dev server on :$CORAL_PORT (home=$CORAL_HOME)..."
"$CORAL_BIN" --host 127.0.0.1 --port "$CORAL_PORT" --home "$CORAL_HOME" --no-browser \
    > "$CORAL_HOME/server.log" 2>&1 &
CORAL_PID=$!

echo "Starting headless Chrome on CDP port $CDP_PORT..."
"$CHROME" --headless=new --disable-gpu \
    --remote-debugging-port="$CDP_PORT" \
    --user-data-dir="$CHROME_PROFILE" \
    --no-first-run --no-default-browser-check \
    about:blank > "$CHROME_PROFILE/chrome.log" 2>&1 &
CHROME_PID=$!

# Wait for both services to respond.
for i in $(seq 1 30); do
    if curl -sS "http://127.0.0.1:$CORAL_PORT/api/system/status" >/dev/null 2>&1 \
       && curl -sS "http://127.0.0.1:$CDP_PORT/json/version" >/dev/null 2>&1; then
        break
    fi
    sleep 0.3
done

if ! curl -sS "http://127.0.0.1:$CORAL_PORT/api/system/status" >/dev/null 2>&1; then
    echo "ERROR: Coral server did not come up" >&2
    cat "$CORAL_HOME/server.log"
    exit 2
fi
if ! curl -sS "http://127.0.0.1:$CDP_PORT/json/version" >/dev/null 2>&1; then
    echo "ERROR: Chrome CDP did not come up" >&2
    cat "$CHROME_PROFILE/chrome.log"
    exit 2
fi

export CORAL_URL="http://127.0.0.1:$CORAL_PORT"
export CDP_PORT
cd "$HERE"

# Install deps if needed.
if [ ! -d node_modules ]; then
    echo "Installing test dependencies..."
    npm install --silent
fi

node acf_model_field.test.js
