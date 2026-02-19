#!/usr/bin/env bash
set -euo pipefail

TARGET_DIR="${1:-.}"
LOG_DIR="/tmp"
MAX_AGENTS=3

# Open a new terminal window attached to a tmux session
open_terminal() {
    local session="$1"
    local title="$2"
    if command -v osascript &>/dev/null; then
        if osascript -e 'tell application "iTerm2" to version' &>/dev/null 2>&1; then
            osascript << EOF
tell application "iTerm2"
    create window with default profile command "tmux attach -t ${session}"
end tell
EOF
        else
            osascript << EOF
tell application "Terminal"
    do script "tmux attach -t ${session}"
    set custom title of front window to "${title}"
end tell
EOF
        fi
    else
        echo "  [!] Cannot open terminal: osascript not found (macOS only)"
    fi
}

# Resolve to absolute path
TARGET_DIR="$(cd "$TARGET_DIR" && pwd)"

# Locate PROTOCOL.md relative to this script
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROTOCOL_PATH="${SCRIPT_DIR}/PROTOCOL.md"

# Collect valid subdirectories (up to MAX_AGENTS)
worktrees=()
for dir in "$TARGET_DIR"/*/; do
    [ -d "$dir" ] || continue
    worktrees+=("$dir")
    [ ${#worktrees[@]} -ge $MAX_AGENTS ] && break
done

if [ ${#worktrees[@]} -eq 0 ]; then
    echo "Error: No subdirectories found in $TARGET_DIR"
    exit 1
fi

echo "=== Claude Fleet Launcher (Independent Sessions) ==="
echo "Target directory: $TARGET_DIR"
echo "Found ${#worktrees[@]} workspace(s):"
echo ""

agent_index=1
for dir in "${worktrees[@]}"; do
    folder_name="$(basename "$dir")"
    session_name="claude-agent-${agent_index}"
    log_file="${LOG_DIR}/claude_fleet_${folder_name}.log"

    # Kill existing session if present
    tmux kill-session -t "$session_name" 2>/dev/null || true

    # Clear old log
    > "$log_file"

    # Create a new detached session rooted in the worktree
    tmux new-session -d -s "$session_name" -c "$dir"

    # Stream stdout to log file
    tmux pipe-pane -t "${session_name}" -o "cat >> ${log_file}"

    # Pane 0: claude (with PROTOCOL.md piped in as system prompt if available)
    tmux send-keys -t "${session_name}.0" "printf '\033]2;${folder_name} — claude\033\\\\'" Enter
    if [ -f "$PROTOCOL_PATH" ]; then
        tmux send-keys -t "${session_name}.0" "claude --system-prompt \"\$(cat '${PROTOCOL_PATH}')\"" Enter
    else
        tmux send-keys -t "${session_name}.0" "claude" Enter
    fi


    # Focus back on the claude pane
    tmux select-pane -t "${session_name}.0"

    # Open a new terminal window and attach to this session
    open_terminal "$session_name" "${folder_name} — claude"

    echo "  [+] Session : $session_name"
    echo "      Dir     : $dir"
    echo "      Log     : $log_file"
    echo "      Attach  : tmux attach -t $session_name"
    echo ""

    agent_index=$((agent_index + 1))
done

echo "=== All sessions launched ==="
echo ""
echo "Quick attach commands:"
for i in $(seq 1 $((agent_index - 1))); do
    echo "  tmux attach -t claude-agent-${i}"
done
echo ""
echo "Kill all agents:"
echo "  for i in \$(seq 1 $((agent_index - 1))); do tmux kill-session -t claude-agent-\$i 2>/dev/null; done"
