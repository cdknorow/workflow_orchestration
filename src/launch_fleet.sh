#!/usr/bin/env bash
set -euo pipefail

SESSION_NAME="claude-fleet"
LOG_DIR="/tmp"
TARGET_DIR="${1:-.}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Resolve to absolute path
TARGET_DIR="$(cd "$TARGET_DIR" && pwd)"

# Collect valid subdirectories
worktrees=()
for dir in "$TARGET_DIR"/*/; do
    [ -d "$dir" ] || continue
    worktrees+=("$dir")
done

if [ ${#worktrees[@]} -eq 0 ]; then
    echo "Error: No subdirectories found in $TARGET_DIR"
    exit 1
fi

# Kill existing session if present
tmux kill-session -t "$SESSION_NAME" 2>/dev/null || true

echo "=== Claude Fleet Launcher ==="
echo "Target directory: $TARGET_DIR"
echo "Found ${#worktrees[@]} workspace(s):"
echo ""

# ── Pane 0: Dashboard (full left side) ──────────────────────
tmux new-session -d -s "$SESSION_NAME" -n "fleet" -c "$SCRIPT_DIR"
tmux send-keys -t "${SESSION_NAME}:fleet.0" \
    "printf '\033]2;dashboard\033\\\\'" Enter
tmux send-keys -t "${SESSION_NAME}:fleet.0" \
    "python '${SCRIPT_DIR}/dashboard.py'" Enter
echo "  [+] dashboard (pane 0) <- left side"

# ── Panes 1+: One pane per worktree (right side) ────────────
pane_index=1
for dir in "${worktrees[@]}"; do
    folder_name="$(basename "$dir")"
    log_file="${LOG_DIR}/claude_fleet_${folder_name}.log"

    # Clear old log
    > "$log_file"

    tmux split-window -t "${SESSION_NAME}:fleet" -c "$dir"

    # Enable logging
    tmux pipe-pane -t "${SESSION_NAME}:fleet.${pane_index}" \
        -o "cat >> ${log_file}"

    # Label pane, navigate, and start claude
    tmux send-keys -t "${SESSION_NAME}:fleet.${pane_index}" \
        "printf '\033]2;${folder_name}\033\\\\'" Enter
    tmux send-keys -t "${SESSION_NAME}:fleet.${pane_index}" \
        "cd '${dir}' && claude" Enter

    echo "  [+] ${folder_name} (pane ${pane_index}) -> ${log_file}"
    pane_index=$((pane_index + 1))
done

# ── Layout: dashboard fills the entire left, agents stack right ──
# main-pane-width controls the dashboard column width (default 50%)
tmux set-option -t "${SESSION_NAME}:fleet" main-pane-width 20%
tmux select-pane  -t "${SESSION_NAME}:fleet.0"
tmux select-layout -t "${SESSION_NAME}:fleet" main-vertical

echo ""
echo "=== Fleet Ready ==="
echo "Tmux session : $SESSION_NAME"
echo "Layout       : dashboard (left) | ${#worktrees[@]} agents (right)"
echo ""
echo "Attach with  : tmux attach -t $SESSION_NAME"
