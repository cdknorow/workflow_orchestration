 Master Prompt: The "Claude Fleet Commander"
  Role: You are an expert Senior Python Engineer and Shell Scripter.

  Objective: Build a terminal-based orchestration system called "Claude Fleet". It manages multiple AI coding
  agents running in parallel git worktrees.

  Architecture:

  The Backend (Tmux): A Bash script dynamically discovers git worktrees and launches a detached tmux session
  with a window for each worktree.

  The Interface (Python TUI): A textual based dashboard that monitors these sessions via log files.

  Part 1: The Launcher Script (launch_fleet.sh)

  Input: Accept a target directory path (defaults to current dir) where the git worktrees are located.

  Discovery Logic:

  Scan the target directory for subdirectories.

  Identify which subdirectories are valid git worktrees (or just assume all subdirs are distinct workspaces).

  Tmux Orchestration:

  Create a new detached tmux session named claude-fleet.

  Loop: For each discovered worktree:

  Create a new tmux window (named after the folder).

  Send keys to navigate to that directory.

  Start pipe-pane to stream stdout to a log file: /tmp/claude_fleet_[folder_name].log.

  (Optional placeholder) Send the command echo "Starting Claude..." (do not actually run claude yet, just prep
  the shell).

  Output: Print the list of created windows and instructions to run the dashboard.

  Part 2: The Dashboard (dashboard.py)

  Tech Stack: Python 3.10+, textual framework.

  Dynamic Layout: The app must detect how many log files (/tmp/claude_fleet_*.log) exist and generate a Grid
  Layout of widgets to match. (e.g., if 3 logs found, show 3 cards).

  Widget Components (Per Worktree):

  Header: The worktree/folder name.

  Live Status: A distinct box.

  File Watcher: Asynchronously tail the corresponding log file.

  Parser (The Convention): Look for lines matching ||STATUS: (.*)||. Update the Status box with the captured
  text.

  Task List: A simple ListView where I can add/delete todo items.

  Persistence: Auto-save these lists to a local fleet_tasks.json so they survive restarts.

  Footer: Display a static help message: "To intervene: Detach dashboard (Ctrl+C), then run: tmux attach -t
  claude-fleet"

  Part 3: The Agent Protocol (PROTOCOL.md)

  Create a markdown file containing the "System Prompt" I must paste into my Claude sessions.

  Rule: "You must update your status by printing a single line: ||STATUS: <Short Description>||. Do this
  whenever you change tasks."

  Deliverables:

  launch_fleet.sh (Bash)

  dashboard.py (Python)

  requirements.txt (Must include textual)

  PROTOCOL.md

  Implementation Guide (Next Steps)
  Once the AI generates the code, here is how you will run it:

  Directory Prep: Ensure your worktrees are in one folder (e.g., ~/dev/my-project-worktrees/).

  Launch:

  Bash
  ./launch_fleet.sh ~/dev/my-project-worktrees/
  Monitor:

  Bash
  python dashboard.py
  Intervene:

  If you see an agent stuck in the UI, hit Ctrl+C to kill the dashboard.

  Run tmux attach -t claude-fleet.

  Navigate to the window (Ctrl+b, n/p), fix the issue, detach (Ctrl+b, d), and restart the dashboard.


