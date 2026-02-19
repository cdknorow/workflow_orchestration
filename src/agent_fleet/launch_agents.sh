#!/usr/bin/env bash
set -euo pipefail

TARGET_DIR="${1:-.}"
AGENT_TYPE="${2:-claude}"
LOG_DIR="/tmp"
MAX_AGENTS=3


TARGET_DIR="$(cd "$TARGET_DIR" && pwd)"

# Clean up all existing fleet logs
rm -f "${LOG_DIR}/"*_fleet_*.log

# Locate assets relative to this script
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROTOCOL_PATH="${SCRIPT_DIR}/PROTOCOL.md"
DASHBOARD_PATH="${SCRIPT_DIR}/dashboard.py"

# New function to launch the dashboard in a direct terminal window (No tmux)
launch_dashboard() {
    local title="Fleet Dashboard"
    local cmd="python3 $DASHBOARD_PATH"

    # If installed as a package, use the entry point
    if command -v fleet-dash &>/dev/null; then
        cmd="fleet-dash"
    elif [ ! -f "$DASHBOARD_PATH" ]; then
        echo "  [!] Dashboard skip: dashboard.py not found and fleet-dash not in PATH."
        return 1
    fi

    echo "=== Launching Dashboard (Direct Window) ==="

    if [ -n "${SSH_CONNECTION:-}" ]; then
        echo "  [~] SSH detected — skipping GUI dashboard."
        return 0
    fi

    if command -v osascript &>/dev/null; then
        if osascript -e 'tell application "iTerm2" to version' &>/dev/null 2>&1; then
            osascript << EOF
tell application "iTerm2"
    create window with default profile command "$cmd"
end tell
EOF
        else
            osascript << EOF
tell application "Terminal"
    do script "$cmd"
    set custom title of front window to "${title}"
end tell
EOF
        fi
        echo "  [+] Dashboard launched in new window."
    else
        echo "  [!] Cannot open dashboard: osascript not found (macOS only)"
    fi
    echo ""
}

# Open a new terminal window attached to a tmux session
open_agent_terminal() {
    local session="$1"
    local title="$2"
    # SSH_CONNECTION is set by sshd whenever the shell is reached via SSH.
    # Skip GUI window opening silently in that environment.
    if [ -n "${SSH_CONNECTION:-}" ]; then
        echo "  [~] SSH detected — skipping terminal window (use: tmux attach -t $session)"
        return 0
    fi
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

echo "=== $AGENT_TYPE Fleet Launcher (Independent Sessions) ==="
echo "Target directory: $TARGET_DIR"
echo "Found ${#worktrees[@]} workspace(s):"
echo ""

agent_index=1
for dir in "${worktrees[@]}"; do
    folder_name="$(basename "$dir")"
    session_name="${AGENT_TYPE}-agent-${agent_index}"
    log_file="${LOG_DIR}/${AGENT_TYPE}_fleet_${folder_name}.log"

    # Kill existing session if present
    tmux kill-session -t "$session_name" 2>/dev/null || true

    # Clear old log
    > "$log_file"

    # Create a new detached session rooted in the worktree
    tmux new-session -d -s "$session_name" -c "$dir"

    # Stream stdout to log file
    tmux pipe-pane -t "${session_name}" -o "cat >> ${log_file}"

    # Pane 0: agent (with PROTOCOL.md piped in as system prompt if available)
    tmux send-keys -t "${session_name}.0" "printf '\033]2;${folder_name} — ${AGENT_TYPE}\033\\\\'" Enter
    if [ "$AGENT_TYPE" == "gemini" ]; then
        if [ -f "$PROTOCOL_PATH" ]; then
            tmux send-keys -t "${session_name}.0" "GEMINI_SYSTEM_MD=\"${PROTOCOL_PATH}\" gemini" Enter
        else
            tmux send-keys -t "${session_name}.0" "gemini" Enter
        fi
    else
        if [ -f "$PROTOCOL_PATH" ]; then
            tmux send-keys -t "${session_name}.0" "claude --append-system-prompt \"\$(cat '${PROTOCOL_PATH}')\"" Enter
        else
            tmux send-keys -t "${session_name}.0" "claude" Enter
        fi
    fi


    # Focus back on the agent pane
    tmux select-pane -t "${session_name}.0"

    # Open a new terminal window and attach to this session
    open_agent_terminal "$session_name" "${folder_name} — ${AGENT_TYPE}"

    echo "  [+] Session : $session_name"
    echo "      Dir     : $dir"
    echo "      Log     : $log_file"
    echo "      Attach  : tmux attach -t $session_name"
    echo ""

    agent_index=$((agent_index + 1))
done


launch_dashboard

echo "=== All sessions launched ==="
echo ""
echo "Quick attach commands:"
for i in $(seq 1 $((agent_index - 1))); do
    echo "  tmux attach -t ${AGENT_TYPE}-agent-${i}"
done
echo ""
echo "Kill all agents:"
echo "  for i in \$(seq 1 $((agent_index - 1))); do tmux kill-session -t ${AGENT_TYPE}-agent-\$i 2>/dev/null; done"
