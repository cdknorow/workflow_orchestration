#!/usr/bin/env bash
set -euo pipefail

SESSION_NAME="claude-fleet"
LOG_DIR="/tmp"
TARGET_DIR="${1:-.}"

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

first=true
for dir in "${worktrees[@]}"; do
    folder_name="$(basename "$dir")"
    log_file="${LOG_DIR}/claude_fleet_${folder_name}.log"

    # Clear old log
    > "$log_file"

    if $first; then
        # Create session with first window
        tmux new-session -d -s "$SESSION_NAME" -n "$folder_name" -c "$dir"
        first=false
    else
        tmux new-window -t "$SESSION_NAME" -n "$folder_name" -c "$dir"
    fi

    # Enable logging via pipe-pane
    tmux pipe-pane -t "${SESSION_NAME}:${folder_name}" -o "cat >> ${log_file}"

    # Send a ready message
    tmux send-keys -t "${SESSION_NAME}:${folder_name}" "echo '||STATUS: Ready — waiting for instructions||'" Enter

    echo "  [+] ${folder_name} -> ${log_file}"
done

echo ""
echo "=== Fleet Ready ==="
echo "Tmux session: $SESSION_NAME (${#worktrees[@]} windows)"
echo ""
echo "Next steps:"
echo "  1. Start the dashboard:  python dashboard.py"
echo "  2. Or attach directly:   tmux attach -t $SESSION_NAME"
