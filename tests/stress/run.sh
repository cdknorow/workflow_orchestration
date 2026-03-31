#!/usr/bin/env bash
#
# Stress test for the Coral board task system.
# Tests task creation, claiming, completion, and listing.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CORAL_DIR="$REPO_ROOT/coral-go"
PORT=8471
HOST="127.0.0.1"
BASE_URL="http://${HOST}:${PORT}"
BOARD="stress-test-$$"
NUM_AGENTS=5
NUM_TASKS=20
PASS=0
FAIL=0
SERVER_PID=""

# ── Helpers ──────────────────────────────────────────────────────────

cleanup() {
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$TMPDIR_STRESS" 2>/dev/null || true
}
trap cleanup EXIT

log()  { echo "[stress] $*"; }
pass() { PASS=$((PASS + 1)); log "PASS: $*"; }
fail() { FAIL=$((FAIL + 1)); log "FAIL: $*"; }

api() {
    local method="$1" path="$2"
    shift 2
    curl -s -m 10 -X "$method" "${BASE_URL}/api/board/${BOARD}${path}" \
        -H "Content-Type: application/json" "$@"
}

api_status() {
    local method="$1" path="$2"
    shift 2
    curl -s -m 10 -o /dev/null -w "%{http_code}" -X "$method" "${BASE_URL}/api/board/${BOARD}${path}" \
        -H "Content-Type: application/json" "$@"
}

wait_for_server() {
    local retries=30
    while ! curl -s -m 5 "${BASE_URL}/api/health" >/dev/null 2>&1; do
        retries=$((retries - 1))
        if [[ $retries -le 0 ]]; then
            log "ERROR: Server failed to start on port $PORT"
            exit 1
        fi
        sleep 0.5
    done
    log "Server is ready on port $PORT"
}

# ── Setup ────────────────────────────────────────────────────────────

TMPDIR_STRESS="$(mktemp -d)"
export CORAL_DATA_DIR="$TMPDIR_STRESS"

log "Building coral (dev mode)..."
cd "$CORAL_DIR"
go build -tags dev -o "$TMPDIR_STRESS/coral" ./cmd/coral/

log "Starting coral server on port $PORT..."
"$TMPDIR_STRESS/coral" --host "$HOST" --port "$PORT" --backend tmux >"$TMPDIR_STRESS/server.log" 2>&1 &
SERVER_PID=$!
wait_for_server

# ── Test 1: Create tasks ────────────────────────────────────────────

log "Creating $NUM_TASKS tasks..."
created=0
for i in $(seq 1 $NUM_TASKS); do
    prio="medium"
    case $((i % 4)) in
        0) prio="critical" ;;
        1) prio="high" ;;
        2) prio="medium" ;;
        3) prio="low" ;;
    esac
    result=$(api POST "/tasks" -d "{\"title\": \"Task $i\", \"body\": \"Stress test task number $i\", \"priority\": \"$prio\", \"subscriber_id\": \"orchestrator\"}")
    tid=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
    if [[ -n "$tid" ]]; then
        created=$((created + 1))
    fi
done

if [[ "$created" -eq "$NUM_TASKS" ]]; then
    pass "Created $NUM_TASKS tasks"
else
    fail "Created $created tasks, expected $NUM_TASKS"
fi

# ── Test 2: List returns all tasks ───────────────────────────────────

list_count=$(api GET "/tasks" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('tasks', [])))" 2>/dev/null || echo "0")

if [[ "$list_count" -eq "$NUM_TASKS" ]]; then
    pass "ListTasks returns all $NUM_TASKS tasks"
else
    fail "ListTasks returned $list_count, expected $NUM_TASKS"
fi

# ── Test 3: Concurrent claims — no duplicates ────────────────────────

log "Testing $NUM_AGENTS concurrent claims..."

# Use xargs -P for reliable parallel execution
CLAIM_RESULTS="$TMPDIR_STRESS/claim_results"
mkdir -p "$CLAIM_RESULTS"

# Write a helper script for xargs to call
cat > "$TMPDIR_STRESS/do_claim.sh" <<SCRIPT
#!/usr/bin/env bash
i=\$1
curl -s -m 10 -X POST "${BASE_URL}/api/board/${BOARD}/tasks/claim" \
    -H "Content-Type: application/json" \
    -d "{\"subscriber_id\": \"agent-\${i}\"}" > "${CLAIM_RESULTS}/agent-\${i}.json"
SCRIPT
chmod +x "$TMPDIR_STRESS/do_claim.sh"

seq 1 $NUM_AGENTS | xargs -P "$NUM_AGENTS" -I{} "$TMPDIR_STRESS/do_claim.sh" {}

# All agents should have claimed a task (20 pending, 5 agents)
claim_success=0
claimed_ids=()
for i in $(seq 1 $NUM_AGENTS); do
    tid=$(cat "$CLAIM_RESULTS/agent-${i}.json" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
    if [[ -n "$tid" ]]; then
        claim_success=$((claim_success + 1))
        claimed_ids+=("$tid")
    fi
done

if [[ "$claim_success" -eq "$NUM_AGENTS" ]]; then
    pass "All $NUM_AGENTS agents claimed a task concurrently"
else
    fail "Expected $NUM_AGENTS claims, got $claim_success"
fi

unique=$(printf '%s\n' "${claimed_ids[@]}" | sort -u | wc -l | tr -d ' ')
if [[ "${#claimed_ids[@]}" -eq "$unique" ]]; then
    pass "No duplicate claims: all $unique task IDs unique"
else
    fail "Duplicate claims: ${#claimed_ids[@]} claims but $unique unique"
fi

# ── Test 4: Claim + complete lifecycle ───────────────────────────────

log "Claiming and completing all remaining tasks..."

# Complete the tasks claimed above
for i in $(seq 1 $NUM_AGENTS); do
    tid=$(cat "$CLAIM_RESULTS/agent-${i}.json" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
    if [[ -n "$tid" ]]; then
        api POST "/tasks/${tid}/complete" -d "{\"subscriber_id\": \"agent-${i}\", \"message\": \"done\"}" >/dev/null
    fi
done

# Claim and complete remaining tasks sequentially
while true; do
    result=$(api POST "/tasks/claim" -d '{"subscriber_id": "agent-1"}')
    tid=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
    [[ -z "$tid" ]] && break
    api POST "/tasks/${tid}/complete" -d '{"subscriber_id": "agent-1", "message": "done"}' >/dev/null
done

# Verify all completed
completed_count=$(api GET "/tasks" | python3 -c "
import sys, json
tasks = json.load(sys.stdin).get('tasks', [])
print(sum(1 for t in tasks if t.get('status') == 'completed'))
" 2>/dev/null || echo "0")

if [[ "$completed_count" -eq "$NUM_TASKS" ]]; then
    pass "All $NUM_TASKS tasks claimed and completed"
else
    fail "Only $completed_count of $NUM_TASKS completed"
fi

# ── Test 5: Claim returns 404 when all done ──────────────────────────

status=$(api_status POST "/tasks/claim" -d '{"subscriber_id": "agent-1"}')
if [[ "$status" == "404" ]]; then
    pass "Claim returns 404 when no tasks available"
else
    fail "Claim returned HTTP $status, expected 404"
fi

# ── Summary ──────────────────────────────────────────────────────────

echo ""
echo "================================"
echo "  STRESS TEST RESULTS"
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo "================================"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
