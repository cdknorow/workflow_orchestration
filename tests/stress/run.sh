#!/bin/bash
# Coral stress test — validates server stability under concurrent load.
#
# Usage:
#   ./tests/stress/run.sh [options]
#
# Options:
#   --teams N              Number of teams to launch (default: 3)
#   --agents-per-team N    Agents per team (default: 4)
#   --duration DURATION    How long to run the monitor phase (default: 60s)
#   --port PORT            Server port (default: 9420)
#   --backend BACKEND      Terminal backend: pty or tmux (default: pty)
#   --skip-build           Skip building the coral binary
#
# Requirements:
#   - Go installed (for building)
#   - tmux installed
#   - curl, jq available

set -euo pipefail

# ── Defaults ────────────────────────────────────────────────────────────
TEAMS=3
AGENTS_PER_TEAM=4
DURATION="60s"
PORT=9420
BACKEND="pty"
SKIP_BUILD=false

# ── Parse args ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --teams)          TEAMS="$2"; shift 2 ;;
        --agents-per-team) AGENTS_PER_TEAM="$2"; shift 2 ;;
        --duration)       DURATION="$2"; shift 2 ;;
        --port)           PORT="$2"; shift 2 ;;
        --backend)        BACKEND="$2"; shift 2 ;;
        --skip-build)     SKIP_BUILD=true; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Convert duration to seconds
DURATION_SECS="${DURATION%s}"

# Ensure common tool paths are available (Go, tmux, etc.)
for p in /usr/local/go/bin /opt/homebrew/bin /usr/local/bin; do
    [[ -d "$p" ]] && [[ ":$PATH:" != *":$p:"* ]] && export PATH="$p:$PATH"
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
GO_DIR="$PROJECT_DIR/coral-go"
TEST_ID="stress-$$-$(date +%s)"
DATA_DIR="/tmp/.coral-${TEST_ID}"
TMUX_SOCKET="${DATA_DIR}/tmux.sock"
CORAL_BIN="${GO_DIR}/coral-stress-test"
MOCK_AGENT_BIN="${GO_DIR}/mock-agent-stress-test"
LOG_FILE="${DATA_DIR}/stress-test.log"
SERVER_PID=""
PASSED=0
FAILED=0
TOTAL=0

# ── Colors ──────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# ── Helpers ─────────────────────────────────────────────────────────────
log()  { echo -e "[$(date +%H:%M:%S)] $*" | tee -a "$LOG_FILE"; }
pass() { TOTAL=$((TOTAL+1)); PASSED=$((PASSED+1)); log "${GREEN}PASS${NC}: $1"; }
fail() { TOTAL=$((TOTAL+1)); FAILED=$((FAILED+1)); log "${RED}FAIL${NC}: $1"; }
warn() { log "${YELLOW}WARN${NC}: $1"; }

api() {
    local method="$1" path="$2"
    shift 2
    curl -s -X "$method" "http://127.0.0.1:${PORT}${path}" "$@"
}

# Count running agent sessions (works for both backends)
count_agent_sessions() {
    local count
    if [[ "$BACKEND" == "tmux" ]]; then
        count=$(tmux -S "$TMUX_SOCKET" list-sessions 2>/dev/null | wc -l | tr -d '[:space:]') || true
    else
        count=$(pgrep -f "mock-agent.*stress-test.*--session-id" 2>/dev/null | wc -l | tr -d '[:space:]') || true
    fi
    echo "${count:-0}"
}

# Kill all agent sessions
kill_agent_sessions() {
    tmux -S "$TMUX_SOCKET" kill-server 2>/dev/null || true
    pkill -f "mock-agent.*stress-test.*--session-id" 2>/dev/null || true
}

wait_for_server() {
    local max_wait=30
    for i in $(seq 1 $max_wait); do
        if curl -s "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

# ── Cleanup ─────────────────────────────────────────────────────────────
cleanup() {
    log "Cleaning up..."
    # Kill all agent sessions and tmux server
    kill_agent_sessions 2>/dev/null || true
    # Kill server
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    # Clean up data dir (keep log for review)
    if [[ -d "$DATA_DIR" ]]; then
        cp "$LOG_FILE" "/tmp/coral-stress-${TEST_ID}.log" 2>/dev/null || true
        rm -rf "$DATA_DIR"
    fi
    # Clean up binary
    rm -f "$CORAL_BIN" "$MOCK_AGENT_BIN"
}
trap cleanup EXIT

# ── Pre-test cleanup ────────────────────────────────────────────────────
# Kill any dangling processes from previous runs
pkill -f "coral-stress-test" 2>/dev/null || true
pkill -f "mock-agent.*stress-test" 2>/dev/null || true
# Kill any tmux servers on old stress test sockets
for old_sock in /tmp/.coral-stress-*/tmux.sock; do
    tmux -S "$old_sock" kill-server 2>/dev/null || true
done
# Remove old stress test data dirs
rm -rf /tmp/.coral-stress-* 2>/dev/null || true
sleep 1

# ── Setup ───────────────────────────────────────────────────────────────
mkdir -p "$DATA_DIR"
touch "$LOG_FILE"

log "════════════════════════════════════════════════════════════"
log "Coral Stress Test"
log "════════════════════════════════════════════════════════════"
log "  Test ID:         $TEST_ID"
log "  Teams:           $TEAMS"
log "  Agents/team:     $AGENTS_PER_TEAM"
log "  Duration:        $DURATION"
log "  Port:            $PORT"
log "  Backend:         $BACKEND"
log "  Data dir:        $DATA_DIR"
log "  Tmux socket:     $TMUX_SOCKET"
log ""

# ── Phase 0: Build ──────────────────────────────────────────────────────
if [[ "$SKIP_BUILD" == "false" ]]; then
    log "Phase 0: Building coral + mock-agent binaries..."
    cd "$GO_DIR"
    go build -tags dev -o "$CORAL_BIN" ./cmd/coral/ 2>&1 | tee -a "$LOG_FILE"
    go build -o "$MOCK_AGENT_BIN" ./cmd/mock-agent/ 2>&1 | tee -a "$LOG_FILE"
    log "  Built: $CORAL_BIN"
    log "  Built: $MOCK_AGENT_BIN"
else
    CORAL_BIN="${GO_DIR}/coral"
    MOCK_AGENT_BIN="${GO_DIR}/mock-agent"
    if [[ ! -f "$CORAL_BIN" ]]; then
        log "ERROR: --skip-build specified but $CORAL_BIN not found"
        exit 1
    fi
    if [[ ! -f "$MOCK_AGENT_BIN" ]]; then
        log "ERROR: --skip-build specified but $MOCK_AGENT_BIN not found"
        exit 1
    fi
    log "Phase 0: Using existing binaries: $CORAL_BIN, $MOCK_AGENT_BIN"
fi

# ── Phase 1: Launch server ──────────────────────────────────────────────
log ""
log "Phase 1: Launching server..."

# Use --home flag for full data isolation. All server paths (DB, tmux socket,
# uploads, logs, tracking, board state) are routed to the isolated dir.
REAL_HOME="$HOME"

# Snapshot real ~/.coral for isolation verification later
REAL_CORAL_DIR="${REAL_HOME}/.coral"
SNAPSHOT_FILE="${DATA_DIR}/home-coral-snapshot.txt"
if [[ -d "$REAL_CORAL_DIR" ]]; then
    find "$REAL_CORAL_DIR" -type f -newer "$LOG_FILE" 2>/dev/null | sort > "$SNAPSHOT_FILE" || true
fi

# Export CORAL_PORT so the tmux server (and all mock-agent sessions) inherit it
export CORAL_PORT="$PORT"
"$CORAL_BIN" --home "$DATA_DIR" --host 127.0.0.1 --port "$PORT" --backend "$BACKEND" --no-browser >> "$LOG_FILE" 2>&1 &
SERVER_PID=$!
log "  Server PID: $SERVER_PID"

if wait_for_server; then
    pass "Server started on port $PORT"
else
    fail "Server failed to start within 30s"
    cat "$LOG_FILE"
    exit 1
fi

# Configure server to use mock-agent CLI instead of real claude
SETTINGS_BODY=$(cat <<EOJSON
{"cli_path_claude":"${MOCK_AGENT_BIN}"}
EOJSON
)
SETTINGS_RESP=$(api PUT /api/settings -H "Content-Type: application/json" -d "$SETTINGS_BODY" 2>/dev/null || echo "")
log "  Configured mock-agent CLI: $MOCK_AGENT_BIN"

# Verify health endpoint
HEALTH=$(api GET /api/health 2>/dev/null || echo "")
if echo "$HEALTH" | grep -q '"ok"'; then
    pass "Health endpoint returns ok"
else
    fail "Health endpoint returned: $HEALTH"
fi

# ── Phase 2: Launch agents ──────────────────────────────────────────────
log ""
log "Phase 2: Launching $TEAMS teams x $AGENTS_PER_TEAM agents..."

TOTAL_AGENTS=$((TEAMS * AGENTS_PER_TEAM))
LAUNCHED=0

# Launch agents via the team API so they're on shared boards (needed for reset team test)
WORK_DIR="${DATA_DIR}/workdir"
mkdir -p "$WORK_DIR"
# Resolve symlinks (macOS: /tmp → /private/tmp) so comparisons match the server's filepath.Abs()
WORK_DIR="$(cd "$WORK_DIR" && pwd -P)"

for t in $(seq 1 $TEAMS); do
    BOARD_NAME="stress-team-${t}"

    # Build agents JSON array
    AGENTS_JSON="["
    for a in $(seq 1 $AGENTS_PER_TEAM); do
        [[ $a -gt 1 ]] && AGENTS_JSON="${AGENTS_JSON},"
        AGENTS_JSON="${AGENTS_JSON}{\"name\":\"Agent ${t}-${a}\",\"prompt\":\"You are stress test agent ${t}-${a}. Run: while true; do echo heartbeat; sleep 2; done\"}"
    done
    AGENTS_JSON="${AGENTS_JSON}]"

    LAUNCH_BODY=$(cat <<EOJSON
{"board_name":"${BOARD_NAME}","working_dir":"${WORK_DIR}","agent_type":"claude","agents":${AGENTS_JSON}}
EOJSON
)

    RESP=$(api POST /api/sessions/launch-team -H "Content-Type: application/json" -d "$LAUNCH_BODY" 2>/dev/null || echo "")
    TEAM_LAUNCHED=$(echo "$RESP" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    agents = data.get('agents', [])
    print(sum(1 for a in agents if 'error' not in a))
except: print(0)
" 2>/dev/null || echo "0")
    LAUNCHED=$((LAUNCHED + TEAM_LAUNCHED))
    log "  Team $BOARD_NAME: launched $TEAM_LAUNCHED/$AGENTS_PER_TEAM agents"
done

if [[ $LAUNCHED -eq $TOTAL_AGENTS ]]; then
    pass "Launched all $TOTAL_AGENTS agents via team API"
elif [[ $LAUNCHED -gt 0 ]]; then
    warn "Launched $LAUNCHED/$TOTAL_AGENTS agents"
else
    fail "Failed to launch any agents"
fi

# Validate: agent sessions are running
sleep 2
SESSION_COUNT=$(count_agent_sessions)
SESSION_COUNT="${SESSION_COUNT:-0}"
if [[ "$SESSION_COUNT" -eq "$TOTAL_AGENTS" ]]; then
    pass "Agent session count matches: $SESSION_COUNT ($BACKEND)"
elif [[ "$SESSION_COUNT" -gt 0 ]]; then
    warn "Agent sessions: $SESSION_COUNT (expected $TOTAL_AGENTS, backend=$BACKEND)"
else
    fail "No agent sessions found (backend=$BACKEND)"
fi

# Validate: API live session count
LIVE_COUNT=$(api GET /api/sessions/live 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
log "  Live sessions reported by API: $LIVE_COUNT"
if [[ "$LIVE_COUNT" -gt 0 ]]; then
    pass "API reports $LIVE_COUNT live sessions"
else
    fail "API reports 0 live sessions"
fi

# Debug: show DB contents to identify extra sessions
DB_PATH="${DATA_DIR}/sessions.db"
if command -v sqlite3 &>/dev/null && [[ -f "$DB_PATH" ]]; then
    log "  DB live_sessions:"
    sqlite3 "$DB_PATH" "SELECT agent_name, board_name, is_sleeping FROM live_sessions" 2>/dev/null | while read -r line; do
        log "    $line"
    done
fi

# Collect agent names for WebSocket cycling
AGENT_NAMES=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
for s in json.load(sys.stdin):
    print(s.get('name', ''))
" 2>/dev/null || true)

# ── Phase 3: Monitor phase + WebSocket cycling ─────────────────────────
log ""
log "Phase 3: Monitoring for ${DURATION} (checking for panics, leaks, crashes)..."

# Start WebSocket terminal cycling in background (exercises tmux polling/fsnotify)
WS_CYCLE_PID=""
if command -v python3 &>/dev/null; then
    python3 -c "
import asyncio, json, random, sys, time

async def ws_cycle(port, names, duration):
    try:
        import websockets
    except ImportError:
        print('websockets not installed, skipping WS cycling', file=sys.stderr)
        return

    end = time.time() + duration
    errors = 0
    connects = 0

    async def cycle_terminal(name):
        nonlocal errors, connects
        try:
            uri = f'ws://127.0.0.1:{port}/ws/terminal/{name}'
            async with websockets.connect(uri, close_timeout=2) as ws:
                connects += 1
                hold = random.uniform(2, 5)
                deadline = time.time() + hold
                while time.time() < deadline and time.time() < end:
                    try:
                        msg = await asyncio.wait_for(ws.recv(), timeout=1.0)
                    except asyncio.TimeoutError:
                        pass
        except Exception as e:
            errors += 1

    while time.time() < end:
        tasks = [cycle_terminal(random.choice(names)) for _ in range(min(3, len(names)))]
        await asyncio.gather(*tasks)

    print(f'WS cycling: {connects} connects, {errors} errors', file=sys.stderr)

names = '''${AGENT_NAMES}'''.strip().split('\n')
names = [n for n in names if n]
if names:
    asyncio.run(ws_cycle(${PORT}, names, ${DURATION_SECS}))
" >> "$LOG_FILE" 2>&1 &
    WS_CYCLE_PID=$!
    log "  WebSocket cycling started (PID: $WS_CYCLE_PID)"
else
    log "  Skipping WebSocket cycling (python3 not available)"
fi

MONITOR_START=$(date +%s)
MONITOR_END=$((MONITOR_START + DURATION_SECS))
CHECK_INTERVAL=5
CHECKS=0
ERRORS=0

while [[ $(date +%s) -lt $MONITOR_END ]]; do
    CHECKS=$((CHECKS+1))

    # Check server is alive
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        fail "Server process died during monitoring (check $CHECKS)"
        ERRORS=$((ERRORS+1))
        break
    fi

    # Check health endpoint
    if ! curl -s "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
        fail "Health endpoint unreachable (check $CHECKS)"
        ERRORS=$((ERRORS+1))
    fi

    # Check for panics in server log (not our own test output)
    if grep -i "panic\|fatal\|concurrent map" "$LOG_FILE" 2>/dev/null | grep -v "Phase\|checking for\|defer\|recovery\|STARTUP" | grep -q .; then
        PANIC_LINE=$(grep -i "panic\|fatal\|concurrent map" "$LOG_FILE" | grep -v "Phase\|checking for\|defer\|recovery\|STARTUP" | head -1)
        fail "Panic detected in log: $PANIC_LINE"
        ERRORS=$((ERRORS+1))
        break
    fi

    # Check FD count (macOS)
    if command -v lsof &>/dev/null; then
        FD_COUNT=$(lsof -p "$SERVER_PID" 2>/dev/null | wc -l | tr -d ' ')
        if [[ "$FD_COUNT" -gt 1000 ]]; then
            warn "High FD count: $FD_COUNT (possible leak)"
        fi
    fi

    # Check goroutine count via runtime debug (if pprof is available)
    GOROUTINES=$(curl -s "http://127.0.0.1:${PORT}/debug/pprof/goroutine?debug=0" 2>/dev/null | head -1 || echo "")

    # Hammer the API with concurrent requests to stress concurrency
    CURL_PIDS=""
    for i in $(seq 1 5); do
        api GET /api/sessions/live > /dev/null 2>&1 & CURL_PIDS="$CURL_PIDS $!"
        api GET /api/health > /dev/null 2>&1 & CURL_PIDS="$CURL_PIDS $!"
    done
    for p in $CURL_PIDS; do wait "$p" 2>/dev/null || true; done

    sleep $CHECK_INTERVAL
done

if [[ $ERRORS -eq 0 ]]; then
    pass "Server survived ${DURATION} monitoring ($CHECKS checks, 0 errors)"
else
    fail "Server had $ERRORS errors during monitoring"
fi

# Stop WebSocket cycling
if [[ -n "$WS_CYCLE_PID" ]] && kill -0 "$WS_CYCLE_PID" 2>/dev/null; then
    kill "$WS_CYCLE_PID" 2>/dev/null || true
    wait "$WS_CYCLE_PID" 2>/dev/null || true
    log "  WebSocket cycling stopped"
fi

# ── Phase 3.5: Validate board messages ────────────────────────────────
log ""
log "Phase 3.5: Checking mock-agent board activity..."

# Check that agents are posting messages to their boards
BOARDS_WITH_MESSAGES=0
for t in $(seq 1 $TEAMS); do
    BOARD="stress-team-${t}"
    MSG_COUNT=$(api GET "/api/board/${BOARD}/messages/all" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    msgs = data if isinstance(data, list) else data.get('messages', [])
    print(len(msgs))
except: print(0)
" 2>/dev/null || echo "0")
    log "  Board $BOARD: $MSG_COUNT messages (via /messages/all)"
    if [[ "$MSG_COUNT" -gt 0 ]]; then
        BOARDS_WITH_MESSAGES=$((BOARDS_WITH_MESSAGES + 1))
    fi
done

if [[ "$BOARDS_WITH_MESSAGES" -eq "$TEAMS" ]]; then
    pass "All $TEAMS boards have messages from mock agents"
elif [[ "$BOARDS_WITH_MESSAGES" -gt 0 ]]; then
    warn "Only $BOARDS_WITH_MESSAGES/$TEAMS boards have messages"
else
    fail "No boards have messages — mock agents are not posting to the board"
fi

# Validate cursor advancement: read messages on behalf of an agent, then read
# again and verify the cursor advanced (second read returns fewer/no messages)
# Board now uses subscriber_id (the role/display_name) as stable identity.
# Use display_name from the live sessions API as the subscriber_id for reads.
CURSOR_BOARD="stress-team-1"
CURSOR_SUB_ID=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json, urllib.parse
for s in json.load(sys.stdin):
    if s.get('board_project') == '${CURSOR_BOARD}':
        print(urllib.parse.quote(s.get('display_name', ''), safe=''))
        break
" 2>/dev/null || true)

if [[ -n "$CURSOR_SUB_ID" ]]; then
    # First read: should return unread messages and advance cursor
    READ1_COUNT=$(api GET "/api/board/${CURSOR_BOARD}/messages?subscriber_id=${CURSOR_SUB_ID}" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    msgs = data if isinstance(data, list) else data.get('messages', [])
    print(len(msgs))
except: print(0)
" 2>/dev/null || echo "0")

    # Second read: cursor should have advanced, so fewer (ideally 0) new messages
    READ2_COUNT=$(api GET "/api/board/${CURSOR_BOARD}/messages?subscriber_id=${CURSOR_SUB_ID}" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    msgs = data if isinstance(data, list) else data.get('messages', [])
    print(len(msgs))
except: print(0)
" 2>/dev/null || echo "0")

    CURSOR_SUB_DISPLAY=$(python3 -c "import urllib.parse; print(urllib.parse.unquote('${CURSOR_SUB_ID}'))" 2>/dev/null || echo "$CURSOR_SUB_ID")
    log "  Cursor test on $CURSOR_BOARD (subscriber_id=$CURSOR_SUB_DISPLAY): read1=$READ1_COUNT, read2=$READ2_COUNT"
    if [[ "$READ1_COUNT" -gt 0 ]] && [[ "$READ2_COUNT" -lt "$READ1_COUNT" ]]; then
        pass "Board read cursor advances (read1=$READ1_COUNT → read2=$READ2_COUNT)"
    elif [[ "$READ1_COUNT" -gt 0 ]] && [[ "$READ2_COUNT" -eq "$READ1_COUNT" ]]; then
        fail "Board read cursor did NOT advance (both reads returned $READ1_COUNT)"
    elif [[ "$READ1_COUNT" -eq 0 ]]; then
        fail "Board read returned 0 messages for subscriber $CURSOR_SUB_ID"
    fi
else
    warn "Could not find an agent on $CURSOR_BOARD for cursor test"
fi

# Validate @mention triggers unread notification for the mentioned agent
MENTION_BOARD="stress-team-1"
MENTION_AGENTS=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json, urllib.parse
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${MENTION_BOARD}']
for s in board[:2]:
    print(s.get('display_name', ''))
" 2>/dev/null || true)

MENTION_TARGET=$(echo "$MENTION_AGENTS" | head -1)
MENTION_SENDER=$(echo "$MENTION_AGENTS" | tail -1)

if [[ -n "$MENTION_TARGET" ]] && [[ -n "$MENTION_SENDER" ]]; then
    MENTION_SENDER_ENC=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${MENTION_SENDER}', safe=''))" 2>/dev/null)
    MENTION_TARGET_ENC=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${MENTION_TARGET}', safe=''))" 2>/dev/null)

    # Clear target's unread by reading all current messages
    api GET "/api/board/${MENTION_BOARD}/messages?subscriber_id=${MENTION_TARGET_ENC}" > /dev/null 2>&1

    # Post a message that @mentions the target agent
    MENTION_BODY=$(python3 -c "
import json
print(json.dumps({'subscriber_id': '${MENTION_SENDER}', 'content': 'Hey @${MENTION_TARGET}, this is a mention test'}))
" 2>/dev/null)
    api POST "/api/board/${MENTION_BOARD}/messages" -H "Content-Type: application/json" -d "$MENTION_BODY" > /dev/null 2>&1

    sleep 1

    # Check unread count for the mentioned agent
    MENTION_UNREAD=$(api GET "/api/board/${MENTION_BOARD}/messages/check?subscriber_id=${MENTION_TARGET_ENC}" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(data.get('unread', data.get('count', 0)))
except: print(0)
" 2>/dev/null || echo "0")

    log "  @mention test: sent '@${MENTION_TARGET}' from ${MENTION_SENDER}, target unread=$MENTION_UNREAD"
    if [[ "$MENTION_UNREAD" -gt 0 ]]; then
        pass "@mention triggers unread for mentioned agent (unread=$MENTION_UNREAD)"
    else
        fail "@mention did not trigger unread for mentioned agent $MENTION_TARGET"
    fi
else
    warn "Could not find agents on $MENTION_BOARD for mention test"
fi

# ── Phase 3.6: Cross-board notification isolation ──────────────────────
# Verifies that when the same role name exists on multiple boards, notifications
# target the correct board (regression test for stale subscription lookup bug).
log ""
log "Phase 3.6: Cross-board notification isolation..."

if [[ $TEAMS -ge 2 ]]; then
    XBOARD1="stress-team-1"
    XBOARD2="stress-team-2"

    # Find an agent on board-1 and one on board-2 with matching display_names
    XBOARD1_AGENTS=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board1 = [s.get('display_name','') for s in sessions if s.get('board_project') == '${XBOARD1}']
print('\n'.join(board1[:1]))
" 2>/dev/null || true)
    XBOARD2_AGENTS=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board2 = [s for s in sessions if s.get('board_project') == '${XBOARD2}']
# Get first two agents: one to post, one to receive
for s in board2[:2]:
    print(s.get('display_name',''))
" 2>/dev/null || true)

    XBOARD2_TARGET=$(echo "$XBOARD2_AGENTS" | head -1)
    XBOARD2_SENDER=$(echo "$XBOARD2_AGENTS" | tail -1)

    if [[ -n "$XBOARD2_TARGET" ]] && [[ -n "$XBOARD2_SENDER" ]] && [[ "$XBOARD2_TARGET" != "$XBOARD2_SENDER" ]]; then
        XBOARD2_TARGET_ENC=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${XBOARD2_TARGET}', safe=''))" 2>/dev/null)
        XBOARD2_SENDER_ENC=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${XBOARD2_SENDER}', safe=''))" 2>/dev/null)

        # Clear target's unread on board-2
        api GET "/api/board/${XBOARD2}/messages?subscriber_id=${XBOARD2_TARGET_ENC}" > /dev/null 2>&1

        # Post a message on board-2 mentioning the target
        XBODY=$(python3 -c "
import json
print(json.dumps({'subscriber_id': '${XBOARD2_SENDER}', 'content': '@${XBOARD2_TARGET} cross-board notification test'}))
" 2>/dev/null)
        api POST "/api/board/${XBOARD2}/messages" -H "Content-Type: application/json" -d "$XBODY" > /dev/null 2>&1
        sleep 1

        # Check unread on board-2 for the target — should have unread
        XUNREAD2=$(api GET "/api/board/${XBOARD2}/messages/check?subscriber_id=${XBOARD2_TARGET_ENC}" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(data.get('unread', data.get('count', 0)))
except: print(0)
" 2>/dev/null || echo "0")

        # Check unread on board-1 for the same display name — should have 0
        XUNREAD1=$(api GET "/api/board/${XBOARD1}/messages/check?subscriber_id=${XBOARD2_TARGET_ENC}" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(data.get('unread', data.get('count', 0)))
except: print(0)
" 2>/dev/null || echo "0")

        log "  Cross-board test: mentioned @${XBOARD2_TARGET} on ${XBOARD2}"
        log "    board-2 unread=$XUNREAD2, board-1 unread=$XUNREAD1"

        if [[ "$XUNREAD2" -gt 0 ]]; then
            pass "Cross-board: notification targets correct board (board-2 unread=$XUNREAD2)"
        else
            fail "Cross-board: expected unread on board-2 for @${XBOARD2_TARGET}"
        fi
        # Board-1 may have unread from other activity, so we just log (not fail)
    else
        warn "Could not find distinct agents on $XBOARD2 for cross-board test"
    fi
else
    warn "Need at least 2 teams for cross-board notification test (have $TEAMS)"
fi

# ── Phase 3.7: Server restart — state preservation ────────────────────
# Shut down the server process mid-test, restart it, and verify that all
# persistent state (sessions DB, board messages, subscriptions, unread
# cursors) survives the restart without loss.
log ""
log "Phase 3.7: Server restart — state preservation..."

# ── Snapshot state BEFORE restart ──────────────────────────────────────
PRE_RESTART_SESSIONS=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
PRE_RESTART_SESSION_COUNT=$(echo "$PRE_RESTART_SESSIONS" | python3 -c "
import sys, json
try: print(len(json.load(sys.stdin)))
except: print(0)
" 2>/dev/null || echo "0")

PRE_RESTART_BOARDS=$(api GET /api/board/projects 2>/dev/null || echo "[]")
PRE_RESTART_BOARD_COUNT=$(echo "$PRE_RESTART_BOARDS" | python3 -c "
import sys, json
try: print(len(json.load(sys.stdin)))
except: print(0)
" 2>/dev/null || echo "0")

# Capture per-board message counts and subscriber counts
PRE_RESTART_BOARD_STATE=$(echo "$PRE_RESTART_BOARDS" | python3 -c "
import sys, json
try:
    boards = json.load(sys.stdin)
    for b in sorted(boards, key=lambda x: x.get('project','')):
        print(f\"{b['project']}|{b.get('message_count',0)}|{b.get('subscriber_count',0)}\")
except: pass
" 2>/dev/null || true)

# Capture session IDs and board assignments
PRE_RESTART_SESSION_IDS=$(echo "$PRE_RESTART_SESSIONS" | python3 -c "
import sys, json
try:
    for s in sorted(json.load(sys.stdin), key=lambda x: x.get('session_id','')):
        print(f\"{s.get('session_id','')}|{s.get('board_project','')}|{s.get('display_name','')}\")
except: pass
" 2>/dev/null || true)

# Post a marker message on each board so we can verify it survives restart
for t in $(seq 1 $TEAMS); do
    RESTART_BOARD="stress-team-${t}"
    RESTART_SENDER=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json, urllib.parse
for s in json.load(sys.stdin):
    if s.get('board_project') == '${RESTART_BOARD}':
        print(s.get('display_name', ''))
        break
" 2>/dev/null || true)
    if [[ -n "$RESTART_SENDER" ]]; then
        MARKER_BODY=$(python3 -c "
import json
print(json.dumps({'subscriber_id': '${RESTART_SENDER}', 'content': 'RESTART_MARKER_${t}: message posted before server restart'}))
" 2>/dev/null)
        api POST "/api/board/${RESTART_BOARD}/messages" -H "Content-Type: application/json" -d "$MARKER_BODY" > /dev/null 2>&1
    fi
done

# Re-snapshot board state after marker messages
PRE_RESTART_MSG_COUNTS=""
for t in $(seq 1 $TEAMS); do
    RESTART_BOARD="stress-team-${t}"
    MC=$(api GET "/api/board/${RESTART_BOARD}/messages/all" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    msgs = data if isinstance(data, list) else data.get('messages', [])
    print(len(msgs))
except: print(0)
" 2>/dev/null || echo "0")
    PRE_RESTART_MSG_COUNTS="${PRE_RESTART_MSG_COUNTS}${RESTART_BOARD}=${MC} "
done

log "  Pre-restart state:"
log "    Sessions: $PRE_RESTART_SESSION_COUNT"
log "    Boards: $PRE_RESTART_BOARD_COUNT"
log "    Messages: $PRE_RESTART_MSG_COUNTS"

# ── Kill the server ────────────────────────────────────────────────────
log "  Stopping server (PID $SERVER_PID)..."
kill "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""
sleep 2

# Verify server is actually down
if curl -s --max-time 2 "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
    fail "Server still responding after kill — not a clean shutdown"
else
    log "  Server stopped"
fi

# ── Restart the server ─────────────────────────────────────────────────
log "  Restarting server..."
"$CORAL_BIN" --home "$DATA_DIR" --host 127.0.0.1 --port "$PORT" --backend "$BACKEND" --no-browser >> "$LOG_FILE" 2>&1 &
SERVER_PID=$!
log "  New server PID: $SERVER_PID"

if wait_for_server; then
    pass "Server restarted successfully"
else
    fail "Server failed to restart within 30s"
    cat "$LOG_FILE" | tail -30
    exit 1
fi

# Re-configure mock-agent CLI (settings are in DB, but verify)
SETTINGS_BODY=$(cat <<EOJSON
{"cli_path_claude":"${MOCK_AGENT_BIN}"}
EOJSON
)
api PUT /api/settings -H "Content-Type: application/json" -d "$SETTINGS_BODY" > /dev/null 2>&1

# ── Verify state AFTER restart ─────────────────────────────────────────
sleep 2  # Brief settle time for DB reconnection

# 1. Check board projects survived
POST_RESTART_BOARDS=$(api GET /api/board/projects 2>/dev/null || echo "[]")
POST_RESTART_BOARD_COUNT=$(echo "$POST_RESTART_BOARDS" | python3 -c "
import sys, json
try: print(len(json.load(sys.stdin)))
except: print(0)
" 2>/dev/null || echo "0")

if [[ "$POST_RESTART_BOARD_COUNT" -eq "$PRE_RESTART_BOARD_COUNT" ]]; then
    pass "Server restart: board count preserved ($POST_RESTART_BOARD_COUNT boards)"
else
    fail "Server restart: board count changed ($PRE_RESTART_BOARD_COUNT → $POST_RESTART_BOARD_COUNT)"
fi

# 2. Check per-board message counts (should be >= pre-restart, mock agents may post more)
RESTART_MSG_OK=0
RESTART_MSG_TOTAL=0
for t in $(seq 1 $TEAMS); do
    RESTART_BOARD="stress-team-${t}"
    POST_MC=$(api GET "/api/board/${RESTART_BOARD}/messages/all" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    msgs = data if isinstance(data, list) else data.get('messages', [])
    print(len(msgs))
except: print(0)
" 2>/dev/null || echo "0")

    # Extract pre-restart count for this board
    PRE_MC=$(echo "$PRE_RESTART_MSG_COUNTS" | grep -o "${RESTART_BOARD}=[0-9]*" | cut -d= -f2)
    PRE_MC="${PRE_MC:-0}"

    RESTART_MSG_TOTAL=$((RESTART_MSG_TOTAL + 1))
    if [[ "$POST_MC" -ge "$PRE_MC" ]]; then
        RESTART_MSG_OK=$((RESTART_MSG_OK + 1))
        log "    $RESTART_BOARD: $PRE_MC → $POST_MC messages (OK)"
    else
        log "    $RESTART_BOARD: $PRE_MC → $POST_MC messages (LOST MESSAGES)"
    fi
done

if [[ "$RESTART_MSG_OK" -eq "$RESTART_MSG_TOTAL" ]]; then
    pass "Server restart: all board messages preserved"
else
    fail "Server restart: messages lost on $((RESTART_MSG_TOTAL - RESTART_MSG_OK))/$RESTART_MSG_TOTAL boards"
fi

# 3. Check marker messages are still readable
MARKERS_FOUND=0
for t in $(seq 1 $TEAMS); do
    RESTART_BOARD="stress-team-${t}"
    MARKER_FOUND=$(api GET "/api/board/${RESTART_BOARD}/messages/all" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    msgs = data if isinstance(data, list) else data.get('messages', [])
    found = any('RESTART_MARKER_${t}' in m.get('content','') for m in msgs)
    print(1 if found else 0)
except: print(0)
" 2>/dev/null || echo "0")
    if [[ "$MARKER_FOUND" == "1" ]]; then
        MARKERS_FOUND=$((MARKERS_FOUND + 1))
    fi
done

if [[ "$MARKERS_FOUND" -eq "$TEAMS" ]]; then
    pass "Server restart: all $TEAMS marker messages survived restart"
else
    fail "Server restart: only $MARKERS_FOUND/$TEAMS marker messages found after restart"
fi

# 4. Check board subscribers survived
POST_RESTART_BOARD_STATE=$(echo "$POST_RESTART_BOARDS" | python3 -c "
import sys, json
try:
    boards = json.load(sys.stdin)
    for b in sorted(boards, key=lambda x: x.get('project','')):
        print(f\"{b['project']}|{b.get('message_count',0)}|{b.get('subscriber_count',0)}\")
except: pass
" 2>/dev/null || true)

SUBS_MATCH=true
while IFS='|' read -r project msgs subs; do
    [[ -z "$project" ]] && continue
    PRE_SUBS=$(echo "$PRE_RESTART_BOARD_STATE" | grep "^${project}|" | cut -d'|' -f3)
    if [[ "$subs" -ne "${PRE_SUBS:-0}" ]]; then
        log "    $project: subscriber count changed ($PRE_SUBS → $subs)"
        SUBS_MATCH=false
    fi
done <<< "$POST_RESTART_BOARD_STATE"

if [[ "$SUBS_MATCH" == "true" ]]; then
    pass "Server restart: board subscriber counts preserved"
else
    fail "Server restart: board subscriber counts changed after restart"
fi

# 5. Check sessions DB survived (live_sessions may differ since tmux sessions are still running)
POST_RESTART_SESSIONS=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
POST_RESTART_SESSION_COUNT=$(echo "$POST_RESTART_SESSIONS" | python3 -c "
import sys, json
try: print(len(json.load(sys.stdin)))
except: print(0)
" 2>/dev/null || echo "0")

# Sessions should still be in the DB (agents are still running in tmux)
if [[ "$POST_RESTART_SESSION_COUNT" -gt 0 ]]; then
    pass "Server restart: sessions DB has $POST_RESTART_SESSION_COUNT live sessions"
else
    fail "Server restart: sessions DB is empty after restart"
fi

# 6. Verify the server is fully functional by posting a new message
FUNCTIONAL_BOARD="stress-team-1"
FUNCTIONAL_SENDER=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
for s in json.load(sys.stdin):
    if s.get('board_project') == '${FUNCTIONAL_BOARD}':
        print(s.get('display_name', ''))
        break
" 2>/dev/null || true)

if [[ -n "$FUNCTIONAL_SENDER" ]]; then
    POST_BODY=$(python3 -c "
import json
print(json.dumps({'subscriber_id': '${FUNCTIONAL_SENDER}', 'content': 'Post-restart functional test message'}))
" 2>/dev/null)
    POST_RESP=$(api POST "/api/board/${FUNCTIONAL_BOARD}/messages" -H "Content-Type: application/json" -d "$POST_BODY" 2>/dev/null || echo "")
    if echo "$POST_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d.get('id',0) > 0" 2>/dev/null; then
        pass "Server restart: post-restart message posting works"
    else
        fail "Server restart: failed to post message after restart"
    fi
else
    warn "Server restart: no agent found for post-restart functional test"
fi

log "  Post-restart state:"
log "    Sessions: $POST_RESTART_SESSION_COUNT (was $PRE_RESTART_SESSION_COUNT)"
log "    Boards: $POST_RESTART_BOARD_COUNT"

# ── Phase 4: Sleep/wake cycles ──────────────────────────────────────────
log ""
log "Phase 4: Sleep/wake cycles..."

# Get live sessions
SESSIONS=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
SESSION_NAMES=$(echo "$SESSIONS" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
for s in sessions[:3]:  # Test with first 3
    print(s.get('name', ''))
" 2>/dev/null || true)

SLEEP_OK=0
WAKE_OK=0
for NAME in $SESSION_NAMES; do
    [[ -z "$NAME" ]] && continue

    # Sleep
    RESP=$(api POST "/api/sessions/live/${NAME}/sleep" -H "Content-Type: application/json" -d '{}' 2>/dev/null || echo "")
    if echo "$RESP" | grep -qi "ok\|success\|sleeping"; then
        SLEEP_OK=$((SLEEP_OK+1))
    fi
    sleep 1

    # Wake
    RESP=$(api POST "/api/sessions/live/${NAME}/wake" -H "Content-Type: application/json" -d '{}' 2>/dev/null || echo "")
    if echo "$RESP" | grep -qi "ok\|success\|woke"; then
        WAKE_OK=$((WAKE_OK+1))
    fi
    sleep 1
done

if [[ $SLEEP_OK -gt 0 ]]; then
    pass "Sleep cycles completed: $SLEEP_OK sessions"
else
    warn "No sleep cycles completed (may need running agents)"
fi

# Validate: API still healthy after sleep/wake cycles
if curl -s "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
    pass "Server healthy after sleep/wake cycles"
else
    fail "Server unhealthy after sleep/wake cycles"
fi

# ── Phase 4.5: Reset Team ──────────────────────────────────────────────
log ""
log "Phase 4.5: Reset team cycle..."

# Get session IDs before reset
PRE_RESET_IDS=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
for s in json.load(sys.stdin):
    print(s.get('session_id', ''))
" 2>/dev/null || true)
PRE_RESET_COUNT=$(echo "$PRE_RESET_IDS" | grep -c . || echo "0")

# Find a board name from live sessions
# Debug: show what the API returns for board detection
LIVE_JSON=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
log "  Live sessions sample: $(echo "$LIVE_JSON" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
if sessions:
    s = sessions[0]
    print({k:v for k,v in s.items() if 'board' in k.lower() or k in ('name','session_id')})
else:
    print('(none)')
" 2>/dev/null || echo "(parse error)")"

# Also check board projects API
BOARDS_JSON=$(api GET /api/board/projects 2>/dev/null || echo "[]")
log "  Board projects: $(echo "$BOARDS_JSON" | head -c 200)"

BOARD_NAME=$(echo "$LIVE_JSON" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
for s in sessions:
    b = s.get('board_name') or s.get('board') or ''
    if b:
        print(b)
        break
" 2>/dev/null || true)

# Fallback: use first stress-team board name directly
if [[ -z "$BOARD_NAME" ]]; then
    BOARD_NAME="stress-team-1"
    log "  Using fallback board name: $BOARD_NAME"
fi

if [[ -n "$BOARD_NAME" ]]; then
    log "  Resetting team on board: $BOARD_NAME (pre-reset count: $PRE_RESET_COUNT)"
    RESET_RESP=$(api POST "/api/sessions/live/team/${BOARD_NAME}/reset" -H "Content-Type: application/json" -d '{}' 2>/dev/null || echo "")

    if echo "$RESET_RESP" | grep -q '"ok":true\|"ok": true'; then
        # Wait for agents to come back up (max 30s)
        RESET_WAIT=0
        while [[ $RESET_WAIT -lt 30 ]]; do
            POST_RESET_COUNT=$(api GET /api/sessions/live 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
            if [[ "$POST_RESET_COUNT" -ge "$PRE_RESET_COUNT" ]]; then
                break
            fi
            sleep 2
            RESET_WAIT=$((RESET_WAIT+2))
        done

        # Verify agents came back
        POST_RESET_IDS=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
for s in json.load(sys.stdin):
    print(s.get('session_id', ''))
" 2>/dev/null || true)

        # Check new session IDs (should be different from pre-reset)
        SAME_IDS=0
        for ID in $POST_RESET_IDS; do
            if echo "$PRE_RESET_IDS" | grep -q "$ID"; then
                SAME_IDS=$((SAME_IDS+1))
            fi
        done

        if [[ "$POST_RESET_COUNT" -ge "$PRE_RESET_COUNT" ]]; then
            pass "Reset team: $POST_RESET_COUNT agents relaunched"
        else
            fail "Reset team: only $POST_RESET_COUNT/$PRE_RESET_COUNT agents relaunched"
        fi

        if [[ "$SAME_IDS" -eq 0 ]]; then
            pass "Reset team: all agents have new session IDs"
        else
            warn "Reset team: $SAME_IDS agents kept same session ID"
        fi

        # Verify server healthy after reset
        if curl -s "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
            pass "Server healthy after team reset"
        else
            fail "Server unhealthy after team reset"
        fi

        # Verify agent sessions after reset
        SESSIONS_POST_RESET=$(count_agent_sessions)
        log "  Agent sessions after reset: $SESSIONS_POST_RESET ($BACKEND)"
    else
        warn "Reset team returned unexpected response: $RESET_RESP"
    fi
else
    warn "No board found — skipping reset team test (agents may not be on a board)"
fi

# ── Phase 4.6: Reset team with sleeping agents ────────────────────────────
log ""
log "Phase 4.6: Reset team with sleeping agents..."

# Use stress-team-2 (stress-team-1 was already reset in Phase 4.5)
P46_BOARD="stress-team-2"

# Get agents on this board
P46_SESSIONS=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
P46_BOARD_AGENTS=$(echo "$P46_SESSIONS" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board_agents = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
for a in board_agents:
    print(json.dumps({'session_id': a['session_id'], 'name': a['name'], 'display_name': a.get('display_name', '')}))
" 2>/dev/null || true)

P46_AGENT_COUNT=$(echo "$P46_BOARD_AGENTS" | grep -c . || echo "0")
log "  Found $P46_AGENT_COUNT agents on board $P46_BOARD"

if [[ "$P46_AGENT_COUNT" -ge 2 ]]; then
    # Collect session IDs and names
    P46_SESSION_IDS=$(echo "$P46_BOARD_AGENTS" | python3 -c "
import sys, json
for line in sys.stdin:
    line = line.strip()
    if line:
        print(json.loads(line)['session_id'])
" 2>/dev/null || true)

    P46_AGENT_NAMES=$(echo "$P46_BOARD_AGENTS" | python3 -c "
import sys, json
for line in sys.stdin:
    line = line.strip()
    if line:
        print(json.loads(line)['name'])
" 2>/dev/null || true)

    P46_DISPLAY_NAMES=$(echo "$P46_BOARD_AGENTS" | python3 -c "
import sys, json
for line in sys.stdin:
    line = line.strip()
    if line:
        print(json.loads(line)['display_name'])
" 2>/dev/null || true)

    # Sleep HALF of them individually (use session_id, not name — the sleep endpoint matches on SessionID)
    P46_HALF=$((P46_AGENT_COUNT / 2))
    P46_SLEEP_COUNT=0
    P46_SLEEP_TARGETS=""
    for SID in $P46_SESSION_IDS; do
        [[ -z "$SID" ]] && continue
        if [[ $P46_SLEEP_COUNT -ge $P46_HALF ]]; then
            break
        fi
        RESP=$(api POST "/api/sessions/live/${SID}/sleep" -H "Content-Type: application/json" -d '{}' 2>/dev/null || echo "")
        if echo "$RESP" | grep -q '"ok":true\|"ok": true'; then
            P46_SLEEP_COUNT=$((P46_SLEEP_COUNT + 1))
            P46_SLEEP_TARGETS="${P46_SLEEP_TARGETS} ${SID}"
        else
            log "    Sleep failed for $SID: $RESP"
        fi
        sleep 0.5
    done
    log "  Slept $P46_SLEEP_COUNT/$P46_HALF agents individually"

    # Verify via API that half are sleeping and half are live
    sleep 1
    P46_POST_SLEEP=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
    P46_SLEEPING_NOW=$(echo "$P46_POST_SLEEP" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
sleeping = sum(1 for s in board if s.get('sleeping'))
live = sum(1 for s in board if not s.get('sleeping'))
print(f'{sleeping} {live}')
" 2>/dev/null || echo "0 0")
    P46_NUM_SLEEPING=$(echo "$P46_SLEEPING_NOW" | awk '{print $1}')
    P46_NUM_LIVE=$(echo "$P46_SLEEPING_NOW" | awk '{print $2}')
    log "  Board state before reset: $P46_NUM_SLEEPING sleeping, $P46_NUM_LIVE live"

    if [[ "$P46_NUM_SLEEPING" -ge 1 ]] && [[ "$P46_NUM_LIVE" -ge 1 ]]; then
        pass "Phase 4.6: Mixed sleeping/live state confirmed ($P46_NUM_SLEEPING sleeping, $P46_NUM_LIVE live)"
    else
        warn "Phase 4.6: Could not achieve mixed state (sleeping=$P46_NUM_SLEEPING, live=$P46_NUM_LIVE)"
    fi

    # Record pre-reset data
    P46_PRE_IDS=$(echo "$P46_POST_SLEEP" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
for s in sessions:
    if s.get('board_project') == '${P46_BOARD}':
        print(s['session_id'])
" 2>/dev/null || true)
    P46_PRE_COUNT=$(echo "$P46_PRE_IDS" | grep -c . || echo "0")
    log "  Pre-reset: $P46_PRE_COUNT sessions on $P46_BOARD"

    # Reset the board
    RESET_RESP=$(api POST "/api/sessions/live/team/${P46_BOARD}/reset" -H "Content-Type: application/json" -d '{}' 2>/dev/null || echo "")
    log "  Reset response: $(echo "$RESET_RESP" | head -c 200)"

    if echo "$RESET_RESP" | grep -q '"ok":true\|"ok": true'; then
        # Wait for agents to come back (max 45s — sleeping agents may take longer)
        P46_WAIT=0
        while [[ $P46_WAIT -lt 45 ]]; do
            P46_POST_COUNT=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
print(len(board))
" 2>/dev/null || echo "0")
            if [[ "$P46_POST_COUNT" -ge "$P46_PRE_COUNT" ]]; then
                break
            fi
            sleep 2
            P46_WAIT=$((P46_WAIT + 2))
        done
        log "  Waited ${P46_WAIT}s for agents to relaunch"

        # ── Validate ──────────────────────────────────────────────────

        # 1. Total live session count equals original (no duplicates)
        P46_FINAL=$(api GET /api/sessions/live 2>/dev/null || echo "[]")
        P46_FINAL_BOARD=$(echo "$P46_FINAL" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
print(len(board))
" 2>/dev/null || echo "0")

        if [[ "$P46_FINAL_BOARD" -eq "$P46_PRE_COUNT" ]]; then
            pass "Phase 4.6: Session count matches original ($P46_FINAL_BOARD == $P46_PRE_COUNT)"
        else
            fail "Phase 4.6: Session count mismatch ($P46_FINAL_BOARD != $P46_PRE_COUNT expected)"
        fi

        # 2. All session IDs are new (none match pre-reset)
        P46_POST_IDS=$(echo "$P46_FINAL" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
for s in sessions:
    if s.get('board_project') == '${P46_BOARD}':
        print(s['session_id'])
" 2>/dev/null || true)

        P46_STALE_IDS=0
        for ID in $P46_POST_IDS; do
            if echo "$P46_PRE_IDS" | grep -q "$ID"; then
                P46_STALE_IDS=$((P46_STALE_IDS + 1))
            fi
        done

        if [[ "$P46_STALE_IDS" -eq 0 ]]; then
            pass "Phase 4.6: All session IDs are new (no stale IDs)"
        else
            fail "Phase 4.6: $P46_STALE_IDS session(s) kept pre-reset ID (stale sessions not cleaned up)"
        fi

        # 3. No sessions on the board have is_sleeping=1
        P46_STILL_SLEEPING=$(echo "$P46_FINAL" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
sleeping = [s for s in board if s.get('sleeping')]
print(len(sleeping))
" 2>/dev/null || echo "0")

        if [[ "$P46_STILL_SLEEPING" -eq 0 ]]; then
            pass "Phase 4.6: No sleeping sessions remain after reset"
        else
            fail "Phase 4.6: $P46_STILL_SLEEPING session(s) still sleeping after reset (sleeping agents not cleaned up)"
        fi

        # 4. Check board_subscribers in messageboard DB
        MB_DB="${DATA_DIR}/messageboard.db"
        if command -v sqlite3 &>/dev/null && [[ -f "$MB_DB" ]]; then
            P46_SUB_COUNT=$(sqlite3 "$MB_DB" "SELECT COUNT(*) FROM board_subscribers WHERE project='${P46_BOARD}' AND is_active=1" 2>/dev/null || echo "?")
            if [[ "$P46_SUB_COUNT" == "$P46_FINAL_BOARD" ]]; then
                pass "Phase 4.6: Board subscribers match agent count ($P46_SUB_COUNT == $P46_FINAL_BOARD)"
            else
                fail "Phase 4.6: Board subscriber mismatch ($P46_SUB_COUNT subscribers vs $P46_FINAL_BOARD agents — ghost subscribers?)"
            fi

            # 4a. No duplicate subscriber_ids on this board
            P46_DUP_SUBS=$(sqlite3 "$MB_DB" "SELECT COUNT(*) FROM (SELECT subscriber_id, COUNT(*) as cnt FROM board_subscribers WHERE project='${P46_BOARD}' AND is_active=1 GROUP BY subscriber_id HAVING cnt > 1)" 2>/dev/null || echo "?")
            if [[ "$P46_DUP_SUBS" == "0" ]]; then
                pass "Phase 4.6: No duplicate subscriber_ids on board"
            else
                fail "Phase 4.6: $P46_DUP_SUBS duplicate subscriber_id(s) found on board"
                sqlite3 "$MB_DB" "SELECT subscriber_id, COUNT(*) as cnt FROM board_subscribers WHERE project='${P46_BOARD}' AND is_active=1 GROUP BY subscriber_id HAVING cnt > 1" 2>/dev/null | while read -r line; do
                    log "    dup: $line"
                done
            fi

            # 4b. subscriber_id matches agent role/display_name (not empty, not a session UUID)
            P46_BAD_SUB_IDS=$(sqlite3 "$MB_DB" "SELECT COUNT(*) FROM board_subscribers WHERE project='${P46_BOARD}' AND is_active=1 AND (subscriber_id IS NULL OR subscriber_id = '' OR subscriber_id LIKE 'claude-%')" 2>/dev/null || echo "?")
            if [[ "$P46_BAD_SUB_IDS" == "0" ]]; then
                pass "Phase 4.6: All subscriber_ids are role names (not session UUIDs)"
            else
                fail "Phase 4.6: $P46_BAD_SUB_IDS subscriber(s) have bad subscriber_id (null/empty/session UUID)"
                sqlite3 "$MB_DB" "SELECT subscriber_id, session_name, job_title FROM board_subscribers WHERE project='${P46_BOARD}' AND is_active=1" 2>/dev/null | while read -r line; do
                    log "    sub: $line"
                done
            fi

            # 4c. session_name updated to new tmux session after reset
            P46_STALE_SESSIONS=$(sqlite3 "$MB_DB" "SELECT COUNT(*) FROM board_subscribers WHERE project='${P46_BOARD}' AND is_active=1 AND session_name IN ($(echo "$P46_PRE_IDS" | sed "s/^/'/;s/$/'/" | paste -sd, -))" 2>/dev/null || echo "?")
            if [[ "$P46_STALE_SESSIONS" == "0" ]]; then
                pass "Phase 4.6: All session_names updated to new sessions after reset"
            else
                warn "Phase 4.6: $P46_STALE_SESSIONS subscriber(s) still have pre-reset session_name"
            fi
        else
            warn "Phase 4.6: Cannot check board_subscribers (sqlite3 unavailable or DB not found at $MB_DB)"
        fi

        # Also check sessions.db for sleeping flag
        SESS_DB="${DATA_DIR}/sessions.db"
        if command -v sqlite3 &>/dev/null && [[ -f "$SESS_DB" ]]; then
            P46_DB_SLEEPING=$(sqlite3 "$SESS_DB" "SELECT COUNT(*) FROM live_sessions WHERE board_name='${P46_BOARD}' AND is_sleeping=1" 2>/dev/null || echo "?")
            if [[ "$P46_DB_SLEEPING" == "0" ]]; then
                pass "Phase 4.6: DB confirms no sleeping sessions on board"
            else
                fail "Phase 4.6: DB has $P46_DB_SLEEPING sleeping session(s) on $P46_BOARD (stale rows)"
            fi
        fi

        # 5. All agents have correct display_name (none should be 'claude')
        P46_BAD_NAMES=$(echo "$P46_FINAL" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
bad = [s['name'] for s in board if s.get('display_name', '').lower() == 'claude' or not s.get('display_name')]
print(len(bad))
for b in bad:
    print(f'  bad: {b}', file=sys.stderr)
" 2>/dev/null || echo "0")

        if [[ "$P46_BAD_NAMES" == "0" ]]; then
            pass "Phase 4.6: All agents have correct display names (none are 'claude')"
        else
            fail "Phase 4.6: $P46_BAD_NAMES agent(s) have bad display_name ('claude' or empty)"
        fi

        # 6. Server healthy after the operation
        if curl -s "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
            pass "Phase 4.6: Server healthy after reset-with-sleeping-agents"
        else
            fail "Phase 4.6: Server unhealthy after reset-with-sleeping-agents"
        fi

        # 7. Working directory preserved after reset
        P46_BAD_WORKDIRS=$(echo "$P46_FINAL" | python3 -c "
import sys, json
sessions = json.load(sys.stdin)
board = [s for s in sessions if s.get('board_project') == '${P46_BOARD}']
expected = '${WORK_DIR}'
bad = []
for s in board:
    wd = s.get('working_directory', '')
    if wd != expected:
        bad.append(f\"{s.get('name')}: got '{wd}' expected '{expected}'\")
print(len(bad))
for b in bad:
    print(b, file=sys.stderr)
" 2>>"$LOG_FILE" || echo "?")

        if [[ "$P46_BAD_WORKDIRS" == "0" ]]; then
            pass "Phase 4.6: All agents have correct working_directory (API)"
        else
            fail "Phase 4.6: $P46_BAD_WORKDIRS agent(s) have wrong working_directory (API) — see log"
        fi

        # 8. DB-level working_dir check
        SESS_DB="${DATA_DIR}/sessions.db"
        if command -v sqlite3 &>/dev/null && [[ -f "$SESS_DB" ]]; then
            P46_DB_BAD_WD=$(sqlite3 "$SESS_DB" "SELECT COUNT(*) FROM live_sessions WHERE board_name='${P46_BOARD}' AND (working_dir != '${WORK_DIR}' OR working_dir IS NULL OR working_dir = '')" 2>/dev/null || echo "?")
            if [[ "$P46_DB_BAD_WD" == "0" ]]; then
                pass "Phase 4.6: DB confirms all agents have correct working_dir"
            else
                fail "Phase 4.6: DB has $P46_DB_BAD_WD agent(s) with wrong/empty working_dir"
                sqlite3 "$SESS_DB" "SELECT session_id, working_dir FROM live_sessions WHERE board_name='${P46_BOARD}'" 2>/dev/null | while read -r line; do
                    log "    $line"
                done
            fi
        fi
    else
        fail "Phase 4.6: Reset team returned error: $RESET_RESP"
    fi
else
    warn "Phase 4.6: Not enough agents on $P46_BOARD ($P46_AGENT_COUNT < 2), skipping"
fi

# ── Phase 4.7: Individual agent restart ───────────────────────────────
log ""
log "Phase 4.7: Individual agent restart..."

# Pick an agent from stress-team-3 (untouched by Phase 4.5/4.6 resets)
P47_BOARD="stress-team-3"
P47_AGENT=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
for s in json.load(sys.stdin):
    if s.get('board_project') == '${P47_BOARD}':
        print(json.dumps({
            'name': s.get('name', ''),
            'session_id': s.get('session_id', ''),
            'display_name': s.get('display_name', ''),
            'working_directory': s.get('working_directory', ''),
            'tmux_session': s.get('tmux_session', ''),
        }))
        break
" 2>/dev/null || true)

if [[ -n "$P47_AGENT" ]]; then
    P47_NAME=$(echo "$P47_AGENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['name'])" 2>/dev/null)
    P47_OLD_SID=$(echo "$P47_AGENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['session_id'])" 2>/dev/null)
    P47_DISPLAY=$(echo "$P47_AGENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['display_name'])" 2>/dev/null)
    P47_WORKDIR=$(echo "$P47_AGENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['working_directory'])" 2>/dev/null)
    log "  Restarting agent: name=$P47_NAME session_id=${P47_OLD_SID:0:8} display=$P47_DISPLAY"

    # Count subscribers before restart
    MB_DB="${DATA_DIR}/messageboard.db"
    P47_PRE_SUB_COUNT=""
    if command -v sqlite3 &>/dev/null && [[ -f "$MB_DB" ]]; then
        P47_PRE_SUB_COUNT=$(sqlite3 "$MB_DB" "SELECT COUNT(*) FROM board_subscribers WHERE project='${P47_BOARD}' AND is_active=1" 2>/dev/null || echo "?")
    fi

    # Restart the agent
    RESTART_RESP=$(api POST "/api/sessions/live/${P47_NAME}/restart" \
        -H "Content-Type: application/json" \
        -d "{\"session_id\":\"${P47_OLD_SID}\",\"agent_type\":\"claude\"}" 2>/dev/null || echo "")

    if echo "$RESTART_RESP" | grep -q '"ok":true\|"ok": true'; then
        P47_NEW_SID=$(echo "$RESTART_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null || echo "")
        log "  Restart OK: new session_id=${P47_NEW_SID:0:8}"
        sleep 2

        # Get the restarted agent's state
        P47_POST=$(api GET /api/sessions/live 2>/dev/null | python3 -c "
import sys, json
for s in json.load(sys.stdin):
    if s.get('session_id') == '${P47_NEW_SID}':
        print(json.dumps(s))
        break
" 2>/dev/null || true)

        if [[ -n "$P47_POST" ]]; then
            # 1. New session ID (different from old)
            if [[ "$P47_NEW_SID" != "$P47_OLD_SID" ]]; then
                pass "Phase 4.7: Agent got new session ID after restart"
            else
                fail "Phase 4.7: Agent kept same session ID after restart"
            fi

            # 2. display_name preserved
            P47_POST_DISPLAY=$(echo "$P47_POST" | python3 -c "import sys,json; print(json.load(sys.stdin).get('display_name',''))" 2>/dev/null)
            if [[ "$P47_POST_DISPLAY" == "$P47_DISPLAY" ]]; then
                pass "Phase 4.7: display_name preserved after restart ($P47_POST_DISPLAY)"
            else
                fail "Phase 4.7: display_name changed after restart ('$P47_POST_DISPLAY' != '$P47_DISPLAY')"
            fi

            # 3. working_directory preserved
            P47_POST_WD=$(echo "$P47_POST" | python3 -c "import sys,json; print(json.load(sys.stdin).get('working_directory',''))" 2>/dev/null)
            if [[ "$P47_POST_WD" == "$P47_WORKDIR" ]]; then
                pass "Phase 4.7: working_directory preserved after restart"
            else
                fail "Phase 4.7: working_directory changed after restart ('$P47_POST_WD' != '$P47_WORKDIR')"
            fi

            # 4. No duplicate board subscribers
            if command -v sqlite3 &>/dev/null && [[ -f "$MB_DB" ]]; then
                P47_POST_SUB_COUNT=$(sqlite3 "$MB_DB" "SELECT COUNT(*) FROM board_subscribers WHERE project='${P47_BOARD}' AND is_active=1" 2>/dev/null || echo "?")
                if [[ "$P47_POST_SUB_COUNT" == "$P47_PRE_SUB_COUNT" ]]; then
                    pass "Phase 4.7: No duplicate subscribers after restart ($P47_POST_SUB_COUNT == $P47_PRE_SUB_COUNT)"
                else
                    fail "Phase 4.7: Subscriber count changed after restart ($P47_POST_SUB_COUNT != $P47_PRE_SUB_COUNT)"
                fi

                # 5. subscriber_id still matches role name
                P47_SUB_ID=$(sqlite3 "$MB_DB" "SELECT subscriber_id FROM board_subscribers WHERE project='${P47_BOARD}' AND is_active=1 AND job_title='${P47_DISPLAY}'" 2>/dev/null || echo "")
                if [[ "$P47_SUB_ID" == "$P47_DISPLAY" ]]; then
                    pass "Phase 4.7: subscriber_id still matches role name after restart"
                else
                    warn "Phase 4.7: subscriber_id is '$P47_SUB_ID' (expected '$P47_DISPLAY')"
                fi
            fi

            # 6. Server healthy
            if curl -s "http://127.0.0.1:${PORT}/api/health" > /dev/null 2>&1; then
                pass "Phase 4.7: Server healthy after individual restart"
            else
                fail "Phase 4.7: Server unhealthy after individual restart"
            fi
        else
            fail "Phase 4.7: Could not find restarted agent with session_id=$P47_NEW_SID"
        fi
    else
        fail "Phase 4.7: Restart returned error: $RESTART_RESP"
    fi
else
    warn "Phase 4.7: No agents found on $P47_BOARD for restart test"
fi

# ── Phase 5: Kill all agents ────────────────────────────────────────────
log ""
log "Phase 5: Killing all agents..."

# Kill all agent sessions
kill_agent_sessions
sleep 2

# Verify no orphan sessions
ORPHANS=$(count_agent_sessions)
ORPHANS="${ORPHANS:-0}"
if [[ "$ORPHANS" -eq 0 ]]; then
    pass "No orphan agent sessions ($BACKEND)"
else
    fail "Found $ORPHANS orphan agent sessions ($BACKEND)"
fi

# Clean up board state files from test data dir
BOARD_FILES=$(find "$DATA_DIR" -name "board_state_*.json" 2>/dev/null | wc -l | tr -d '[:space:]')
if [[ "$BOARD_FILES" -gt 0 ]]; then
    log "  Cleaning up $BOARD_FILES board_state files from test dir"
    find "$DATA_DIR" -name "board_state_*.json" -delete 2>/dev/null
fi

# Check server is still alive after killing agents
if kill -0 "$SERVER_PID" 2>/dev/null; then
    pass "Server survived agent kill phase"
else
    fail "Server died during agent kill phase"
fi

# Validate: API reports 0 live sessions after kill
sleep 1
POST_KILL_COUNT=$(api GET /api/sessions/live 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "?")
log "  Live sessions after kill: $POST_KILL_COUNT"

# ── Phase 6: Isolation verification ─────────────────────────────────────
log ""
log "Phase 6: Isolation verification..."

# Check that the REAL ~/.coral was NOT modified during the test
if [[ -d "$REAL_CORAL_DIR" ]]; then
    POST_SNAPSHOT="${DATA_DIR}/home-coral-post.txt"
    find "$REAL_CORAL_DIR" -type f -newer "$LOG_FILE" 2>/dev/null | sort > "$POST_SNAPSHOT" || true
    NEW_FILES=$(comm -13 "$SNAPSHOT_FILE" "$POST_SNAPSHOT" 2>/dev/null || true)
    if [[ -z "$NEW_FILES" ]]; then
        pass "Real ~/.coral was not modified during test"
    else
        fail "Real ~/.coral was modified during test: $NEW_FILES"
    fi
else
    pass "Real ~/.coral does not exist (fully isolated)"
fi

# Check no board_state files leaked to real ~/.coral
if [[ -d "$REAL_CORAL_DIR" ]]; then
    LEAKED_BOARD=$(find "$REAL_CORAL_DIR" -name "board_state_*stress*" -newer "$LOG_FILE" 2>/dev/null | head -3)
    if [[ -z "$LEAKED_BOARD" ]]; then
        pass "No board_state files leaked to real ~/.coral"
    else
        fail "board_state files leaked to real ~/.coral: $LEAKED_BOARD"
    fi
fi

# Verify data was written to our isolated dir
if find "$DATA_DIR" -name "sessions.db" -print -quit 2>/dev/null | grep -q .; then
    DB_PATH=$(find "$DATA_DIR" -name "sessions.db" -print -quit 2>/dev/null)
    pass "Data written to isolated dir: $DB_PATH"
else
    warn "No sessions.db found in isolated dir"
fi

# ── Summary ─────────────────────────────────────────────────────────────
log ""
log "════════════════════════════════════════════════════════════"
log "Results: ${PASSED}/${TOTAL} passed, ${FAILED} failed"
if [[ $FAILED -eq 0 ]]; then
    log "${GREEN}ALL TESTS PASSED${NC}"
else
    log "${RED}${FAILED} TEST(S) FAILED${NC}"
fi
log "Log saved to: /tmp/coral-stress-${TEST_ID}.log"
log "════════════════════════════════════════════════════════════"

exit $FAILED
