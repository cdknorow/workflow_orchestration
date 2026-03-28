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
SKIP_BUILD=false

# ── Parse args ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --teams)          TEAMS="$2"; shift 2 ;;
        --agents-per-team) AGENTS_PER_TEAM="$2"; shift 2 ;;
        --duration)       DURATION="$2"; shift 2 ;;
        --port)           PORT="$2"; shift 2 ;;
        --skip-build)     SKIP_BUILD=true; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Convert duration to seconds
DURATION_SECS="${DURATION%s}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
GO_DIR="$PROJECT_DIR/coral-go"
TEST_ID="stress-$$-$(date +%s)"
DATA_DIR="/tmp/.coral-${TEST_ID}"
TMUX_SOCKET="${DATA_DIR}/tmux.sock"
CORAL_BIN="${GO_DIR}/coral-stress-test"
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
    # Kill server
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    # Kill any tmux sessions on our socket
    tmux -S "$TMUX_SOCKET" kill-server 2>/dev/null || true
    # Clean up data dir (keep log for review)
    if [[ -d "$DATA_DIR" ]]; then
        cp "$LOG_FILE" "/tmp/coral-stress-${TEST_ID}.log" 2>/dev/null || true
        rm -rf "$DATA_DIR"
    fi
    # Clean up binary
    rm -f "$CORAL_BIN"
}
trap cleanup EXIT

# ── Pre-test cleanup ────────────────────────────────────────────────────
# Kill any dangling coral-stress-test processes from previous runs
pkill -f "coral-stress-test" 2>/dev/null || true
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
log "  Data dir:        $DATA_DIR"
log "  Tmux socket:     $TMUX_SOCKET"
log ""

# ── Phase 0: Build ──────────────────────────────────────────────────────
if [[ "$SKIP_BUILD" == "false" ]]; then
    log "Phase 0: Building coral binary..."
    cd "$GO_DIR"
    go build -tags dev -o "$CORAL_BIN" ./cmd/coral/ 2>&1 | tee -a "$LOG_FILE"
    log "  Built: $CORAL_BIN"
else
    CORAL_BIN="${GO_DIR}/coral"
    if [[ ! -f "$CORAL_BIN" ]]; then
        log "ERROR: --skip-build specified but $CORAL_BIN not found"
        exit 1
    fi
    log "Phase 0: Using existing binary: $CORAL_BIN"
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

"$CORAL_BIN" --home "$DATA_DIR" --host 127.0.0.1 --port "$PORT" --no-browser >> "$LOG_FILE" 2>&1 &
SERVER_PID=$!
log "  Server PID: $SERVER_PID"

if wait_for_server; then
    pass "Server started on port $PORT"
else
    fail "Server failed to start within 30s"
    cat "$LOG_FILE"
    exit 1
fi

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

# Validate: tmux session count matches expected
sleep 2
TMUX_COUNT=$(tmux -S "$TMUX_SOCKET" list-sessions 2>/dev/null | wc -l | tr -d '[:space:]' || echo "0")
TMUX_COUNT="${TMUX_COUNT:-0}"
if [[ "$TMUX_COUNT" -eq "$TOTAL_AGENTS" ]]; then
    pass "Tmux session count matches: $TMUX_COUNT"
elif [[ "$TMUX_COUNT" -gt 0 ]]; then
    warn "Tmux sessions: $TMUX_COUNT (expected $TOTAL_AGENTS)"
else
    fail "No tmux sessions found"
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

        # Verify tmux sessions match
        TMUX_POST_RESET=$(tmux -S "$TMUX_SOCKET" list-sessions 2>/dev/null | wc -l | tr -d '[:space:]' || echo "0")
        log "  Tmux sessions after reset: $TMUX_POST_RESET"
    else
        warn "Reset team returned unexpected response: $RESET_RESP"
    fi
else
    warn "No board found — skipping reset team test (agents may not be on a board)"
fi

# ── Phase 5: Kill all agents ────────────────────────────────────────────
log ""
log "Phase 5: Killing all agents..."

# Kill via tmux
tmux -S "$TMUX_SOCKET" kill-server 2>/dev/null || true
sleep 2

# Verify no orphan tmux sessions on our socket
ORPHANS=$(tmux -S "$TMUX_SOCKET" list-sessions 2>/dev/null | wc -l | tr -d '[:space:]' || echo "0")
ORPHANS="${ORPHANS:-0}"
if [[ "$ORPHANS" -eq 0 ]]; then
    pass "No orphan tmux sessions"
else
    fail "Found $ORPHANS orphan tmux sessions"
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
